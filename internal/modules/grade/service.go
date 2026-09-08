package grade

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"sort"
	"spider-go/internal/cache"
	"spider-go/internal/common"
	"spider-go/internal/service"
	"spider-go/internal/shared"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"gorm.io/gorm"
)

// GradeRepository 成绩数据访问接口（用于离线查询）
type GradeRepository interface {
	GetGradesByUid(ctx context.Context, uid int) ([]Grade, error)
	GetLevelGradesByUid(ctx context.Context, uid int) ([]LevelGrade, error)
}

// gradeRepository 成绩仓储实现
type gradeRepository struct {
	db *gorm.DB
}

// NewGradeRepository 创建成绩仓储
func NewGradeRepository(db *gorm.DB) GradeRepository {
	return &gradeRepository{db: db}
}

// GetGradesByUid 从数据库获取用户成绩
func (r *gradeRepository) GetGradesByUid(ctx context.Context, uid int) ([]Grade, error) {
	var dbGrades []struct {
		SerialNo       string
		Term           string
		Code           string
		Subject        string
		Score          string
		Credit         float64
		Gpa            float64
		Status         int
		Property       string
		CourseProperty string
		Flag           string
	}

	err := r.db.WithContext(ctx).
		Table("grades").
		Select("serial_no, term, code, subject, score, credit, gpa, status, property, course_property, flag").
		Where("uid = ? AND is_deleted = ?", uid, false).
		Order("term DESC, code ASC").
		Find(&dbGrades).Error

	if err != nil {
		return nil, err
	}

	grades := make([]Grade, len(dbGrades))
	for i, g := range dbGrades {
		grades[i] = Grade{
			SerialNo:       g.SerialNo,
			Term:           g.Term,
			Code:           g.Code,
			Subject:        g.Subject,
			Score:          g.Score,
			Credit:         g.Credit,
			Gpa:            g.Gpa,
			Status:         g.Status,
			Property:       g.Property,
			CourseProperty: g.CourseProperty,
			Flag:           g.Flag,
		}
	}

	return grades, nil
}

// GetLevelGradesByUid 从数据库获取用户等级考试成绩
func (r *gradeRepository) GetLevelGradesByUid(ctx context.Context, uid int) ([]LevelGrade, error) {
	var dbLevelGrades []struct {
		No         string
		CourseName string
		LevGrade   string
		Time       string
	}

	err := r.db.WithContext(ctx).
		Table("level_exams").
		Select("no, course_name, lev_grade, time").
		Where("uid = ? AND is_deleted = ?", uid, false).
		Order("time DESC").
		Find(&dbLevelGrades).Error

	if err != nil {
		return nil, err
	}

	levelGrades := make([]LevelGrade, len(dbLevelGrades))
	for i, lg := range dbLevelGrades {
		levelGrades[i] = LevelGrade{
			No:         lg.No,
			CourseName: lg.CourseName,
			LevGrade:   lg.LevGrade,
			Time:       lg.Time,
		}
	}

	return levelGrades, nil
}

// ReconciliationTrigger 对账触发器接口（避免循环依赖）
type ReconciliationTrigger interface {
	TriggerGradeSync(ctx context.Context, uid int)
}

// Service 成绩服务接口
type Service interface {
	GetAllGrades(ctx context.Context, uid int) ([]Grade, *GPA, error)
	// GetAllGradesForSync 获取所有成绩（供对账模块使用，不触发递归同步）
	GetAllGradesForSync(ctx context.Context, uid int) ([]Grade, *GPA, error)
	GetGradesByTerm(ctx context.Context, uid int, term string) ([]Grade, *GPA, error)
	GetGradesByYear(ctx context.Context, uid int, year string) ([]Grade, *GPA, error)
	GetLevelGrades(ctx context.Context, uid int) ([]LevelGrade, error)
	// GetLevelGradesForSync 获取等级考试成绩（供对账模块使用，不触发递归同步）
	GetLevelGradesForSync(ctx context.Context, uid int) ([]LevelGrade, error)
	GetRegularGrades(ctx context.Context, uid int, term string, courseId string) (*RegularGrade, error)
	// GetRecentTermsGrades 成绩分析接口
	GetRecentTermsGrades(ctx context.Context, uid int) (*TermsGradesAnalysis, error)
	// GetUserGradeMajorClass 获取用户年级专业班级
	GetUserGradeMajorClass(ctx context.Context, uid int) (*UserDetailedInfo, error)
	// SetReconciliationTrigger 设置对账触发器（用于延迟注入，避免循环依赖）
	SetReconciliationTrigger(trigger ReconciliationTrigger)
}

// gradeService 成绩服务实现
type gradeService struct {
	userQuery             shared.UserQuery
	sessionService        service.SessionService
	crawlerService        service.CrawlerService
	userDataCache         cache.UserDataCache
	configCache           cache.ConfigCache
	gradeRepo             GradeRepository
	reconciliationTrigger ReconciliationTrigger
	gradeURL              string
	gradeLevelURL         string
}

// baseURL 从配置注入的教务 URL 反推站点根地址（scheme://host）。
// 替换此前硬编码的 jwgl 域名，使 campus 与 webvpn 两种模式自动适配。
func (s *gradeService) baseURL() string {
	if u, err := url.Parse(s.gradeURL); err == nil && u.Scheme != "" && u.Host != "" {
		return u.Scheme + "://" + u.Host
	}
	return ""
}

// NewService 创建成绩服务
func NewService(
	userQuery shared.UserQuery,
	sessionService service.SessionService,
	crawlerService service.CrawlerService,
	userDataCache cache.UserDataCache,
	configCache cache.ConfigCache,
	gradeURL string,
	gradeLevelURL string,
) Service {
	return &gradeService{
		userQuery:      userQuery,
		sessionService: sessionService,
		crawlerService: crawlerService,
		userDataCache:  userDataCache,
		configCache:    configCache,
		gradeURL:       gradeURL,
		gradeLevelURL:  gradeLevelURL,
	}
}

// SetGradeRepository 设置成绩仓储（用于延迟注入）
func (s *gradeService) SetGradeRepository(repo GradeRepository) {
	s.gradeRepo = repo
}

// SetReconciliationTrigger 设置对账触发器
func (s *gradeService) SetReconciliationTrigger(trigger ReconciliationTrigger) {
	s.reconciliationTrigger = trigger
}

// GetAllGrades 获取所有成绩
// 策略：先尝试从教务系统获取（2秒超时），超时则返回数据库数据
// 注意：登录失败等认证错误不降级，直接返回错误
func (s *gradeService) GetAllGrades(ctx context.Context, uid int) ([]Grade, *GPA, error) {
	// 获取用户信息
	user, err := s.userQuery.GetUserByUid(ctx, uid)
	if err != nil {
		return nil, nil, common.NewAppError(common.CodeUserNotFound, "用户不存在")
	}

	if user.Sid == "" || user.Spwd == "" {
		return nil, nil, common.NewAppError(common.CodeJwcNotBound, "未绑定教务系统账号")
	}

	// 先查询 Redis 缓存
	type GradeData struct {
		Grades []Grade `json:"grades"`
		GPA    *GPA    `json:"gpa"`
	}
	var cachedData GradeData
	if err := s.userDataCache.GetGrades(ctx, uid, "", &cachedData); err == nil {
		return cachedData.Grades, cachedData.GPA, nil
	}

	// 创建带 15 秒超时的上下文
	// 注意：WebVPN 模式下完整登录链路（auth/start + CAS + callback + 令牌交换 + 强智本地登录）
	// 需 4~8 秒，2 秒超时会误伤正常登录，故放宽到 15 秒；超时后仍降级到数据库。
	timeoutCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// 尝试从教务系统获取成绩
	grades, gpa, err := s.fetchGradesFromJwc(timeoutCtx, uid, user.Sid, user.Spwd)

	if err == nil {
		// 成功从教务系统获取，异步触发对账更新
		s.triggerAsyncReconciliation(uid)
		return grades, gpa, nil
	}

	// 检查是否是"未教评"错误，直接返回，不清除绑定，也不降级到数据库
	if appErr, ok := err.(*common.AppError); ok && appErr.Code == common.CodeJwcNotEvaluated {
		return nil, nil, err
	}

	// 判断错误类型：登录失败/认证错误不降级，直接返回错误
	if s.isAuthenticationError(err) {
		// 注意：此处不再清除数据库中的绑定（sid/spwd）。
		// 保留学号，前端才能弹出"请输入该学号的教务密码"重新绑定弹窗；
		// 只清除已失效的会话缓存，避免后续请求继续复用过期 cookie。
		log.Printf("[GetAllGrades] 认证错误，标记绑定失效（保留学号）：uid=%d, err=%v", uid, err)
		if invErr := s.sessionService.InvalidateSession(ctx, uid); invErr != nil {
			log.Printf("[GetAllGrades] 清除会话缓存失败：uid=%d, err=%v", uid, invErr)
		}
		// 统一为"绑定已失效"，前端据此弹出重新输入教务密码的弹窗
		return nil, nil, common.NewAppError(common.CodeJwcBindExpired, common.MsgJwcBindExpired)
	}

	// 超时或网络错误，尝试从数据库获取
	log.Printf("[GetAllGrades] 教务系统请求超时/网络错误，尝试从数据库获取：uid=%d, err=%v", uid, err)

	dbGrades, dbGPA, dbErr := s.getGradesFromDatabase(ctx, uid)
	if dbErr == nil && len(dbGrades) > 0 {
		// 从数据库获取成功，不触发对账（教务系统超时，触发对账也没意义）
		return dbGrades, dbGPA, nil
	}

	// 数据库也没有数据，返回原始错误
	return nil, nil, err
}

