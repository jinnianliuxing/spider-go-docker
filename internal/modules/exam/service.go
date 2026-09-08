package exam

import (
	"bytes"
	"context"
	"encoding/json"
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

// ExamRepository 考试数据访问接口（用于离线查询）
type ExamRepository interface {
	GetExamsByUidAndTerm(ctx context.Context, uid int, term string) ([]ExamArrangement, error)
}

// ReconciliationTrigger 对账触发器接口（避免循环依赖）
type ReconciliationTrigger interface {
	TriggerExamSync(ctx context.Context, uid int)
}

// Service 考试服务接口
type Service interface {
	GetAllExams(ctx context.Context, uid int, term string) ([]ExamArrangement, error)
	// GetAllExamsForSync 获取考试安排（供对账模块使用，不触发递归同步）
	GetAllExamsForSync(ctx context.Context, uid int, term string) ([]ExamArrangement, error)
	// SetExamRepository 设置考试仓储（用于延迟注入）
	SetExamRepository(repo ExamRepository)
	// SetReconciliationTrigger 设置对账触发器（用于延迟注入）
	SetReconciliationTrigger(trigger ReconciliationTrigger)
}

// examService 考试服务实现
type examService struct {
	userQuery             shared.UserQuery
	sessionService        service.SessionService
	crawlerService        service.CrawlerService
	userDataCache         cache.UserDataCache
	examRepo              ExamRepository
	reconciliationTrigger ReconciliationTrigger
	examURL               string
}

// NewService 创建考试服务
func NewService(
	userQuery shared.UserQuery,
	sessionService service.SessionService,
	crawlerService service.CrawlerService,
	userDataCache cache.UserDataCache,
	examURL string,
) Service {
	return &examService{
		userQuery:      userQuery,
		sessionService: sessionService,
		crawlerService: crawlerService,
		userDataCache:  userDataCache,
		examURL:        examURL,
	}
}

// SetExamRepository 设置考试仓储（用于延迟注入）
func (s *examService) SetExamRepository(repo ExamRepository) {
	s.examRepo = repo
}

// SetReconciliationTrigger 设置对账触发器
func (s *examService) SetReconciliationTrigger(trigger ReconciliationTrigger) {
	s.reconciliationTrigger = trigger
}

// GetAllExams 获取考试安排
// 策略：先尝试从教务系统获取（2秒超时），超时则返回数据库数据
// 注意：登录失败等认证错误不降级，直接返回错误
func (s *examService) GetAllExams(ctx context.Context, uid int, term string) ([]ExamArrangement, error) {
	// 校验参数
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
	var cachedExams []ExamArrangement
	if err := s.userDataCache.GetExams(ctx, uid, term, &cachedExams); err == nil {
		return cachedExams, nil
	}

	// 创建带 15 秒超时的上下文（WebVPN 完整登录链路需 4~8 秒）
	timeoutCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// 尝试从教务系统获取
	exams, err := s.fetchExamsFromJwc(timeoutCtx, uid, user.Sid, user.Spwd, term)

	if err == nil {
		// 成功从教务系统获取，异步触发对账更新
		s.triggerAsyncReconciliation(uid)
		return exams, nil
	}

	// 判断错误类型：登录失败/认证错误不降级，直接返回错误
	if s.isAuthenticationError(err) {
		// 保留绑定（学号），仅清除失效会话缓存，便于前端弹出重新绑定弹窗
		log.Printf("[GetAllExams] 认证错误，标记绑定失效（保留学号）：uid=%d, err=%v", uid, err)
		if invErr := s.sessionService.InvalidateSession(ctx, uid); invErr != nil {
			log.Printf("[GetAllExams] 清除会话缓存失败：uid=%d, err=%v", uid, invErr)
		}
		return nil, common.NewAppError(common.CodeJwcBindExpired, common.MsgJwcBindExpired)
	}

	// 超时或网络错误，尝试从数据库获取
	log.Printf("[GetAllExams] 教务系统请求超时/网络错误，尝试从数据库获取：uid=%d, err=%v", uid, err)

	dbExams, dbErr := s.getExamsFromDatabase(ctx, uid, term)
	if dbErr == nil && len(dbExams) > 0 {
		return dbExams, nil
	}

	// 数据库也没有数据，返回原始错误
	return nil, err
}

// GetAllExamsForSync 获取考试安排（供对账模块使用，不触发递归同步）
func (s *examService) GetAllExamsForSync(ctx context.Context, uid int, term string) ([]ExamArrangement, error) {
	// 校验参数
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
	return s.fetchExamsFromJwc(ctx, uid, user.Sid, user.Spwd, term)
}

// fetchExamsFromJwc 从教务系统获取考试安排
// 新版教务页面为 /jsxsd/xsks/xsksap_query（layui 异步表格），数据接口仍为 /jsxsd/xsks/xsksap_list，返回 JSON
func (s *examService) fetchExamsFromJwc(ctx context.Context, uid int, sid, spwd, term string) ([]ExamArrangement, error) {
	// 获取会话
	cookies, err := s.getCookiesOrLogin(ctx, uid, sid, spwd)
	if err != nil {
		return nil, err
	}

	// 分页拉取全部考试安排（GET 优先，POST 兜底，兼容旧版 HTML）
	const pageSize = 20
	var all []ExamArrangement

	for pageNum := 1; ; pageNum++ {
		form := url.Values{}
		form.Set("xnxqid", term)
		form.Set("pageNum", strconv.Itoa(pageNum))
		form.Set("pageSize", strconv.Itoa(pageSize))

		exams, total, isJSON, err := s.fetchExamsPage(ctx, cookies, form)
		if err != nil {
			// 会话失效：清掉过期 cookie，下次请求会用库里的密码重新登录
			if appErr, ok := err.(*common.AppError); ok && appErr.Code == common.CodeJwcBindExpired {
				_ = s.sessionService.InvalidateSession(ctx, uid)
			}
			return nil, err
		}
		if err != nil {
			return nil, err
		}

		if !isJSON {
			// 旧版 HTML 响应一次性返回全部数据
			all = exams
			break
		}

		all = append(all, exams...)

		if len(exams) < pageSize {
			break
		}
		if total > 0 && len(all) >= total {
			break
		}
		if pageNum >= 100 {
			break // 安全上限
		}
	}

	// 写入缓存（1小时过期）
	_ = s.userDataCache.CacheExams(ctx, uid, term, all, time.Hour)

	return all, nil
}

// fetchExamsPage 请求单页考试安排：GET 优先，POST 兜底；JSON 失败时回退旧版 HTML 解析
func (s *examService) fetchExamsPage(ctx context.Context, cookies []*http.Cookie, form url.Values) ([]ExamArrangement, int, bool, error) {
	type attempt struct {
		method string
		target string
		form   url.Values
	}
	tries := []attempt{
		{"GET", s.examURL + "?" + form.Encode(), nil},
		{"POST", s.examURL, form},
	}

	for _, a := range tries {
		body, err := s.crawlerService.FetchWithCookies(ctx, a.method, a.target, cookies, a.form)
		if err != nil {
			continue
		}
		raw, rerr := io.ReadAll(body)
		body.Close()
		if rerr != nil {
			continue
		}

		if looksLikeJSONExam(raw) {
			exams, total, jerr := parseExamsJSONBytes(raw)
			if jerr != nil {
				continue
			}
			return exams, total, true, nil
		}

		// 旧版 HTML 表格响应
		exams, herr := s.parseExamArrangementFromHTML(bytes.NewReader(raw))
		if herr == nil {
			return exams, 0, false, nil
		}
		return nil, 0, false, herr
	}

	return nil, 0, false, common.NewAppError(common.CodeHttpRequestFailed, "考试安排接口请求失败")
}

// getExamsFromDatabase 从数据库获取考试安排
func (s *examService) getExamsFromDatabase(ctx context.Context, uid int, term string) ([]ExamArrangement, error) {
	if s.examRepo == nil {
		return nil, common.NewAppError(common.CodeInternalError, "考试仓储未配置")
	}

	exams, err := s.examRepo.GetExamsByUidAndTerm(ctx, uid, term)
	if err != nil {
		return nil, err
	}

	return exams, nil
}

// isAuthenticationError 判断是否是认证相关错误
func (s *examService) isAuthenticationError(err error) bool {
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
			common.CodeJwcSessionExpired,
			common.CodeJwcBindExpired,
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
		"登录状态已失效",
	}
	for _, keyword := range authKeywords {
		if strings.Contains(errMsg, keyword) {
			return true
		}
	}

	return false
}

