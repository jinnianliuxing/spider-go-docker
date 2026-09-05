package course

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"spider-go/internal/cache"
	"spider-go/internal/common"
	"spider-go/internal/service"
	"spider-go/internal/shared"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// CourseRepository 课表数据访问接口（用于离线查询）
type CourseRepository interface {
	GetCoursesByUidTermWeek(ctx context.Context, uid int, term string, week int) (*WeekSchedule, error)
}

// ReconciliationTrigger 对账触发器接口（避免循环依赖）
type ReconciliationTrigger interface {
	TriggerCourseSync(ctx context.Context, uid int)
}

// Service 课程服务接口
type Service interface {
	GetCourseTableByWeek(ctx context.Context, uid int, week int, term string) (*WeekSchedule, error)
	// GetCourseTableByWeekForSync 获取课表（供对账模块使用，不触发递归同步）
	GetCourseTableByWeekForSync(ctx context.Context, uid int, week int, term string) (*WeekSchedule, error)
	// SetCourseRepository 设置课表仓储（用于延迟注入）
	SetCourseRepository(repo CourseRepository)
	// SetReconciliationTrigger 设置对账触发器（用于延迟注入）
	SetReconciliationTrigger(trigger ReconciliationTrigger)
}

// courseService 课程服务实现
type courseService struct {
	userQuery             shared.UserQuery
	sessionService        service.SessionService
	crawlerService        service.CrawlerService
	userDataCache         cache.UserDataCache
	courseRepo            CourseRepository
	reconciliationTrigger ReconciliationTrigger
	courseURL             string
}

// NewService 创建课程服务
func NewService(
	userQuery shared.UserQuery,
	sessionService service.SessionService,
	crawlerService service.CrawlerService,
	userDataCache cache.UserDataCache,
	courseURL string,
) Service {
	return &courseService{
		userQuery:      userQuery,
		sessionService: sessionService,
		crawlerService: crawlerService,
		userDataCache:  userDataCache,
		courseURL:      courseURL,
	}
}

// SetCourseRepository 设置课表仓储（用于延迟注入）
func (s *courseService) SetCourseRepository(repo CourseRepository) {
	s.courseRepo = repo
}

// SetReconciliationTrigger 设置对账触发器
func (s *courseService) SetReconciliationTrigger(trigger ReconciliationTrigger) {
	s.reconciliationTrigger = trigger
}

// GetCourseTableByWeek 获取指定周的课程表
// 策略：先尝试从教务系统获取（2秒超时），超时则返回数据库数据
// 注意：登录失败等认证错误不降级，直接返回错误
func (s *courseService) GetCourseTableByWeek(ctx context.Context, uid int, week int, term string) (*WeekSchedule, error) {
	// 校验参数
	if week > 20 || week < 1 {
		return nil, common.NewAppError(common.CodeJwcInvalidParams, "周次必须在1-20之间")
	}

	if term == "" {
		return nil, common.NewAppError(common.CodeJwcInvalidParams, "学期不能为空")
	}

	re := regexp.MustCompile(`^\d{4}-\d{4}-[12]$`)
	if !re.MatchString(term) {
		return nil, common.NewAppError(common.CodeJwcInvalidParams, "学期格式错误")
	}

	// 获取用户信息
	user, err := s.userQuery.GetUserByUid(ctx, uid)
	if err != nil {
		return nil, common.NewAppError(common.CodeUserNotFound, "用户不存在")
	}

	if user.Sid == "" || user.Spwd == "" {
		return nil, common.NewAppError(common.CodeJwcNotBound, "未绑定教务系统账号")
	}

	// 先查询缓存
	var cachedSchedule WeekSchedule
	if err := s.userDataCache.GetCourseTable(ctx, uid, term, week, &cachedSchedule); err == nil {
		return &cachedSchedule, nil
	}

	// 创建带 15 秒超时的上下文（WebVPN 完整登录链路需 4~8 秒）
	timeoutCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// 尝试从教务系统获取
	schedule, err := s.fetchCourseTableFromJwc(timeoutCtx, uid, user.Sid, user.Spwd, week, term)

	if err == nil {
		// 成功从教务系统获取，异步触发对账更新
		s.triggerAsyncReconciliation(uid)
		return schedule, nil
	}

	// 判断错误类型：登录失败/认证错误不降级，直接返回错误
	if s.isAuthenticationError(err) {
		log.Printf("[GetCourseTableByWeek] 认证错误，清除绑定信息：uid=%d, err=%v", uid, err)
		// 清除用户的教务系统绑定
		if clearErr := s.userQuery.ClearJwcBinding(ctx, uid); clearErr != nil {
			log.Printf("[GetCourseTableByWeek] 清除绑定信息失败：uid=%d, err=%v", uid, clearErr)
		}
		return nil, err
	}

	// 超时或网络错误，尝试从数据库获取
	log.Printf("[GetCourseTableByWeek] 教务系统请求超时/网络错误，尝试从数据库获取：uid=%d, err=%v", uid, err)

	dbSchedule, dbErr := s.getCourseTableFromDatabase(ctx, uid, term, week)
	if dbErr == nil && dbSchedule != nil {
		return dbSchedule, nil
	}

	// 数据库也没有数据，返回原始错误
	return nil, err
}