// isAuthenticationError 判断是否是认证相关错误（不应降级到数据库）
func (s *gradeService) isAuthenticationError(err error) bool {
	if err == nil {
		return false
	}

	// 检查是否是 AppError
	if appErr, ok := err.(*common.AppError); ok {
		// 明确排除的非认证错误
		switch appErr.Code {
		case common.CodeJwcLoginTimeout, // 超时错误 - 应该降级到数据库
			common.CodeJwcRequestFailed, // 请求失败（网络/服务器错误）- 应该降级
			common.CodeJwcParseFailed,   // 解析失败 - 不是认证问题
			common.CodeJwcMFARequired:   // MFA验证要求 - 不是密码错误
			return false
		}

		// 真正的认证错误
		switch appErr.Code {
		case common.CodeJwcLoginFailed, // 登录失败（用户名/密码错误）
			common.CodeJwcNotBound,       // 未绑定
			common.CodeJwcSessionExpired, // 教务会话已失效（HTTP 401）
			common.CodeJwcBindExpired,    // 已判定为绑定失效
			common.CodeUnauthorized:      // 未授权
			return true
		}
	}

	// 检查错误信息是否包含登录相关关键字（作为后备检查）
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

// fetchGradesFromJwc 从教务系统获取成绩
func (s *gradeService) fetchGradesFromJwc(ctx context.Context, uid int, sid, spwd string) ([]Grade, *GPA, error) {
	// 获取会话
	cookies, err := s.getCookiesOrLogin(ctx, uid, sid, spwd)
	if err != nil {
		return nil, nil, err
	}

	// 分页拉取全部成绩（新版 JSON 接口，自动兼容旧版 HTML）
	gradeList, err := s.fetchAllGradesJSON(ctx, cookies, "")
	if err != nil {
		return nil, nil, err
	}

	// 计算 GPA
	gpa := s.calculateGPA(gradeList)

	// 写入总成绩缓存（1小时过期）
	data := struct {
		Grades []Grade `json:"grades"`
		GPA    *GPA    `json:"gpa"`
	}{
		Grades: gradeList,
		GPA:    gpa,
	}
	_ = s.userDataCache.CacheGrades(ctx, uid, "", data, time.Hour)

	// 按学期分组并缓存
	s.cacheGradesByTerm(ctx, uid, gradeList)

	return gradeList, gpa, nil
}

// getGradesFromDatabase 从数据库获取成绩
func (s *gradeService) getGradesFromDatabase(ctx context.Context, uid int) ([]Grade, *GPA, error) {
	if s.gradeRepo == nil {
		return nil, nil, common.NewAppError(common.CodeInternalError, "成绩仓储未配置")
	}

	grades, err := s.gradeRepo.GetGradesByUid(ctx, uid)
	if err != nil {
		return nil, nil, err
	}

	if len(grades) == 0 {
		return nil, nil, common.NewAppError(common.CodeJwcParseFailed, "暂无成绩数据")
	}

	// 计算 GPA
	gpa := s.calculateGPA(grades)

	return grades, gpa, nil
}

// triggerAsyncReconciliation 异步触发对账更新
func (s *gradeService) triggerAsyncReconciliation(uid int) {
	if s.reconciliationTrigger == nil {
		return
	}

	// 异步执行，不阻塞当前请求
	go func() {
		ctx := context.Background()
		s.reconciliationTrigger.TriggerGradeSync(ctx, uid)
	}()
}

// GetAllGradesForSync 获取所有成绩（供对账模块使用，不触发递归同步）
// 直接从教务系统获取，不走超时降级逻辑，失败直接返回错误
func (s *gradeService) GetAllGradesForSync(ctx context.Context, uid int) ([]Grade, *GPA, error) {
	// 获取用户信息
	user, err := s.userQuery.GetUserByUid(ctx, uid)
	if err != nil {
		return nil, nil, common.NewAppError(common.CodeUserNotFound, "用户不存在")
	}

	if user.Sid == "" || user.Spwd == "" {
		return nil, nil, common.NewAppError(common.CodeJwcNotBound, "未绑定教务系统账号")
	}

	// 直接从教务系统获取成绩，不触发同步
	grades, gpa, err := s.fetchGradesFromJwc(ctx, uid, user.Sid, user.Spwd)
	if err != nil {
		return nil, nil, err
	}

	return grades, gpa, nil
}

// GetGradesByTerm 根据学期获取成绩
func (s *gradeService) GetGradesByTerm(ctx context.Context, uid int, term string) ([]Grade, *GPA, error) {
	// 校验参数
	re := regexp.MustCompile(`^\d{4}-\d{4}-[12]$`)
	if !re.MatchString(term) {
		return nil, nil, common.NewAppError(common.CodeJwcInvalidParams, "学期格式错误")
	}

	// 获取用户信息
	user, err := s.userQuery.GetUserByUid(ctx, uid)
	if err != nil {
		return nil, nil, common.NewAppError(common.CodeUserNotFound, "用户不存在")
	}

	if user.Sid == "" || user.Spwd == "" {
		return nil, nil, common.NewAppError(common.CodeJwcNotBound, "未绑定教务系统账号")
	}

	// 先查询缓存
	type GradeData struct {
		Grades []Grade `json:"grades"`
		GPA    *GPA    `json:"gpa"`
	}
	var cachedData GradeData
	if err := s.userDataCache.GetGrades(ctx, uid, term, &cachedData); err == nil {
		return cachedData.Grades, cachedData.GPA, nil
	}

	// 获取会话
	cookies, err := s.getCookiesOrLogin(ctx, uid, user.Sid, user.Spwd)
	if err != nil {
		return nil, nil, err
	}

	// 分页拉取指定学期成绩（新版 JSON 接口，kksj 参数与旧版一致）
	gradeList, err := s.fetchAllGradesJSON(ctx, cookies, term)
	if err != nil {
		return nil, nil, err
	}

	// 计算 GPA
	gpa := s.calculateGPA(gradeList)

	// 写入缓存（1小时过期）
	data := GradeData{
		Grades: gradeList,
		GPA:    gpa,
	}
	_ = s.userDataCache.CacheGrades(ctx, uid, term, data, time.Hour)

	return gradeList, gpa, nil
}

// GetGradesByYear 根据学年获取成绩
func (s *gradeService) GetGradesByYear(ctx context.Context, uid int, year string) ([]Grade, *GPA, error) {
	// 校验参数格式：2023-2024
	re := regexp.MustCompile(`^\d{4}-\d{4}$`)
	if !re.MatchString(year) {
		return nil, nil, common.NewAppError(common.CodeJwcInvalidParams, "学年格式错误")
	}

	// 获取用户信息
	user, err := s.userQuery.GetUserByUid(ctx, uid)
	if err != nil {
		return nil, nil, common.NewAppError(common.CodeUserNotFound, "用户不存在")
	}

	if user.Sid == "" || user.Spwd == "" {
		return nil, nil, common.NewAppError(common.CodeJwcNotBound, "未绑定教务系统账号")
	}

	// 先查询缓存
	type GradeData struct {
		Grades []Grade `json:"grades"`
		GPA    *GPA    `json:"gpa"`
	}
	var cachedData GradeData
	cacheKey := year // 使用学年作为缓存键
	if err := s.userDataCache.GetGrades(ctx, uid, cacheKey, &cachedData); err == nil {
		return cachedData.Grades, cachedData.GPA, nil
	}

	// 构造两个学期的标识
	term1 := year + "-1"
	term2 := year + "-2"

	// 获取会话
	cookies, err := s.getCookiesOrLogin(ctx, uid, user.Sid, user.Spwd)
	if err != nil {
		return nil, nil, err
	}

	// 获取第一学期成绩
	gradeList1, err := s.fetchAllGradesJSON(ctx, cookies, term1)
	if err != nil {
		// 如果是"未教评"错误，直接返回错误
		if appErr, ok := err.(*common.AppError); ok && appErr.Code == common.CodeJwcNotEvaluated {
			return nil, nil, err
		}
		// 如果是其他解析错误，说明该学期还没有成绩，使用空数组
		gradeList1 = []Grade{}
	}

	// 获取第二学期成绩
	gradeList2, err := s.fetchAllGradesJSON(ctx, cookies, term2)
	if err != nil {
		// 如果是"未教评"错误，直接返回错误
		if appErr, ok := err.(*common.AppError); ok && appErr.Code == common.CodeJwcNotEvaluated {
			return nil, nil, err
		}
		// 如果是其他解析错误，说明该学期还没有成绩，使用空数组
		gradeList2 = []Grade{}
	}

	// 合并两个学期的成绩
	allGrades := append(gradeList1, gradeList2...)

	// 如果两个学期都没有成绩，返回错误
	if len(allGrades) == 0 {
		return nil, nil, common.NewAppError(common.CodeJwcParseFailed, "该学年暂无成绩数据")
	}

	// 计算学年 GPA（使用相同的计算方法）
	gpa := s.calculateGPA(allGrades)

	// 写入缓存（1小时过期）
	data := GradeData{
		Grades: allGrades,
		GPA:    gpa,
	}
	_ = s.userDataCache.CacheGrades(ctx, uid, cacheKey, data, time.Hour)

	return allGrades, gpa, nil
}

// GetLevelGrades 获取等级考试成绩
// 策略：先尝试从教务系统获取（2秒超时），超时则返回数据库数据
// 注意：登录失败等认证错误不降级，直接返回错误
func (s *gradeService) GetLevelGrades(ctx context.Context, uid int) ([]LevelGrade, error) {
	// 获取用户信息
	user, err := s.userQuery.GetUserByUid(ctx, uid)
	if err != nil {
		return nil, common.NewAppError(common.CodeUserNotFound, "用户不存在")
	}

	if user.Sid == "" || user.Spwd == "" {
		return nil, common.NewAppError(common.CodeJwcNotBound, "未绑定教务系统账号")
	}

	// 创建带 15 秒超时的上下文
	// 注意：WebVPN 模式下完整登录链路（auth/start + CAS + callback + 令牌交换 + 强智本地登录）
	// 需 4~8 秒，2 秒超时会误伤正常登录，故放宽到 15 秒；超时后仍降级到数据库。
	timeoutCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// 尝试从教务系统获取
	levelGrades, err := s.fetchLevelGradesFromJwc(timeoutCtx, uid, user.Sid, user.Spwd)

	if err == nil {
		return levelGrades, nil
	}

	// 判断错误类型：登录失败/认证错误不降级，直接返回错误
	if s.isAuthenticationError(err) {
		// 同上：保留绑定（学号），仅清除失效会话缓存
		log.Printf("[GetLevelGrades] 认证错误，标记绑定失效（保留学号）：uid=%d, err=%v", uid, err)
		if invErr := s.sessionService.InvalidateSession(ctx, uid); invErr != nil {
			log.Printf("[GetLevelGrades] 清除会话缓存失败：uid=%d, err=%v", uid, invErr)
		}
		// 统一为"绑定已失效"，前端据此弹出重新输入教务密码的弹窗
		return nil, common.NewAppError(common.CodeJwcBindExpired, common.MsgJwcBindExpired)
	}

	// 超时或网络错误，尝试从数据库获取
	log.Printf("[GetLevelGrades] 教务系统请求超时/网络错误，尝试从数据库获取：uid=%d, err=%v", uid, err)

	dbLevelGrades, dbErr := s.getLevelGradesFromDatabase(ctx, uid)
	if dbErr == nil && len(dbLevelGrades) > 0 {
		return dbLevelGrades, nil
	}

	// 数据库也没有数据，返回原始错误
	return nil, err
}

// GetLevelGradesForSync 获取等级考试成绩（供对账模块使用，不触发递归同步）
// 直接从教务系统获取，不走超时降级逻辑，失败直接返回错误
func (s *gradeService) GetLevelGradesForSync(ctx context.Context, uid int) ([]LevelGrade, error) {
	// 获取用户信息
	user, err := s.userQuery.GetUserByUid(ctx, uid)
	if err != nil {
		return nil, common.NewAppError(common.CodeUserNotFound, "用户不存在")
	}

	if user.Sid == "" || user.Spwd == "" {
		return nil, common.NewAppError(common.CodeJwcNotBound, "未绑定教务系统账号")
	}

	// 直接从教务系统获取等级考试成绩，不触发同步
	return s.fetchLevelGradesFromJwc(ctx, uid, user.Sid, user.Spwd)
}

// fetchLevelGradesFromJwc 从教务系统获取等级考试成绩
func (s *gradeService) fetchLevelGradesFromJwc(ctx context.Context, uid int, sid, spwd string) ([]LevelGrade, error) {
	// 获取会话
	cookies, err := s.getCookiesOrLogin(ctx, uid, sid, spwd)
	if err != nil {
		return nil, err
	}

	// 分页拉取全部等级成绩（新版 JSON 接口，config 中 URL 已带 ?type=listData）
	const pageSize = 20
	var all []LevelGrade

	for pageNum := 1; ; pageNum++ {
		form := url.Values{}
		form.Set("pageNum", strconv.Itoa(pageNum))
		form.Set("pageSize", strconv.Itoa(pageSize))

		sep := "?"
		if strings.Contains(s.gradeLevelURL, "?") {
			sep = "&"
		}

		// GET 优先（layui 默认），POST 兜底
		getURL := s.gradeLevelURL + sep + form.Encode()
		postForm := url.Values{}
		if idx := strings.Index(s.gradeLevelURL, "?"); idx >= 0 {
			postForm, _ = url.ParseQuery(s.gradeLevelURL[idx+1:])
		}
		postForm.Set("pageNum", strconv.Itoa(pageNum))
		postForm.Set("pageSize", strconv.Itoa(pageSize))

		levelGrades, total, isJSON, err := s.fetchLevelGradesPage(ctx, cookies, getURL, postForm)
		if err != nil {
			return nil, err
		}

		if !isJSON {
			// 旧版 HTML 响应一次性返回全部数据
			all = levelGrades
			break
		}

		all = append(all, levelGrades...)

		if len(levelGrades) < pageSize {
			break
		}
		if total > 0 && len(all) >= total {
			break
		}
		if pageNum >= 100 {
			break // 安全上限
		}
	}

	if len(all) == 0 {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "未找到等级考试数据")
	}
	return all, nil
}