// triggerAsyncReconciliation 异步触发对账更新
func (s *examService) triggerAsyncReconciliation(uid int) {
	if s.reconciliationTrigger == nil {
		return
	}

	go func() {
		ctx := context.Background()
		s.reconciliationTrigger.TriggerExamSync(ctx, uid)
	}()
}

// getCookiesOrLogin 获取缓存的 cookies 或登录
func (s *examService) getCookiesOrLogin(ctx context.Context, uid int, sid, spwd string) ([]*http.Cookie, error) {
	cookies, err := s.sessionService.GetCachedCookies(ctx, uid)
	if err != nil {
		return nil, common.NewAppError(common.CodeCacheError, "缓存错误")
	}

	if len(cookies) > 0 {
		return cookies, nil
	}

	if err := s.sessionService.LoginAndCache(ctx, uid, sid, spwd); err != nil {
		// 密码错误等认证类失败 → 转换为"绑定已失效"，让前端提示重新输入密码
		return nil, common.ToBindExpired(err)
	}

	cookies, err = s.sessionService.GetCachedCookies(ctx, uid)
	if err != nil || len(cookies) == 0 {
		return nil, common.NewAppError(common.CodeJwcLoginFailed, "获取会话失败")
	}

	return cookies, nil
}

// parseExamArrangementFromHTML 解析考试安排 HTML
func (s *examService) parseExamArrangementFromHTML(r io.Reader) ([]ExamArrangement, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "解析HTML失败")
	}

	title := strings.TrimSpace(doc.Find("title").Text())
	if title != "我的考试 - 考试安排查询" {
		// 会话失效被踢回登录页 → 与页面改版区分开，单独返回"绑定已失效"
		if isSessionExpiredDoc(doc, title) {
			return nil, common.NewAppError(common.CodeJwcBindExpired, common.MsgJwcBindExpired)
		}
		return nil, common.NewAppError(common.CodeJwcParseFailed, "页面错误")
	}

	table := doc.Find("#dataList")
	if table.Length() == 0 {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "未找到考试安排数据")
	}

	rows := table.Find("tr")
	if rows.Length() <= 1 {
		return nil, nil // 只有表头，无数据
	}

	// 检查是否显示"未查询到数据"
	if strings.Contains(rows.Eq(1).Text(), "未查询到数据") {
		return nil, nil
	}

	var exams []ExamArrangement

	rows.Each(func(i int, tr *goquery.Selection) {
		if i == 0 {
			return // 跳过表头
		}

		tds := tr.Find("td")
		if tds.Length() < 9 {
			return
		}

		trim := func(s string) string {
			s = strings.TrimSpace(s)
			s = strings.ReplaceAll(s, "\u00A0", "")
			return s
		}

		exams = append(exams, ExamArrangement{
			SerialNo:  trim(tds.Eq(0).Text()),
			ClassNo:   trim(tds.Eq(2).Text()),
			ClassName: trim(tds.Eq(3).Text()),
			Time:      trim(tds.Eq(4).Text()),
			Place:     trim(tds.Eq(5).Text()),
			Execution: trim(tds.Eq(8).Text()),
		})
	})

	return exams, nil
}