// GetCourseTableByWeekForSync 获取课表（供对账模块使用，不触发递归同步）
func (s *courseService) GetCourseTableByWeekForSync(ctx context.Context, uid int, week int, term string) (*WeekSchedule, error) {
	// 校验参数
	if week > 20 || week < 1 {
		return nil, common.NewAppError(common.CodeJwcInvalidParams, "周次必须在1-20之间")
	}

	if term == "" {
		return nil, common.NewAppError(common.CodeJwcInvalidParams, "学期不能为空")
	}

	re := regexp.MustCompile(`^\d{4}-\d{4}-[12]$`)
	if !re.MatchString(term) {
		return nil, common.NewAppError(common.CodeJwcInvalidParams, "学期格式错误")
	}

	// 获取用户信息
	user, err := s.userQuery.GetUserByUid(ctx, uid)
	if err != nil {
		return nil, common.NewAppError(common.CodeUserNotFound, "用户不存在")
	}

	if user.Sid == "" || user.Spwd == "" {
		return nil, common.NewAppError(common.CodeJwcNotBound, "未绑定教务系统账号")
	}

	// 直接从教务系统获取，不触发同步
	return s.fetchCourseTableFromJwc(ctx, uid, user.Sid, user.Spwd, week, term)
}

// fetchCourseTableFromJwc 从教务系统获取课表
func (s *courseService) fetchCourseTableFromJwc(ctx context.Context, uid int, sid, spwd string, week int, term string) (*WeekSchedule, error) {
	// 获取会话
	cookies, err := s.getCookiesOrLogin(ctx, uid, sid, spwd)
	if err != nil {
		return nil, err
	}

	// 构造请求（新版教务课表为 GET /xskb/xskb_list.do?viweType=0&zc=..&xnxq01id=..）
	query := url.Values{}
	query.Add("zc", strconv.Itoa(week))
	query.Add("xnxq01id", term)

	sep := "?"
	if strings.Contains(s.courseURL, "?") {
		sep = "&"
	}
	getURL := s.courseURL + sep + query.Encode()

	// 发起请求
	body, err := s.crawlerService.FetchWithCookies(ctx, "GET", getURL, cookies, nil)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	// 解析响应
	schedule, err := s.parseCourseTableFromHTML(body, week)
	if err != nil {
		return nil, err
	}

	// 写入缓存（1小时过期）
	_ = s.userDataCache.CacheCourseTable(ctx, uid, term, week, schedule, time.Hour)

	return schedule, nil
}

// getCourseTableFromDatabase 从数据库获取课表
func (s *courseService) getCourseTableFromDatabase(ctx context.Context, uid int, term string, week int) (*WeekSchedule, error) {
	if s.courseRepo == nil {
		return nil, common.NewAppError(common.CodeInternalError, "课表仓储未配置")
	}

	schedule, err := s.courseRepo.GetCoursesByUidTermWeek(ctx, uid, term, week)
	if err != nil {
		return nil, err
	}

	return schedule, nil
}