// fetchLevelGradesPage 请求单页等级成绩：GET 优先，POST 兜底；JSON 失败时回退旧版 HTML 解析
func (s *gradeService) fetchLevelGradesPage(ctx context.Context, cookies []*http.Cookie, getURL string, postForm url.Values) ([]LevelGrade, int, bool, error) {
	type attempt struct {
		method string
		target string
		form   url.Values
	}
	tries := []attempt{
		{"GET", getURL, nil},
		{"POST", s.gradeLevelURL, postForm},
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

		if looksLikeJSON(raw) {
			levelGrades, total, jerr := parseLevelGradesJSONBytes(raw)
			if jerr != nil {
				continue
			}
			return levelGrades, total, true, nil
		}

		// 旧版 HTML 表格响应
		levelGrades, herr := s.parseLevelGradesFromHTML(bytes.NewReader(raw))
		if herr == nil {
			return levelGrades, 0, false, nil
		}
		return nil, 0, false, herr
	}

	return nil, 0, false, common.NewAppError(common.CodeHttpRequestFailed, "等级成绩接口请求失败")
}

// getLevelGradesFromDatabase 从数据库获取等级考试成绩
func (s *gradeService) getLevelGradesFromDatabase(ctx context.Context, uid int) ([]LevelGrade, error) {
	if s.gradeRepo == nil {
		return nil, common.NewAppError(common.CodeInternalError, "成绩仓储未配置")
	}

	levelGrades, err := s.gradeRepo.GetLevelGradesByUid(ctx, uid)
	if err != nil {
		return nil, err
	}

	return levelGrades, nil
}