// ============ 新版教务 JSON 考试接口 ============
// 响应：{code, msg, count, data:[...]}
// 字段（来自 layui cols 配置）：xqmc 校区 / ksxq 考试校区 / kssj 考试时间 /
// js_mc 考场 / kch 课程编号 / kskcmc 课程名称 / jsxm 授课教师 / zwh 座位号 /
// zkzh 准考证号 / ksccmc 考试场次 / bzywmc 备注 / skrs 上课人数

// looksLikeJSONExam 判断响应体是否为 JSON
func looksLikeJSONExam(raw []byte) bool {
	trimmed := bytes.TrimLeft(bytes.TrimSpace(raw), "\uFEFF")
	return len(trimmed) > 0 && trimmed[0] == '{'
}

// examResp layui 分页响应包装
type examResp struct {
	Code  json.RawMessage `json:"code"`
	Msg   string          `json:"msg"`
	Count json.RawMessage `json:"count"`
	Data  []examRow       `json:"data"`
}

// examRow 考试行。字段统一用 json.RawMessage 兼容字符串 / 数字两种类型
type examRow struct {
	Xqmc   json.RawMessage `json:"xqmc"`   // 校区
	Ksxq   json.RawMessage `json:"ksxq"`   // 考试校区
	Kssj   json.RawMessage `json:"kssj"`   // 考试时间
	JsMc   json.RawMessage `json:"js_mc"`  // 考场
	Kch    json.RawMessage `json:"kch"`    // 课程编号
	Kskcmc json.RawMessage `json:"kskcmc"` // 课程名称
	Jsxm   json.RawMessage `json:"jsxm"`   // 授课教师
	Zwh    json.RawMessage `json:"zwh"`    // 座位号
	Zkzh   json.RawMessage `json:"zkzh"`   // 准考证号
	Ksccmc json.RawMessage `json:"ksccmc"` // 考试场次
	Bzywmc json.RawMessage `json:"bzywmc"` // 备注
	Skrs   json.RawMessage `json:"skrs"`   // 上课人数
}