// isAuthenticationError 判断是否是认证相关错误
func (s *courseService) isAuthenticationError(err error) bool {
	if err == nil {
		return false
	}

	if appErr, ok := err.(*common.AppError); ok {
		// 明确排除的非认证错误
		switch appErr.Code {
		case common.CodeJwcLoginTimeout, // 超时错误 - 应该降级到数据库
			common.CodeJwcRequestFailed, // 请求失败（网络/服务器错误）- 应该降级
			common.CodeJwcParseFailed:   // 解析失败 - 不是认证问题
			return false
		}

		// 真正的认证错误
		switch appErr.Code {
		case common.CodeJwcLoginFailed,
			common.CodeJwcNotBound,
			common.CodeUnauthorized:
			return true
		}
	}

	errMsg := err.Error()
	authKeywords := []string{
		"用户名或密码错误",
		"密码错误",
		"账号被锁",
		"认证失败",
	}
	for _, keyword := range authKeywords {
		if strings.Contains(errMsg, keyword) {
			return true
		}
	}

	return false
}

// triggerAsyncReconciliation 异步触发对账更新
func (s *courseService) triggerAsyncReconciliation(uid int) {
	if s.reconciliationTrigger == nil {
		return
	}

	go func() {
		ctx := context.Background()
		s.reconciliationTrigger.TriggerCourseSync(ctx, uid)
	}()
}

// getCookiesOrLogin 获取缓存的 cookies 或登录
func (s *courseService) getCookiesOrLogin(ctx context.Context, uid int, sid, spwd string) ([]*http.Cookie, error) {
	cookies, err := s.sessionService.GetCachedCookies(ctx, uid)
	if err != nil {
		return nil, common.NewAppError(common.CodeCacheError, "缓存错误")
	}

	if len(cookies) > 0 {
		return cookies, nil
	}

	if err := s.sessionService.LoginAndCache(ctx, uid, sid, spwd); err != nil {
		return nil, err
	}

	cookies, err = s.sessionService.GetCachedCookies(ctx, uid)
	if err != nil || len(cookies) == 0 {
		return nil, common.NewAppError(common.CodeJwcLoginFailed, "获取会话失败")
	}

	return cookies, nil
}

// parseCourseTableFromHTML 解析课程表 HTML
func (s *courseService) parseCourseTableFromHTML(r io.Reader, requestWeek int) (*WeekSchedule, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "解析HTML失败")
	}

	// 仅在明确被踢回登录页/错误页时报错。
	// 不再硬校验标题：新版标题已由“学期理论课表”改为“个人课表信息”。
	if isLoginOrErrorPage(doc) {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "页面错误")
	}

	// 解析当前周次
	weekNo := requestWeek
	if opt := doc.Find("select#zc option[selected]"); opt.Length() > 0 {
		val, _ := opt.Attr("value")
		if v, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
			weekNo = v
		}
	}

	// 新版教务（layui + qz-* 组件）使用 qz-weeklyTable 结构
	if doc.Find("table.qz-weeklyTable").Length() > 0 {
		return parseCourseTableQZ(doc, weekNo)
	}

	// 初始化 7 天
	days := make([]DaySchedule, 7)
	for i := 0; i < 7; i++ {
		days[i] = DaySchedule{
			Weekday: i + 1,
			Courses: nil,
		}
	}

	// 遍历课表行
	doc.Find("#kbtable tr").Each(func(i int, tr *goquery.Selection) {
		if i == 0 {
			return // 跳过表头
		}

		thText := strings.TrimSpace(tr.Find("th").First().Text())
		if thText == "" || strings.HasPrefix(thText, "备注") {
			return
		}

		startP, endP := parsePeriodRange(thText)
		if startP == 0 && endP == 0 {
			return
		}

		// 遍历一行的 7 列（周一到周日）
		tr.Find("td").Each(func(col int, td *goquery.Selection) {
			weekday := col + 1

			td.Find("div.kbcontent").Each(func(_ int, div *goquery.Selection) {
				name := extractCourseName(div)
				if name == "" || name == "&nbsp;" {
					return
				}

				var teacher, classroom, weeksStr string
				div.Find("font").Each(func(_ int, f *goquery.Selection) {
					title, _ := f.Attr("title")
					text := strings.TrimSpace(f.Text())
					switch {
					case strings.Contains(title, "老师"):
						teacher = text
					case strings.Contains(title, "周次"):
						weeksStr = text
					case strings.Contains(title, "教室"):
						classroom = text
					}
				})

				// 按周次过滤
				if weekNo > 0 && weeksStr != "" && !weekInWeeks(weekNo, weeksStr) {
					return
				}

				c := Course{
					Name:        name,
					Teacher:     teacher,
					Classroom:   classroom,
					Weekday:     weekday,
					StartPeriod: startP,
					EndPeriod:   endP,
				}

				days[weekday-1].Courses = append(days[weekday-1].Courses, c)
			})
		})
	})

	return &WeekSchedule{
		WeekNo:    weekNo,
		Starttime: "",
		Endtime:   "",
		Days:      days,
	}, nil
}