func (s *gradeService) GetRegularGrades(ctx context.Context, uid int, term string, courseId string) (*RegularGrade, error) {
	// 先从redis数据库里获取平时分链接，没有则返回空
	// 这个方法要求用户必须先调用过 GetGradesByTerm 或 GetGradesByYear，才能获取到缓存的平时分链接
	// 这样可以防止爬虫等非法访问
	link, err := s.getRegularGradeLink(ctx, uid, term, courseId)
	if err != nil {
		// 如果缓存不存在，直接返回错误，不再查表和登录
		return nil, err
	}

	// 获取会话cookies (从缓存中获取，不需要查数据库)
	cookies, err := s.sessionService.GetCachedCookies(ctx, uid)
	if err != nil || len(cookies) == 0 {
		return nil, common.NewAppError(common.CodeUnauthorized, "会话已过期，请重新获取成绩")
	}

	// 创建 HTTP 客户端并手动发起请求以获取状态码
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
	}

	// 将 cookies 添加到 jar
	parsedURL, _ := url.Parse(link)
	client.Jar.SetCookies(parsedURL, cookies)

	// 发起请求
	resp, err := client.Get(link)
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcRequestFailed, "获取平时分失败")
	}
	defer resp.Body.Close()

	// 检查状态码
	if resp.StatusCode == 404 {
		return nil, common.NewAppError(common.CodeJwcNoRegularGrade, "该课程没有平时分数据")
	}

	if resp.StatusCode != 200 {
		return nil, common.NewAppError(common.CodeJwcRequestFailed, "获取平时分失败")
	}

	// 解析平时分HTML
	regularGrade, err := s.parseRegularGradeFromHTML(resp.Body)
	if err != nil {
		return nil, err
	}

	return regularGrade, nil
}

// GetUserGradeMajorClass 获取用户年级、学院、专业、班级信息
func (s *gradeService) GetUserGradeMajorClass(ctx context.Context, uid int) (*UserDetailedInfo, error) {
	// 获取用户信息
	user, err := s.userQuery.GetUserByUid(ctx, uid)
	if err != nil {
		return nil, common.NewAppError(common.CodeUserNotFound, "用户不存在")
	}

	if user.Sid == "" || user.Spwd == "" {
		return nil, common.NewAppError(common.CodeJwcNotBound, "未绑定教务系统账号")
	}

	// 先查询缓存
	var cachedInfo UserDetailedInfo
	if err := s.userDataCache.GetStudentInfo(ctx, uid, &cachedInfo); err == nil {
		// 如果缓存存在且姓名不为空，直接返回
		if cachedInfo.Name != "" {
			return &cachedInfo, nil
		}
		// 否则重新获取（旧缓存没有姓名字段）
	}

	// 获取会话
	cookies, err := s.getCookiesOrLogin(ctx, uid, user.Sid, user.Spwd)
	if err != nil {
		return nil, err
	}

	// 构造学生信息卡片URL
	studentInfoURL := s.baseURL() + "/jsxsd/grxx/xsxx"

	// 发起请求
	body, err := s.crawlerService.FetchWithCookies(ctx, "GET", studentInfoURL, cookies, nil)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	// 解析学生信息
	info, err := s.parseStudentInfoFromHTML(body, user.Sid)
	if err != nil {
		return nil, err
	}

	// 写入缓存（24小时过期，学生信息很少变化）
	_ = s.userDataCache.CacheStudentInfo(ctx, uid, info, 24*time.Hour)

	return info, nil
}

// parseStudentInfoFromHTML 从HTML中解析学生信息（年级、学院、专业、班级、姓名）
func (s *gradeService) parseStudentInfoFromHTML(r io.Reader, sid string) (*UserDetailedInfo, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "解析HTML失败")
	}

	// 从学号中提取年级（例如：20232343 -> 2023）
	var grade string
	if len(sid) >= 4 {
		grade = sid[:4]
	}

	// 查找包含院系、专业、班级信息的表格行
	// HTML结构：
	// <tr>
	//   <td>院系：计算机与数学学院</td>
	//   <td>专业：计算机科学与技术</td>
	//   <td>学制：4</td>
	//   <td>班级：2023计算机科学与技术2班</td>
	//   <td>学号：20232343</td>
	// </tr>
	var college, major, class, name string

	// 查找包含"院系："的单元格
	doc.Find("td").Each(func(i int, sel *goquery.Selection) {
		text := strings.TrimSpace(sel.Text())

		// 提取院系
		if strings.HasPrefix(text, "院系：") {
			college = strings.TrimPrefix(text, "院系：")
		}

		// 提取专业
		if strings.HasPrefix(text, "专业：") {
			major = strings.TrimPrefix(text, "专业：")
		}

		// 提取班级
		if strings.HasPrefix(text, "班级：") {
			class = strings.TrimPrefix(text, "班级：")
		}
	})

	// 提取姓名：从学籍卡片表格中获取
	// HTML结构：
	// <tr>
	//   <td>姓名</td>
	//   <td>&nbsp;费德</td>
	//   ...
	// </tr>
	doc.Find("tr").Each(func(i int, tr *goquery.Selection) {
		tds := tr.Find("td")
		if tds.Length() >= 2 {
			firstTd := strings.TrimSpace(tds.Eq(0).Text())
			if firstTd == "姓名" {
				// 获取第二个td的内容作为姓名
				nameText := strings.TrimSpace(tds.Eq(1).Text())
				// 去除 &nbsp; 转换后的空格
				nameText = strings.ReplaceAll(nameText, "\u00A0", "")
				nameText = strings.TrimSpace(nameText)
				if nameText != "" && name == "" {
					name = nameText
				}
			}
		}
	})

	// 检查是否成功解析到必要信息
	if college == "" || major == "" || class == "" {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "未能解析到完整的学生信息")
	}

	return &UserDetailedInfo{
		Grade:   grade,
		Class:   class,
		Major:   major,
		Collage: college,
		Name:    name,
	}, nil
}

// getCookiesOrLogin 获取缓存的 cookies 或登录
func (s *gradeService) getCookiesOrLogin(ctx context.Context, uid int, sid, spwd string) ([]*http.Cookie, error) {
	cookies, err := s.sessionService.GetCachedCookies(ctx, uid)
	if err != nil {
		return nil, common.NewAppError(common.CodeCacheError, "缓存错误")
	}

	if len(cookies) > 0 {
		return cookies, nil
	}

	// 尝试登录教务系统
	if err := s.sessionService.LoginAndCache(ctx, uid, sid, spwd); err != nil {
		// 密码错误等认证类失败 → 转换为"绑定已失效"，让前端提示重新输入密码
		return nil, common.ToBindExpired(err)
	}

	// 登录成功后从缓存获取 cookies
	cookies, err = s.sessionService.GetCachedCookies(ctx, uid)
	if err != nil {
		return nil, common.NewAppError(common.CodeCacheError, "读取缓存失败")
	}
	if len(cookies) == 0 {
		// 登录声称成功了但缓存没有 cookies，
		// 说明教务系统返回了 302 但目标系统不可达（例如校园网外访问教务系统）
		return nil, common.NewAppError(common.CodeJwcRequestFailed, "教务系统网络连接异常，请稍后重试")
	}

	return cookies, nil
}