// jsonStrExam 容错提取字符串：兼容 "abc" 与 abc 两种 JSON 形态
func jsonStrExam(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
	}
	return string(raw)
}

// jsonIntExam 容错提取整数
func jsonIntExam(raw json.RawMessage) int {
	s := jsonStrExam(raw)
	if s == "" {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// parseExamsJSONBytes 解析 layui JSON 考试响应
func parseExamsJSONBytes(raw []byte) ([]ExamArrangement, int, error) {
	var resp examResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, 0, err
	}

	// code 兼容：0 / 200 / 缺省 均视为成功
	codeStr := strings.TrimSpace(jsonStrExam(resp.Code))
	if codeStr != "" && codeStr != "0" && codeStr != "200" {
		msg := strings.TrimSpace(resp.Msg)
		if msg == "" {
			msg = "考试安排接口返回异常"
		}
		return nil, 0, common.NewAppError(common.CodeJwcParseFailed, msg)
	}

	exams := make([]ExamArrangement, 0, len(resp.Data))
	for i, row := range resp.Data {
		className := strings.TrimSpace(jsonStrExam(row.Kskcmc))
		if className == "" {
			continue
		}
		exams = append(exams, ExamArrangement{
			SerialNo:  strconv.Itoa(i + 1),
			ClassNo:   strings.TrimSpace(jsonStrExam(row.Kch)),
			ClassName: className,
			Time:      strings.TrimSpace(jsonStrExam(row.Kssj)),
			Place:     strings.TrimSpace(jsonStrExam(row.JsMc)),
			SeatNo:    strings.TrimSpace(jsonStrExam(row.Zwh)),
			ExamType:  strings.TrimSpace(jsonStrExam(row.Ksccmc)),
			Execution: strings.TrimSpace(jsonStrExam(row.Bzywmc)), // 新版无"执行情况"列，以备注代替
		})
	}

	return exams, jsonIntExam(resp.Count), nil
}

// isSessionExpiredDoc 判断响应是否因登录态失效被踢回登录页
// （保守判定：仅在出现明确未登录信号时命中，避免把页面改版误判成绑定失效）
func isSessionExpiredDoc(doc *goquery.Document, title string) bool {
	if strings.Contains(title, "登录") {
		return true
	}
	bodyText := doc.Find("body").Text()
	return strings.Contains(bodyText, "用户没有登录") ||
		strings.Contains(bodyText, "请重新登录") ||
		strings.Contains(bodyText, "正在登录")
}