// isLoginOrErrorPage 判断响应是否为登录页或错误页
func isLoginOrErrorPage(doc *goquery.Document) bool {
	title := strings.TrimSpace(doc.Find("title").Text())
	if strings.Contains(title, "登录") || strings.Contains(title, "出错") || strings.Contains(title, "错误") {
		return true
	}
	bodyText := doc.Find("body").Text()
	return strings.Contains(bodyText, "用户没有登录") ||
		strings.Contains(bodyText, "请重新登录") ||
		strings.Contains(bodyText, "正在登录")
}

// parseCourseTableQZ 解析新版教务 qz-weeklyTable 课表
//
// 新版结构：
//
//	table.qz-weeklyTable > tbody > tr
//	  td[name=timeTd]        节次列（.index-title = "第1，2节"，.index-detailtext = "(01、02小节)"）
//	  td[name=kbDataTd]      7 天课程列（可能带 rowspan，导致后续行 td 数量不足）
//	    ul.courselists > li.courselists-item
//	      .qz-hasCourse-title     课程名称
//	      .qz-hasCourse-abbrinfo  "老师:xxx;时间:10-17周[1-2节];地点:xxx"
//	      span[name=kchSpan]      ";课程编号:xxxxx"
//
// 必须做 rowspan 追踪，否则按 td 索引映射星期会错位。
func parseCourseTableQZ(doc *goquery.Document, weekNo int) (*WeekSchedule, error) {
	days := make([]DaySchedule, 7)
	for i := 0; i < 7; i++ {
		days[i] = DaySchedule{Weekday: i + 1, Courses: nil}
	}

	// rowspan 残留追踪：column -> 剩余被占用的行数
	pending := make(map[int]int)

	tbody := doc.Find("table.qz-weeklyTable tbody")
	if tbody.Length() == 0 {
		tbody = doc.Find("table.qz-weeklyTable")
	}

	lastStart, lastEnd := 0, 0
	tbody.Find("tr").Each(func(_ int, tr *goquery.Selection) {
		col := 0
		// 先跳过被上方 rowspan 占用的列
		for pending[col] > 0 {
			pending[col]--
			col++
		}

		tr.Children().Each(func(_ int, cell *goquery.Selection) {
			nodeName := goquery.NodeName(cell)
			if nodeName != "td" && nodeName != "th" {
				return
			}

			name, _ := cell.Attr("name")
			rowspan := 1
			if rs, ok := cell.Attr("rowspan"); ok {
				if v, err := strconv.Atoi(strings.TrimSpace(rs)); err == nil && v > 1 {
					rowspan = v
				}
			}

			if name == "timeTd" || nodeName == "th" {
				// 节次列：优先取 .index-title（"第1，2节"），避免吃进时间等无关数字
				periodText := strings.TrimSpace(cell.Find(".index-title").First().Text())
				if periodText == "" {
					periodText = strings.TrimSpace(cell.Text())
				}
				startP, endP := parsePeriodRange(periodText)
				if startP > 0 {
					lastStart, lastEnd = startP, endP
				}
			} else if name == "kbDataTd" || name == "" {
				// 课程列：col 即星期（0 = 周一）
				if col >= 1 && col <= 7 {
					weekday := col
					cell.Find("li.courselists-item").Each(func(_ int, li *goquery.Selection) {
						c := parseQZCourseItem(li, weekday, lastStart, lastEnd, weekNo)
						if c != nil {
							days[weekday-1].Courses = append(days[weekday-1].Courses, *c)
						}
					})
				}
			}

			// 登记 rowspan
			if rowspan > 1 {
				for c := col; c < col+1; c++ {
					pending[c] += rowspan - 1
				}
			}

			col++
			for pending[col] > 0 {
				pending[col]--
				col++
			}
		})
	})

	return &WeekSchedule{
		WeekNo:    weekNo,
		Starttime: "",
		Endtime:   "",
		Days:      days,
	}, nil
}