// parseGradesFromHTML 解析成绩 HTML
func (s *gradeService) parseGradesFromHTML(r io.Reader) ([]Grade, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "解析HTML失败")
	}

	// 检测是否包含"未教评"提示信息
	// 教务系统在未完成教评时可能返回提示文本而非数据表格
	bodyText := doc.Text()
	if strings.Contains(bodyText, "未教评") || strings.Contains(bodyText, "教学评价") && strings.Contains(bodyText, "未完成") {
		return nil, common.NewAppError(common.CodeJwcNotEvaluated, "您还有未完成的教学评价，请先完成教评后再查询成绩")
	}

	table := doc.Find("#dataList")
	if table.Length() == 0 {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "未找到成绩数据")
	}

	var grades []Grade
	table.Find("tr").Each(func(i int, tr *goquery.Selection) {
		tds := tr.Find("td")
		if tds.Length() < 13 {
			return
		}

		trim := func(s string) string {
			return strings.TrimSpace(strings.ReplaceAll(s, "\u00A0", ""))
		}

		serialNo := trim(tds.Eq(0).Text())
		term := trim(tds.Eq(1).Text())
		code := trim(tds.Eq(2).Text())
		subject := trim(tds.Eq(3).Text())
		score := trim(tds.Eq(4).Text())
		credit := parseFloatSafe(trim(tds.Eq(5).Text()))
		gpa := parseFloatSafe(trim(tds.Eq(7).Text()))
		flag := trim(tds.Eq(8).Text()) // 成绩标志（缓考等）

		// 处理 status
		statusNormalRegexp := regexp.MustCompile(`^正常考试$|.*重.*`)
		var status int
		if statusNormalRegexp.MatchString(trim(tds.Eq(10).Text())) {
			status = 0
		} else {
			status = 1
		}

		property := trim(tds.Eq(11).Text())

		if subject == "" && score == "" {
			return
		}

		grades = append(grades, Grade{
			SerialNo: serialNo,
			Term:     term,
			Code:     code,
			Subject:  subject,
			Score:    score,
			Credit:   credit,
			Gpa:      gpa,
			Status:   status,
			Property: property,
			Flag:     flag,
		})
	})

	if len(grades) == 0 {
		return nil, common.NewAppError(common.CodeJwcNotEvaluated, "未查询到成绩数据，您可能还有未完成的教学评价，请先完成教评后再查询成绩")
	}

	return grades, nil
}

// ============ 新版教务 JSON 成绩接口 ============
// 新版教务的成绩查询页面 /jsxsd/kscj/cjcx_frm 为 layui 异步表格，
// 数据通过 /jsxsd/kscj/cjcx_list 以 JSON 返回：
//
//	请求：GET（layui table.render 默认）或 POST，参数 pageNum / pageSize=20 / kksj / kcxz / kcmc / xsfs=all
//	响应：{code, msg, count, data:[{xnxqid,kch,kc_mc,ksdw,zcjstr,cjbs,xf,zxs,jd,bcxq,ksfs,ksxz,kcsx,kcxzmc,txklb,bz}]}

// statusNormalRe 与旧版 HTML 解析保持一致的状态判定
var statusNormalRe = regexp.MustCompile(`^正常考试$|.*重.*`)

// cjcxResp layui 分页响应包装。code 兼容 0 / 200 / 缺省三种情况
type cjcxResp struct {
	Code  json.Number `json:"code"`
	Msg   string      `json:"msg"`
	Count json.Number `json:"count"`
	Data  []cjcxRow   `json:"data"`
}

// cjcxRow 成绩行。字段统一用 json.RawMessage 接收，兼容字符串 / 数字两种类型
// （教务后端部分字段返回 "85"，部分返回 85，无法预知，必须容错）
type cjcxRow struct {
	Xnxqid   json.RawMessage `json:"xnxqid"`   // 学期
	Xnxq01id json.RawMessage `json:"xnxq01id"` // 学期（备用字段名）
	Kch      json.RawMessage `json:"kch"`      // 课程编号
	KcMc     json.RawMessage `json:"kc_mc"`    // 课程名称
	Ksdw     json.RawMessage `json:"ksdw"`     // 开课单位
	Zcjstr   json.RawMessage `json:"zcjstr"`   // 成绩
	Cjbs     json.RawMessage `json:"cjbs"`     // 成绩标识（缓考等）
	Xf       json.RawMessage `json:"xf"`       // 学分
	Zxs      json.RawMessage `json:"zxs"`      // 总学时
	Jd       json.RawMessage `json:"jd"`       // 绩点
	Ksfs     json.RawMessage `json:"ksfs"`     // 考核方式
	Ksxz     json.RawMessage `json:"ksxz"`     // 考试性质
	Kcsx     json.RawMessage `json:"kcsx"`     // 课程属性
	Kcxzmc   json.RawMessage `json:"kcxzmc"`   // 课程性质名称（必修/选修）
	Txklb    json.RawMessage `json:"txklb"`    // 通选课类别
	Bz       json.RawMessage `json:"bz"`       // 备注
}

// jsonStr 容错提取字符串：兼容 "abc" 与 abc 两种 JSON 形态，空值返回 ""
func jsonStr(raw json.RawMessage) string {
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

// jsonFloat 容错提取数值：兼容 2.0 与 "2.0" 两种形态
func jsonFloat(raw json.RawMessage) float64 {
	s := jsonStr(raw)
	if s == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// looksLikeJSON 判断响应体是否为 JSON（layui 响应以 { 开头）
func looksLikeJSON(raw []byte) bool {
	trimmed := bytes.TrimLeft(bytes.TrimSpace(raw), "\uFEFF")
	return len(trimmed) > 0 && trimmed[0] == '{'
}

// fetchAllGradesJSON 分页拉取全部成绩（新版 JSON 接口），自动兼容旧版 HTML 单页响应
func (s *gradeService) fetchAllGradesJSON(ctx context.Context, cookies []*http.Cookie, kksj string) ([]Grade, error) {
	const pageSize = 20
	var all []Grade

	for pageNum := 1; ; pageNum++ {
		form := url.Values{}
		form.Set("kksj", kksj)
		form.Set("kcxz", "")
		form.Set("kcmc", "")
		form.Set("xsfs", "all")
		form.Set("pageNum", strconv.Itoa(pageNum))
		form.Set("pageSize", strconv.Itoa(pageSize))

		grades, total, isJSON, err := s.fetchGradesPage(ctx, cookies, form)
		if err != nil {
			return nil, err
		}

		if !isJSON {
			// 旧版 HTML 响应一次性返回全部数据，无需分页
			return grades, nil
		}

		all = append(all, grades...)

		// 终止条件双保险：不足一页 或 已达 count
		if len(grades) < pageSize {
			break
		}
		if total > 0 && len(all) >= total {
			break
		}
		if pageNum >= 100 {
			break // 安全上限，防止异常响应导致死循环
		}
	}

	if len(all) == 0 {
		return nil, common.NewAppError(common.CodeJwcNotEvaluated, "未查询到成绩数据，您可能还有未完成的教学评价，请先完成教评后再查询成绩")
	}
	return all, nil
}

// fetchGradesPage 请求单页成绩：GET 优先（layui 默认），POST 兜底；JSON 失败时回退旧版 HTML 解析
// 返回值：成绩列表、总数（仅 JSON 有意义）、是否 JSON 响应、错误
func (s *gradeService) fetchGradesPage(ctx context.Context, cookies []*http.Cookie, form url.Values) ([]Grade, int, bool, error) {
	type attempt struct {
		method string
		target string
		form   url.Values
	}
	tries := []attempt{
		{"GET", s.gradeURL + "?" + form.Encode(), nil},
		{"POST", s.gradeURL, form},
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

		if looksLikeJSON(raw) {
			grades, total, jerr := parseGradesJSONBytes(raw)
			if jerr != nil {
				continue
			}
			return grades, total, true, nil
		}

		// 旧版 HTML 表格响应（含"未教评"提示检测）
		grades, herr := s.parseGradesFromHTML(bytes.NewReader(raw))
		if herr == nil {
			return grades, 0, false, nil
		}
		// "未教评"等业务错误直接返回；其余（如新版返回 layui 页面外壳，
		// HTML 里无成绩表格）继续尝试下一种请求方式（POST）
		if appErr, ok := herr.(*common.AppError); ok && appErr.Code == common.CodeJwcNotEvaluated {
			return nil, 0, false, herr
		}
	}

	return nil, 0, false, common.NewAppError(common.CodeHttpRequestFailed, "成绩接口请求失败")
}

// parseGradesJSONBytes 解析 layui JSON 成绩响应
func parseGradesJSONBytes(raw []byte) ([]Grade, int, error) {
	var resp cjcxResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, 0, err
	}

	// code 兼容：0 / 200 / 缺省 均视为成功
	codeStr := strings.TrimSpace(resp.Code.String())
	if codeStr != "" && codeStr != "0" && codeStr != "200" {
		msg := strings.TrimSpace(resp.Msg)
		if strings.Contains(msg, "教评") || strings.Contains(msg, "评价") {
			return nil, 0, common.NewAppError(common.CodeJwcNotEvaluated, "您还有未完成的教学评价，请先完成教评后再查询成绩")
		}
		if msg == "" {
			msg = "成绩接口返回异常"
		}
		return nil, 0, common.NewAppError(common.CodeJwcParseFailed, msg)
	}

	grades := make([]Grade, 0, len(resp.Data))
	for i, row := range resp.Data {
		subject := strings.TrimSpace(jsonStr(row.KcMc))
		score := strings.TrimSpace(jsonStr(row.Zcjstr))
		if subject == "" && score == "" {
			continue
		}

		credit := jsonFloat(row.Xf)
		gpa := jsonFloat(row.Jd)

		term := strings.TrimSpace(jsonStr(row.Xnxqid))
		if term == "" {
			term = strings.TrimSpace(jsonStr(row.Xnxq01id))
		}

		// 状态判定与旧版一致：考试性质匹配 "正常考试" 或含 "重" 视为 0
		status := 1
		if statusNormalRe.MatchString(strings.TrimSpace(jsonStr(row.Ksxz))) {
			status = 0
		}

		grades = append(grades, Grade{
			SerialNo:       strconv.Itoa(i + 1),
			Term:           term,
			Code:           strings.TrimSpace(jsonStr(row.Kch)),
			Subject:        subject,
			Score:          score,
			Credit:         credit,
			Gpa:            gpa,
			Status:         status,
			Property:       strings.TrimSpace(jsonStr(row.Kcxzmc)),
			CourseProperty: strings.TrimSpace(jsonStr(row.Kcsx)),
			Flag:           strings.TrimSpace(jsonStr(row.Cjbs)),
		})
	}

	total := 0
	if t, err := resp.Count.Int64(); err == nil && t > 0 {
		total = int(t)
	}
	return grades, total, nil
}

// levelGradeResp layui 分页响应包装
type levelGradeResp struct {
	Code  json.RawMessage `json:"code"`
	Msg   string          `json:"msg"`
	Count json.RawMessage `json:"count"`
	Data  []levelGradeRow `json:"data"`
}

// levelGradeRow 等级成绩行。字段统一用 json.RawMessage 兼容字符串 / 数字两种类型
type levelGradeRow struct {
	Skkcdjmc json.RawMessage `json:"skkcdjmc"` // 考级课程(等级)
	Kssj     json.RawMessage `json:"kssj"`     // 考级开始时间
	Jssj     json.RawMessage `json:"jssj"`     // 考级结束时间
	Bz       json.RawMessage `json:"bz"`       // 备注
	Shzt     json.RawMessage `json:"shzt"`     // 审核状态
	Fslbscj  json.RawMessage `json:"fslbscj"`  // 笔试成绩（百分制）
	Fslsjcj  json.RawMessage `json:"fslsjcj"`  // 机试成绩（百分制）
	Fslcj    json.RawMessage `json:"fslcj"`    // 总成绩（百分制）
	Skcjdj   json.RawMessage `json:"skcjdj"`   // 非百分制成绩
	Djlbscj  json.RawMessage `json:"djlbscj"`  // 笔试成绩（等级制）
	Djlsjcj  json.RawMessage `json:"djlsjcj"`  // 机试成绩（等级制）
	Djlzcj   json.RawMessage `json:"djlzcj"`   // 总成绩（等级制）
}

// parseLevelGradesJSONBytes 解析 layui JSON 等级成绩响应
func parseLevelGradesJSONBytes(raw []byte) ([]LevelGrade, int, error) {
	var resp levelGradeResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, 0, err
	}

	// code 兼容：0 / 200 / 缺省 均视为成功
	codeStr := strings.TrimSpace(jsonStr(resp.Code))
	if codeStr != "" && codeStr != "0" && codeStr != "200" {
		msg := strings.TrimSpace(resp.Msg)
		if msg == "" {
			msg = "等级成绩接口返回异常"
		}
		return nil, 0, common.NewAppError(common.CodeJwcParseFailed, msg)
	}

	levelGrades := make([]LevelGrade, 0, len(resp.Data))
	for i, row := range resp.Data {
		courseName := strings.TrimSpace(jsonStr(row.Skkcdjmc))
		if courseName == "" {
			continue
		}

		// 成绩取值与旧版一致：优先百分制总成绩，其次等级制总成绩，最后非百分制成绩
		levGrade := strings.TrimSpace(jsonStr(row.Fslcj))
		if levGrade == "" {
			levGrade = strings.TrimSpace(jsonStr(row.Djlzcj))
		}
		if levGrade == "" {
			levGrade = strings.TrimSpace(jsonStr(row.Skcjdj))
		}

		levelGrades = append(levelGrades, LevelGrade{
			No:         strconv.Itoa(i + 1),
			CourseName: courseName,
			LevGrade:   levGrade,
			Time:       strings.TrimSpace(jsonStr(row.Kssj)),
		})
	}

	total := 0
	if t := jsonFloat(resp.Count); t > 0 {
		total = int(t)
	}
	return levelGrades, total, nil
}

// parseLevelGradesFromHTML 解析等级考试成绩 HTML
func (s *gradeService) parseLevelGradesFromHTML(r io.Reader) ([]LevelGrade, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "解析HTML失败")
	}

	table := doc.Find("#dataList")
	if table.Length() == 0 {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "未找到等级考试数据")
	}

	var levelGrades []LevelGrade
	table.Find("tr").Each(func(i int, s *goquery.Selection) {
		tds := s.Find("td")
		if tds.Length() < 9 {
			return
		}

		trim := func(s string) string {
			return strings.ReplaceAll(s, "\u00A0", "")
		}

		no := trim(tds.Eq(0).Text())
		courseName := trim(tds.Eq(1).Text())

		// 处理分数类和等级类成绩
		var levGrade string
		if trim(tds.Eq(4).Text()) == "" {
			levGrade = trim(tds.Eq(7).Text())
		} else {
			levGrade = trim(tds.Eq(4).Text())
		}

		time := trim(tds.Eq(8).Text())

		levelGrades = append(levelGrades, LevelGrade{
			No:         no,
			CourseName: courseName,
			LevGrade:   levGrade,
			Time:       time,
		})
	})

	return levelGrades, nil
}

// calculateGPA 计算 GPA
func (s *gradeService) calculateGPA(gradeArray []Grade) *GPA {
	distinct := s.distinctGrades(gradeArray)

	var (
		sumScore   float64
		sumGp      float64
		sumCredit  float64
		num2       int
		sumScore2  float64
		sumCredit2 float64
	)

	for _, g := range distinct {
		// 只算"课程属性 kcsx=必修"的课程（公选/任选/通识等课程属性不计，见 IsAwardEligible）
		if !IsAwardEligible(g) {
			continue
		}

		// 跳过缓考成绩，不计入GPA计算
		if g.Flag == "缓考" {
			continue
		}

		scoreText := g.Score

		// BasicPoint
		if g.Status == 0 {
			gradeD := mapGradeToScoreForBasic(scoreText)
			sumScore2 += gradeD * g.Credit
			sumCredit2 += g.Credit
		}

		// GPA & APF
		numericScore, isNum := parseNumeric(scoreText)

		if isNum && g.Status == 0 && numericScore >= 59.9 {
			sumScore += numericScore
			gp := s.getCourseGp(g, scoreText)
			sumGp += gp * g.Credit
			sumCredit += g.Credit
			num2++
		} else {
			if g.Status == 0 && !isNum {
				gp := s.getCourseGp(g, scoreText)
				score := gp*10.0 + 50.0
				sumScore += score
				sumGp += gp * g.Credit
				sumCredit += g.Credit
				num2++
			} else {
				if g.Status == 1 && isNum && numericScore >= 59.9 {
					sumScore += 60.0
					gp := s.getCourseGp(g, scoreText)
					sumGp += gp * 1.0
					sumCredit += g.Credit
					num2++
				} else if g.Status == 1 && !isNum && (scoreText == "及格" || scoreText == "合格") {
					gp := s.getCourseGp(g, scoreText)
					sumScore += 60.0
					sumGp += gp * 1.0
					sumCredit += g.Credit
					num2++
				} else if g.Status == 1 && !isNum && (scoreText == "不及格" || scoreText == "不合格") {
					sumCredit += g.Credit
					num2++
				} else if g.Status == 1 && isNum && numericScore <= 59.9 {
					sumCredit += g.Credit
					num2++
				} else {
					sumCredit += g.Credit
					num2++
					if isNum {
						sumScore += numericScore
					} else {
						log.Println("特殊成绩样式:", scoreText)
					}
				}
			}
		}
	}

	var gpa, apf, basic float64
	if sumCredit != 0 {
		gpa = sumGp / sumCredit
	}
	if num2 != 0 {
		apf = sumScore / float64(num2)
	}
	if sumCredit2 != 0 {
		basic = sumScore2 / sumCredit2
	}

	if math.IsNaN(gpa) {
		gpa = 0
	}
	if math.IsNaN(apf) {
		apf = 0
	}
	if math.IsNaN(basic) {
		basic = 0
	}

	return &GPA{
		AverageGPA:   round3(gpa),
		AverageScore: round3(apf),
		BasicScore:   round3(basic),
	}
}