// parseQZCourseItem 解析单个课程条目，返回 nil 表示该条目不在当前周或无有效信息
func parseQZCourseItem(li *goquery.Selection, weekday, startP, endP, weekNo int) *Course {
	name := strings.TrimSpace(li.Find(".qz-hasCourse-title").First().Text())
	if name == "" {
		return nil
	}

	abbr := strings.TrimSpace(li.Find(".qz-hasCourse-abbrinfo").First().Text())
	abbr = strings.Join(strings.Fields(abbr), " ") // 压缩空白

	var teacher, classroom, weeksStr string
	for _, seg := range strings.Split(abbr, ";") {
		seg = strings.TrimSpace(seg)
		switch {
		case strings.HasPrefix(seg, "老师:"):
			teacher = strings.TrimPrefix(seg, "老师:")
		case strings.HasPrefix(seg, "时间:"):
			weeksStr = strings.TrimPrefix(seg, "时间:")
		case strings.HasPrefix(seg, "地点:"):
			classroom = strings.TrimPrefix(seg, "地点:")
		}
	}

	// 时间格式 "10-17周[1-2节]" → 周次 "10-17"
	if weeksStr != "" {
		if m := weeksRe.FindStringSubmatch(weeksStr); m != nil {
			weeksStr = m[1]
		}
	}

	// 按周次过滤
	if weekNo > 0 && weeksStr != "" && !weekInWeeks(weekNo, weeksStr) {
		return nil
	}

	return &Course{
		Name:        name,
		Teacher:     teacher,
		Classroom:   classroom,
		Weekday:     weekday,
		StartPeriod: startP,
		EndPeriod:   endP,
	}
}

// weeksRe 从 "10-17周[1-2节]" 中提取周次部分 "10-17"
var weeksRe = regexp.MustCompile(`(\d+(?:-\d+)?(?:,\d+(?:-\d+)?)*)\s*周`)

// parsePeriodRange 解析节次范围
func parsePeriodRange(text string) (int, int) {
	text = strings.TrimSpace(text)
	re := regexp.MustCompile(`\d+`)
	nums := re.FindAllString(text, -1)
	if len(nums) == 0 {
		return 0, 0
	}
	start, _ := strconv.Atoi(nums[0])
	end := start
	if len(nums) > 1 {
		end, _ = strconv.Atoi(nums[len(nums)-1])
	}
	return start, end
}

// extractCourseName 提取课程名称
func extractCourseName(div *goquery.Selection) string {
	name := ""
	div.Contents().EachWithBreak(func(i int, sel *goquery.Selection) bool {
		if goquery.NodeName(sel) == "#text" {
			t := strings.TrimSpace(sel.Text())
			if t != "" {
				name = t
				return false
			}
		}
		if goquery.NodeName(sel) == "br" {
			return false
		}
		return true
	})
	return name
}

// weekInWeeks 判断某周是否在周次范围内
func weekInWeeks(weekNo int, weeksStr string) bool {
	// 去掉 "(周)" 后缀
	if idx := strings.Index(weeksStr, "("); idx >= 0 {
		weeksStr = weeksStr[:idx]
	}
	weeksStr = strings.TrimSpace(weeksStr)
	if weeksStr == "" {
		return true
	}

	parts := strings.Split(weeksStr, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.Contains(part, "-") {
			se := strings.SplitN(part, "-", 2)
			if len(se) != 2 {
				continue
			}
			start, err1 := strconv.Atoi(strings.TrimSpace(se[0]))
			end, err2 := strconv.Atoi(strings.TrimSpace(se[1]))
			if err1 != nil || err2 != nil {
				continue
			}
			if weekNo >= start && weekNo <= end {
				return true
			}
		} else {
			n, err := strconv.Atoi(part)
			if err != nil {
				continue
			}
			if weekNo == n {
				return true
			}
		}
	}
	return false
}