// distinctGrades 去重成绩
func (s *gradeService) distinctGrades(grades []Grade) []Grade {
	m := make(map[string]Grade)
	for _, g := range grades {
		key := g.SerialNo + "|" + g.Code + "|" + g.Term

		// 如果key已存在，优先保留非缓考的成绩
		if existing, exists := m[key]; exists {
			// 如果现有记录是缓考，但新记录不是，则替换
			if existing.Flag == "缓考" && g.Flag != "缓考" {
				m[key] = g
			}
			// 否则保留现有记录（包括：现有不是缓考，或两者都是缓考，或两者都不是缓考）
		} else {
			m[key] = g
		}
	}
	res := make([]Grade, 0, len(m))
	for _, g := range m {
		res = append(res, g)
	}
	return res
}

// getCourseGp 获取课程绩点
func (s *gradeService) getCourseGp(g Grade, scoreText string) float64 {
	if !math.IsNaN(g.Gpa) && g.Gpa > 0 {
		return g.Gpa
	}
	return handelGp(scoreText)
}

// 辅助函数
func parseFloatSafe(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	s = strings.ReplaceAll(s, ",", "")
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func parseNumeric(s string) (float64, bool) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func mapGradeToScoreForBasic(scoreText string) float64 {
	switch scoreText {
	case "不及格", "不合格":
		return 50.0
	case "及格", "合格":
		return 60.0
	case "中":
		return 70.0
	case "良":
		return 80.0
	case "优":
		return 90.0
	default:
		if v, ok := parseNumeric(scoreText); ok {
			return v
		}
		return 0
	}
}

func handelGp(scoreText string) float64 {
	switch scoreText {
	case "不及格", "不合格":
		return 0
	case "及格", "合格":
		return 1.0
	case "中":
		return 2.0
	case "良":
		return 3.0
	case "优":
		return 4.0
	}

	score, ok := parseNumeric(scoreText)
	if !ok {
		log.Println("额外成绩样式:", scoreText)
		return 0
	}

	raw := (score - 50.0) / 10.0
	raw = round3(raw)
	if raw <= 0.1 {
		return 0
	}
	return raw
}

func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}

// GetRecentTermsGrades 获取成绩分析（覆盖用户全部有成绩记录的学期）
func (s *gradeService) GetRecentTermsGrades(ctx context.Context, uid int) (*TermsGradesAnalysis, error) {
	// 1. 获取所有成绩（实时教务抓取优先，超时/网络错误自动降级数据库）
	allGrades, overallGPA, err := s.GetAllGrades(ctx, uid)
	if err != nil {
		return nil, err
	}

	// 2. 从成绩中提取全部学期（去重、按时间倒序：最新在前），不依赖"当前学期"配置，
	//    这样既避免"当前学期未设置"导致接口报错，也自然覆盖全部学期。
	terms := extractTermsDesc(allGrades)
	if len(terms) == 0 {
		return &TermsGradesAnalysis{
			CurrentTerm:   "",
			Semesters:     []TermGradesData{},
			OverallGPA:    overallGPA,
			TrendAnalysis: s.analyzeTrend([]TermGradesData{}),
		}, nil
	}

	// 3. 按学期分组成绩（返回扁平统计字段，对齐前端成绩分析 tab）
	termsData := make([]TermGradesData, 0)
	for _, term := range terms {
		termGrades := s.filterGradesByTerm(allGrades, term)

		var termGPA *GPA
		if len(termGrades) == 0 {
			// 如果该学期没有成绩，返回空统计
			termGPA = &GPA{
				AverageGPA:   0,
				AverageScore: 0,
				BasicScore:   0,
			}
		} else {
			// 计算该学期的 GPA
			termGPA = s.calculateGPA(termGrades)
		}

		// 计算该学期总学分与课程数
		var totalCredit float64
		courseCount := 0
		for _, g := range termGrades {
			if g.Subject == "" && g.Score == "" {
				continue
			}
			totalCredit += g.Credit
			courseCount++
		}

		termsData = append(termsData, TermGradesData{
			Term:         term,
			AverageGPA:   termGPA.AverageGPA,
			AverageScore: termGPA.AverageScore,
			TotalCredit:  totalCredit,
			CourseCount:  courseCount,
		})
	}

	// 4. 趋势分析
	trendAnalysis := s.analyzeTrend(termsData)

	return &TermsGradesAnalysis{
		CurrentTerm:   terms[0],
		Semesters:     termsData,
		OverallGPA:    overallGPA,
		TrendAnalysis: trendAnalysis,
	}, nil
}

// extractTermsDesc 从成绩列表中提取去重后的学期，并按时间倒序（最新在前）返回。
// 例如 [2023-2024-1, 2025-2026-2, 2023-2024-2] -> [2025-2026-2, 2023-2024-2, 2023-2024-1]
func extractTermsDesc(grades []Grade) []string {
	seen := make(map[string]struct{}, 0)
	terms := make([]string, 0, len(grades))
	for _, g := range grades {
		t := strings.TrimSpace(g.Term)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		terms = append(terms, t)
	}

	// 学期格式：YYYY-YYYY-N，按其起止学年与学期号倒序比较
	sort.Slice(terms, func(i, j int) bool {
		return termCompare(terms[i], terms[j]) > 0
	})
	return terms
}

// termCompare 比较两个学期字符串 a、b 的时间先后；返回正数表示 a 比 b 新，负数表示 a 比 b 旧。
// 格式：YYYY-YYYY-N（N 为 1/2）。
func termCompare(a, b string) int {
	pi := strings.Split(a, "-")
	pj := strings.Split(b, "-")
	parse := func(p []string) (int, int) {
		y, _ := strconv.Atoi(p[0])
		sem := 0
		if len(p) == 3 {
			sem, _ = strconv.Atoi(p[2])
		}
		return y, sem
	}
	ai, asi := parse(pi)
	bi, bsi := parse(pj)
	// 先比起止学年的结束年（第二段），等同用结束年；再比学期号
	ae, _ := strconv.Atoi(pi[1])
	be, _ := strconv.Atoi(pj[1])
	if ae != be {
		return ae - be
	}
	if asi != bsi {
		return asi - bsi
	}
	return ai - bi
}

// filterGradesByTerm 按学期过滤成绩
func (s *gradeService) filterGradesByTerm(grades []Grade, term string) []Grade {
	filtered := make([]Grade, 0)
	for _, grade := range grades {
		if grade.Term == term {
			filtered = append(filtered, grade)
		}
	}
	return filtered
}

// analyzeTrend 分析成绩趋势
func (s *gradeService) analyzeTrend(termsData []TermGradesData) *TrendAnalysis {
	if len(termsData) < 2 {
		return &TrendAnalysis{
			GPATrend:     "数据不足",
			ScoreTrend:   "数据不足",
			BestTerm:     "",
			BestTermGPA:  0,
			WorstTerm:    "",
			WorstTermGPA: 0,
		}
	}

	// 找出最好和最差的学期
	var bestTerm, worstTerm string
	var bestGPA, worstGPA float64 = 0, 999.0
	firstValidGPA := true

	gpas := make([]float64, 0)
	for _, data := range termsData {
		// 检查该学期是否有有效的 GPA 数据
		if data.AverageGPA == 0 && data.AverageScore == 0 {
			continue
		}

		gpa := data.AverageGPA
		gpas = append(gpas, gpa)

		// 初始化最好和最差的 GPA
		if firstValidGPA {
			bestGPA = gpa
			worstGPA = gpa
			bestTerm = data.Term
			worstTerm = data.Term
			firstValidGPA = false
			continue
		}

		// 更新最好的学期
		if gpa > bestGPA {
			bestGPA = gpa
			bestTerm = data.Term
		}

		// 更新最差的学期
		if gpa < worstGPA {
			worstGPA = gpa
			worstTerm = data.Term
		}
	}

	// 如果没有有效数据
	if len(gpas) == 0 {
		return &TrendAnalysis{
			GPATrend:     "暂无数据",
			ScoreTrend:   "暂无数据",
			BestTerm:     "",
			BestTermGPA:  0,
			WorstTerm:    "",
			WorstTermGPA: 0,
		}
	}

	// 分析趋势（比较最近两个学期）
	gpaTrend := "稳定"
	scoreTrend := "稳定"

	if len(gpas) >= 2 {
		// gpas[0] 是当前学期，gpas[1] 是上一学期
		diff := gpas[0] - gpas[1]
		if diff > 0.1 {
			gpaTrend = "上升"
			scoreTrend = "上升"
		} else if diff < -0.1 {
			gpaTrend = "下降"
			scoreTrend = "下降"
		}
	}

	return &TrendAnalysis{
		GPATrend:     gpaTrend,
		ScoreTrend:   scoreTrend,
		BestTerm:     bestTerm,
		BestTermGPA:  bestGPA,
		WorstTerm:    worstTerm,
		WorstTermGPA: worstGPA,
	}
}

// extractAndCacheRegularGradeLinks 从HTML中提取平时分链接并缓存到Redis Hash
// 教务系统返回的HTML已包含所有学期的平时分链接，因此函数内部自行解析所有学期。
func (s *gradeService) extractAndCacheRegularGradeLinks(ctx context.Context, uid int, htmlContent string) {
	// 按学期分组的平时分链接
	termRegularGradeLinks := make(map[string]map[string]interface{})

	// 平时分链接在显示表格（id="dataList"）的HTML注释中
	// HTML结构:
	// <table id="dataList">
	//   <tr>
	//     <td>1</td>
	//     <td>2023-2024-1</td>                                                  <- 学期
	//     <td align="left">130010050</td>                                       <- 课程编号
	//     <td align="left">林学概论</td>
	//     <!-- <td><a href="/jsxsd/kscj/pscj_list.do?...">85</a></td> -->      <- 平时分链接在注释中
	//     <td><font color="blue">85</font></td>
	//     ...
	//   </tr>
	// </table>

	// 提取 id="dataList" 表格中的所有 <tr> 标签
	dataListRegex := regexp.MustCompile(`(?s)<table[^>]*id="dataList"[^>]*>.*?</table>`)
	dataListMatch := dataListRegex.FindString(htmlContent)
	if dataListMatch == "" {
		return
	}

	// 提取表格中的所有 <tr> (排除表头)
	trRegex := regexp.MustCompile(`(?s)<tr\s+>.*?</tr>`)
	trMatches := trRegex.FindAllString(dataListMatch, -1)

	// 预编译正则表达式
	termRegex := regexp.MustCompile(`<td[^>]*>\s*(\d{4}-\d{4}-[12])\s*</td>`)
	courseCodeRegex := regexp.MustCompile(`<td\s+align="left"[^>]*>\s*(\d+)\s*</td>`)
	regularLinkRegex := regexp.MustCompile(`/jsxsd/kscj/pscj_list\.do\?[^'">\s]+`)

	for _, trContent := range trMatches {
		// 在当前tr中提取学期
		termMatches := termRegex.FindStringSubmatch(trContent)
		if len(termMatches) < 2 {
			continue
		}
		courseTerm := termMatches[1]

		// 在当前tr中提取课程编号（第一个 align="left" 的td）
		codeMatches := courseCodeRegex.FindStringSubmatch(trContent)
		if len(codeMatches) < 2 {
			continue
		}
		courseCode := codeMatches[1]

		// 在当前tr中提取平时分链接（在HTML注释中）
		linkMatches := regularLinkRegex.FindStringSubmatch(trContent)
		if len(linkMatches) > 0 {
			regularLink := linkMatches[0]

			// 按学期分组
			if termRegularGradeLinks[courseTerm] == nil {
				termRegularGradeLinks[courseTerm] = make(map[string]interface{})
			}
			termRegularGradeLinks[courseTerm][courseCode] = regularLink
		}
	}

	// 如果没有提取到链接,不执行缓存操作
	if len(termRegularGradeLinks) == 0 {
		return
	}

	// 无论是否指定学期，都将HTML中所有学期的平时分链接分别缓存
	// 因为教务系统返回的HTML包含所有学期的平时分链接
	for courseTerm, links := range termRegularGradeLinks {
		_ = s.userDataCache.CacheRegularGrades(ctx, uid, courseTerm, links, time.Hour)
	}
}

// getRegularGradeLink 根据课程编号获取平时分链接
// 从Redis Hash中查询指定课程的平时分链接
func (s *gradeService) getRegularGradeLink(ctx context.Context, uid int, term string, courseCode string) (string, error) {
	// 从缓存中获取平时分链接映射
	var regularGradeLinks map[string]string
	err := s.userDataCache.GetRegularGrades(ctx, uid, term, &regularGradeLinks)
	if err != nil {
		return "", common.NewAppError(common.CodeCacheError, "平时分链接缓存不存在")
	}

	// 查找指定课程编号的链接
	link, exists := regularGradeLinks[courseCode]
	if !exists {
		return "", common.NewAppError(common.CodeJwcNoRegularGrade, "该课程没有平时分数据")
	}
	link = s.baseURL() + link
	return link, nil
}

// parseRegularGradeFromHTML 解析平时分 HTML
func (s *gradeService) parseRegularGradeFromHTML(r io.Reader) (*RegularGrade, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "解析平时分HTML失败")
	}

	table := doc.Find("#dataList")
	if table.Length() == 0 {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "未找到平时分数据")
	}

	// 查找数据行 (跳过表头)
	dataRow := table.Find("tr").Eq(1)
	if dataRow.Length() == 0 {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "未找到平时分数据行")
	}

	tds := dataRow.Find("td")
	if tds.Length() < 5 {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "平时分数据格式错误")
	}

	trim := func(s string) string {
		return strings.TrimSpace(strings.ReplaceAll(s, "\u00A0", ""))
	}

	// 解析数据
	// HTML 结构:
	// <tr>
	//   <td>1</td>                    <!-- 序号 -->
	//   <td>100</td>                  <!-- 期末成绩 -->
	//   <td>40%</td>                  <!-- 期末成绩比例 -->
	//   <td>69.17</td>                <!-- 平时成绩 -->
	//   <td>60%</td>                  <!-- 平时成绩比例 -->
	//   <td>82</td>                   <!-- 总成绩 -->
	// </tr>

	regularGrade := &RegularGrade{
		FinalExamScore: trim(tds.Eq(1).Text()),
		FinalExamRatio: trim(tds.Eq(2).Text()),
		RegularScore:   trim(tds.Eq(3).Text()),
		RegularRatio:   trim(tds.Eq(4).Text()),
		FinalScore:     trim(tds.Eq(5).Text()),
	}

	return regularGrade, nil
}

// cacheGradesByTerm 将成绩按学期和学年分组并缓存
func (s *gradeService) cacheGradesByTerm(ctx context.Context, uid int, grades []Grade) {
	type GradeData struct {
		Grades []Grade `json:"grades"`
		GPA    *GPA    `json:"gpa"`
	}

	// 按学期分组
	termGrades := make(map[string][]Grade)
	// 按学年分组
	yearGrades := make(map[string][]Grade)

	for _, grade := range grades {
		if grade.Term == "" {
			continue
		}

		// 按学期分组
		termGrades[grade.Term] = append(termGrades[grade.Term], grade)

		// 提取学年 (例如: 2024-2025-1 -> 2024-2025)
		termRegex := regexp.MustCompile(`^(\d{4}-\d{4})-[12]$`)
		if matches := termRegex.FindStringSubmatch(grade.Term); len(matches) > 1 {
			year := matches[1]
			yearGrades[year] = append(yearGrades[year], grade)
		}
	}

	// 缓存每个学期的成绩
	for term, gradeList := range termGrades {
		gpa := s.calculateGPA(gradeList)
		data := GradeData{
			Grades: gradeList,
			GPA:    gpa,
		}
		_ = s.userDataCache.CacheGrades(ctx, uid, term, data, time.Hour)
	}

	// 缓存每个学年的成绩
	for year, gradeList := range yearGrades {
		gpa := s.calculateGPA(gradeList)
		data := GradeData{
			Grades: gradeList,
			GPA:    gpa,
		}
		_ = s.userDataCache.CacheGrades(ctx, uid, year, data, time.Hour)
	}
}
