package evaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"spider-go/internal/cache"
	"spider-go/internal/common"
	"spider-go/internal/service"
	"spider-go/internal/shared"
	"strings"
	"sync"
	"time"
)

type Service interface {
	GetEvaluationInfo(ctx context.Context, uid int) (*[]EvaluationInfo, error)
	LoginAndCacheEvaluation(ctx context.Context, uid int, sid, spwd string) error
	// 新增接口
	GetEvaluationTasks(ctx context.Context, uid int) (*[]EvaluationTask, error)
	GetEvaluationCourses(ctx context.Context, uid int, taskId int) (*[]EvaluationCourse, error)
	GetEvaluationQuestions(ctx context.Context, uid int, indexId, pjCourseType string) (*[]EvaluationQuestion, error)
	// GetEvaluateResultId 取某门课的评教结果记录(含每题答案记录 id)，提交评教前必须调用
	GetEvaluateResultId(ctx context.Context, uid int, pjjgId int) (*[]EvaluationResultItem, error)
	SubmitEvaluation(ctx context.Context, uid int, submitData []EvaluationSubmitRequest) error
	// 自动评教接口
	AutoEvaluation(ctx context.Context, uid int) (*AutoEvaluationResult, error)
	// 查看评教状态
	GetEvaluationStatus(ctx context.Context, uid int) (*EvaluationStatus, error)
}

type evaluationService struct {
	userQuery       shared.UserQuery
	sessionService  service.SessionService
	evaluationCache cache.EvaluationCache
	sessionCache    cache.SessionCache // 用于删除 TGC
	// 教评系统相关 URL
	evaluationInfoURL string
	casRedirectURL    string // 教评系统 CAS 回调 URL（用于获取 ticket）
	doLoginURL        string // 教评系统 doLogin API
	apiBaseURL        string // 教评业务接口基础地址，如 https://<host>/api/xspj/xspj
	timeout           time.Duration
	cacheExpire       time.Duration
	// clients 缓存每个用户「登录教评系统时用的那个 http.Client」。
	// 必须复用它：webvpn 模式下业务接口依赖该 client cookie jar 里的 webvpn-token，
	// 新建裸 client 会因缺少该 cookie 被网关拦截。
	clients sync.Map // uid -> *http.Client
}

func NewService(
	userQuery shared.UserQuery,
	sessionService service.SessionService,
	evaluationCache cache.EvaluationCache,
	sessionCache cache.SessionCache,
	evaluationInfoURL string,
	casRedirectURL string,
	doLoginURL string,
	apiBaseURL string,
) Service {
	return &evaluationService{
		userQuery:         userQuery,
		sessionService:    sessionService,
		evaluationCache:   evaluationCache,
		sessionCache:      sessionCache,
		evaluationInfoURL: evaluationInfoURL,
		casRedirectURL:    casRedirectURL,
		doLoginURL:        doLoginURL,
		apiBaseURL:        strings.TrimRight(apiBaseURL, "/"),
		timeout:           30 * time.Second,
		cacheExpire:       30 * time.Minute, // 教评 accessToken 缓存 30 分钟
	}
}

func (s *evaluationService) GetEvaluationInfo(ctx context.Context, uid int) (*[]EvaluationInfo, error) {
	user, err := s.userQuery.GetUserByUid(ctx, uid)
	if err != nil {
		return nil, common.NewAppError(common.CodeInternalError, "查询数据库错误")
	}

	accessToken, client, err := s.getSession(ctx, uid, user.Sid, user.Spwd)
	if err != nil {
		return nil, err
	}

	// 使用 accessToken 请求教评信息
	body, err := s.fetchWithAccessToken(ctx, client, "POST", s.evaluationInfoURL, accessToken, nil)
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcRequestFailed, "发送教评请求失败")
	}
	defer body.Close()

	// 解析响应
	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "读取响应失败")
	}

	var apiResp EvaluationAPIResponse
	if err := json.Unmarshal(bodyBytes, &apiResp); err != nil {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "解析教评响应失败")
	}

	if apiResp.Code != 200 {
		return nil, common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("教评系统返回错误: %s", apiResp.Msg))
	}

	// 转换为统一格式
	result := make([]EvaluationInfo, 0, len(apiResp.Data.List))
	for _, item := range apiResp.Data.List {
		status := "未评"
		if item.Status == 1 {
			status = "已评"
		}

		result = append(result, EvaluationInfo{
			TaskId:       item.TaskId,
			TaskName:     item.TaskName,
			CourseName:   item.CourseName,
			TeacherName:  item.TeacherName,
			Status:       status,
			EvaluateType: item.EvaluateType,
			BeginTime:    item.BeginTime,
			EndTime:      item.EndTime,
		})
	}

	return &result, nil
}

// LoginAndCacheEvaluation 登录教评系统并缓存 accessToken
// 流程：复用 SessionService 登录获取带 TGC 的 client → 用 TGC 访问教评系统重定向链 → 获取 userToken → doLogin 获取 accessToken
func (s *evaluationService) LoginAndCacheEvaluation(ctx context.Context, uid int, sid, spwd string) error {
	// 1. 使用 SessionService 登录 CAS，获取带 TGC cookie 的 client
	client, err := s.sessionService.LoginAndGetClient(ctx, sid, spwd)
	if err != nil {
		return err
	}

	// 2. 用这个 client 访问教评系统的 CAS 重定向 URL
	// CAS 服务器会识别 TGC 并签发 ticket，然后重定向到教评系统
	return s.followRedirectsAndGetToken(ctx, client, s.casRedirectURL, uid)
}

// followRedirectsAndGetToken 跟随重定向链，获取 userToken 并调用 doLogin 获取 accessToken
func (s *evaluationService) followRedirectsAndGetToken(ctx context.Context, client *http.Client, startURL string, uid int) error {
	currentURL := startURL
	var userToken string

	// 跟随重定向，最多 10 次
	for i := 0; i < 10; i++ {
		req, err := http.NewRequest("GET", currentURL, nil)
		if err != nil {
			return common.NewAppError(common.CodeJwcLoginFailed, "构造请求失败")
		}
		req.Header.Set("User-Agent", "Mozilla/5.0")

		resp, err := client.Do(req)
		if err != nil {
			return common.NewAppError(common.CodeJwcLoginFailed, "请求失败")
		}

		// 检查是否是最终页面（包含 userToken 的重定向）
		location := resp.Header.Get("Location")

		// 检查当前 URL 或 Location 是否包含 userToken
		if strings.Contains(currentURL, "userToken=") {
			parsedURL, _ := url.Parse(currentURL)
			userToken = parsedURL.Query().Get("userToken")
		} else if strings.Contains(location, "userToken=") {
			parsedURL, _ := url.Parse(location)
			userToken = parsedURL.Query().Get("userToken")
		}

		resp.Body.Close()

		if userToken != "" {
			break
		}

		if resp.StatusCode/100 != 3 || location == "" {
			// 非重定向，尝试从响应中提取
			break
		}

		// 解析相对 URL
		base, _ := url.Parse(currentURL)
		next, _ := url.Parse(location)
		currentURL = base.ResolveReference(next).String()
	}

	if userToken == "" {
		return common.NewAppError(common.CodeJwcLoginFailed, "未能获取 userToken")
	}

	// 7. 调用 doLogin 换取 accessToken
	accessToken, err := s.doLoginWithToken(ctx, client, userToken)
	if err != nil {
		return err
	}

	// 8. 缓存 accessToken
	if err := s.evaluationCache.SetAccessToken(ctx, uid, accessToken, s.cacheExpire); err != nil {
		return common.NewAppError(common.CodeCacheError, "缓存 accessToken 失败")
	}

	// 9. 保存登录态 client：后续业务接口必须复用它，
	//    否则 webvpn 模式下会因缺少 webvpn-token cookie 被网关拦截。
	s.clients.Store(uid, client)

	// 10. 成功获取 accessToken 后，立即删除 TGC（一次性使用）
	_ = s.sessionCache.DeleteTGC(ctx, uid)

	return nil
}

// doLoginWithToken 调用 doLogin 换取 accessToken。
// userToken 是 base64，可能包含 '+'：标准 url 编码会得到 %2B，
// 而浏览器抓包显示实际发出的是 %20（把 '+' 当空格处理）。
// 这里两种编码都尝试一次，避免服务端解析口径差异导致登录失败。
func (s *evaluationService) doLoginWithToken(ctx context.Context, client *http.Client, userToken string) (string, error) {
	standard := url.QueryEscape(userToken)                                      // + -> %2B
	browserLike := strings.ReplaceAll(url.QueryEscape(userToken), "%2B", "%20") // + -> %20（与浏览器一致）

	var lastErr error
	for _, encoded := range []string{standard, browserLike} {
		token, err := s.requestDoLogin(ctx, client, encoded)
		if err == nil && token != "" {
			return token, nil
		}
		if err != nil {
			lastErr = err
		}
	}

	if lastErr != nil {
		return "", lastErr
	}
	return "", common.NewAppError(common.CodeJwcLoginFailed, "未获取到 accessToken")
}

// requestDoLogin 发起一次 doLogin 请求并解析 accessToken
func (s *evaluationService) requestDoLogin(ctx context.Context, client *http.Client, encodedToken string) (string, error) {
	doLoginFullURL := fmt.Sprintf("%s?userToken=%s", s.doLoginURL, encodedToken)

	req, err := http.NewRequestWithContext(ctx, "POST", doLoginFullURL, nil)
	if err != nil {
		return "", common.NewAppError(common.CodeJwcLoginFailed, "构造 doLogin 请求失败")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	// Origin/Referer 必须与实际访问的域名一致（webvpn 模式下不是 jxzlpt.csuft.edu.cn）
	if origin := originOf(s.doLoginURL); origin != "" {
		req.Header.Set("Origin", origin)
		req.Header.Set("Referer", origin+"/")
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", common.NewAppError(common.CodeJwcLoginFailed, "doLogin 请求失败")
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", common.NewAppError(common.CodeJwcParseFailed, "读取 doLogin 响应失败")
	}

	var loginResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}

	if err := json.Unmarshal(bodyBytes, &loginResp); err != nil {
		return "", common.NewAppError(common.CodeJwcParseFailed, fmt.Sprintf("解析 doLogin 响应失败: %v", err))
	}

	return loginResp.Data.AccessToken, nil
}

// originOf 从完整 URL 中提取 scheme://host，用于构造 Origin/Referer 头
func originOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// getSession 获取教评会话：accessToken + 登录教评系统时使用的 http.Client。
//
// 两者必须同源：accessToken 缓存在 redis（可跨进程重启存活），但携带 webvpn-token
// cookie 的 client 只在进程内存里。若只剩 token 而没有 client，业务请求会被 webvpn
// 网关拦截返回空/未授权，因此这种情况必须重新走一次完整登录。
func (s *evaluationService) getSession(ctx context.Context, uid int, sid, spwd string) (string, *http.Client, error) {
	if v, ok := s.clients.Load(uid); ok {
		accessToken, err := s.evaluationCache.GetAccessToken(ctx, uid)
		if err == nil && accessToken != "" {
			return accessToken, v.(*http.Client), nil
		}
	}

	// 没有可用会话（首次访问 / token 过期 / 进程重启后 client 丢失），重新登录教评系统
	if err := s.LoginAndCacheEvaluation(ctx, uid, sid, spwd); err != nil {
		// 密码错误等认证类失败 → 转换为"绑定已失效"，让前端提示重新输入密码
		return "", nil, common.ToBindExpired(err)
	}

	v, ok := s.clients.Load(uid)
	if !ok {
		return "", nil, common.NewAppError(common.CodeJwcLoginFailed, "获取教评系统会话失败")
	}

	accessToken, err := s.evaluationCache.GetAccessToken(ctx, uid)
	if err != nil || accessToken == "" {
		return "", nil, common.NewAppError(common.CodeJwcLoginFailed, "获取教评系统会话失败")
	}

	return accessToken, v.(*http.Client), nil
}

// fetchWithAccessToken 使用 accessToken 发起请求。
// client 应传入登录教评系统时使用的那个实例（其 cookie jar 里有 webvpn-token，
// 网关靠它路由请求）；传 nil 时退化成新建裸 client（仅适用于校园网直连模式）。
func (s *evaluationService) fetchWithAccessToken(ctx context.Context, client *http.Client, method, targetURL string, accessToken string, formData url.Values) (io.ReadCloser, error) {
	var body io.Reader
	if formData != nil {
		body = strings.NewReader(formData.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, body)
	if err != nil {
		return nil, common.NewAppError(common.CodeHttpRequestFailed, "创建请求失败")
	}

	// 设置请求头
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Authorization", "Bearer"+accessToken) // 关键：添加 accessToken 到请求头
	if formData != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	if client == nil {
		client = &http.Client{Timeout: s.timeout}
	}

	resp, err := client.Do(req)

	if err != nil {
		return nil, common.NewAppError(common.CodeHttpRequestFailed, "请求失败")
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, common.NewAppError(common.CodeInvalidResponse, fmt.Sprintf("响应状态码异常: %d", resp.StatusCode))
	}

	return resp.Body, nil
}

// ============ 新增方法实现 ============

// GetEvaluationTasks 获取教评任务列表
func (s *evaluationService) GetEvaluationTasks(ctx context.Context, uid int) (*[]EvaluationTask, error) {
	user, err := s.userQuery.GetUserByUid(ctx, uid)
	if err != nil {
		return nil, common.NewAppError(common.CodeInternalError, "查询数据库错误")
	}

	accessToken, client, err := s.getSession(ctx, uid, user.Sid, user.Spwd)
	if err != nil {
		return nil, err
	}

	// 请求教评任务列表
	taskURL := s.apiBaseURL + "/getXspjtask"
	body, err := s.fetchWithAccessToken(ctx, client, "POST", taskURL, accessToken, nil)
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcRequestFailed, "获取教评任务失败")
	}
	defer body.Close()

	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "读取响应失败")
	}

	var taskResp EvaluationTaskResponse
	if err := json.Unmarshal(bodyBytes, &taskResp); err != nil {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "解析教评任务响应失败")
	}

	if taskResp.Code != 200 {
		return nil, common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("教评系统返回错误: %s", taskResp.Message))
	}

	return &taskResp.Data.PageData, nil
}

// GetEvaluationCourses 查询评教课程列表
func (s *evaluationService) GetEvaluationCourses(ctx context.Context, uid int, taskId int) (*[]EvaluationCourse, error) {
	user, err := s.userQuery.GetUserByUid(ctx, uid)
	if err != nil {
		return nil, common.NewAppError(common.CodeInternalError, "查询数据库错误")
	}

	accessToken, client, err := s.getSession(ctx, uid, user.Sid, user.Spwd)
	if err != nil {
		return nil, err
	}

	// 请求评教课程列表
	courseURL := fmt.Sprintf("%s/getXspjStudentCourses?taskid=%d", s.apiBaseURL, taskId)
	body, err := s.fetchWithAccessToken(ctx, client, "POST", courseURL, accessToken, nil)
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcRequestFailed, "获取评教课程失败")
	}
	defer body.Close()

	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "读取响应失败")
	}

	var courseResp EvaluationCoursesResponse
	if err := json.Unmarshal(bodyBytes, &courseResp); err != nil {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "解析评教课程响应失败")
	}

	if courseResp.Code != 200 {
		return nil, common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("教评系统返回错误: %s", courseResp.Message))
	}

	return &courseResp.Data.PageData, nil
}

// GetEvaluationQuestions 获取评教题目
func (s *evaluationService) GetEvaluationQuestions(ctx context.Context, uid int, indexId, pjCourseType string) (*[]EvaluationQuestion, error) {
	user, err := s.userQuery.GetUserByUid(ctx, uid)
	if err != nil {
		return nil, common.NewAppError(common.CodeInternalError, "查询数据库错误")
	}

	accessToken, client, err := s.getSession(ctx, uid, user.Sid, user.Spwd)
	if err != nil {
		return nil, err
	}

	// 请求评教题目
	questionURL := fmt.Sprintf("%s/getXspjTindexSystem?indexid=%s&pjcoursetype=%s",
		s.apiBaseURL, indexId, url.QueryEscape(pjCourseType))
	body, err := s.fetchWithAccessToken(ctx, client, "POST", questionURL, accessToken, nil)
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcRequestFailed, "获取评教题目失败")
	}
	defer body.Close()

	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "读取响应失败")
	}

	var questionResp EvaluationQuestionsResponse
	if err := json.Unmarshal(bodyBytes, &questionResp); err != nil {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "解析评教题目响应失败")
	}

	if questionResp.Code != 200 {
		return nil, common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("教评系统返回错误: %s", questionResp.Message))
	}

	return &questionResp.Data.PageData, nil
}

// GetEvaluateResultId 获取某门课的评教结果记录（含每道题的答案记录 ID）。
// 真实流程：进入评教页面时先调 getevaluateResultId?id=<课程.pjjgid> 取回一条已初始化的
// 结果记录，提交时把它作为 tevaluateResultid 回传、每条答案带上各自的 id，
// 服务端据此更新已有记录；缺少这步提交会被拒绝或产生脏数据。
func (s *evaluationService) GetEvaluateResultId(ctx context.Context, uid int, pjjgId int) (*[]EvaluationResultItem, error) {
	user, err := s.userQuery.GetUserByUid(ctx, uid)
	if err != nil {
		return nil, common.NewAppError(common.CodeInternalError, "查询数据库错误")
	}

	accessToken, client, err := s.getSession(ctx, uid, user.Sid, user.Spwd)
	if err != nil {
		return nil, err
	}

	resultURL := fmt.Sprintf("%s/getevaluateResultId?id=%d", s.apiBaseURL, pjjgId)
	body, err := s.fetchWithAccessToken(ctx, client, "POST", resultURL, accessToken, nil)
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcRequestFailed, "获取评教结果记录失败")
	}
	defer body.Close()

	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "读取响应失败")
	}

	var resultResp EvaluationResultResponse
	if err := json.Unmarshal(bodyBytes, &resultResp); err != nil {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "解析评教结果记录响应失败")
	}

	if resultResp.Code != 200 {
		return nil, common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("教评系统返回错误: %s", resultResp.Message))
	}

	return &resultResp.Data.PageData, nil
}

// SubmitEvaluation 提交评教
func (s *evaluationService) SubmitEvaluation(ctx context.Context, uid int, submitData []EvaluationSubmitRequest) error {
	user, err := s.userQuery.GetUserByUid(ctx, uid)
	if err != nil {
		return common.NewAppError(common.CodeInternalError, "查询数据库错误")
	}

	accessToken, client, err := s.getSession(ctx, uid, user.Sid, user.Spwd)
	if err != nil {
		return err
	}

	// 序列化提交数据
	jsonData, err := json.Marshal(submitData)
	if err != nil {
		return common.NewAppError(common.CodeInvalidParams, "序列化提交数据失败")
	}

	// 构造请求
	submitURL := s.apiBaseURL + "/saveStudentComment"
	req, err := http.NewRequestWithContext(ctx, "POST", submitURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return common.NewAppError(common.CodeHttpRequestFailed, "创建请求失败")
	}

	// 设置请求头
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Authorization", "Bearer"+accessToken)
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	if origin := originOf(s.apiBaseURL); origin != "" {
		req.Header.Set("Origin", origin)
		req.Header.Set("Referer", origin+"/")
	}

	if client == nil {
		client = &http.Client{Timeout: s.timeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		return common.NewAppError(common.CodeHttpRequestFailed, "提交评教失败")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return common.NewAppError(common.CodeInvalidResponse, fmt.Sprintf("响应状态码异常: %d", resp.StatusCode))
	}

	// 解析响应
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return common.NewAppError(common.CodeJwcParseFailed, "读取响应失败")
	}

	var submitResp EvaluationSubmitResponse
	if err := json.Unmarshal(bodyBytes, &submitResp); err != nil {
		return common.NewAppError(common.CodeJwcParseFailed, "解析提交响应失败")
	}

	if submitResp.Code != 200 {
		return common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("提交评教失败: %s", submitResp.Message))
	}

	return nil
}

// AutoEvaluation 自动评教 - 自动完成所有未评课程的评教
func (s *evaluationService) AutoEvaluation(ctx context.Context, uid int) (*AutoEvaluationResult, error) {
	user, err := s.userQuery.GetUserByUid(ctx, uid)
	if err != nil {
		return nil, common.NewAppError(common.CodeInternalError, "查询数据库错误")
	}

	// 确保已登录教评系统
	_, _, err = s.getSession(ctx, uid, user.Sid, user.Spwd)
	if err != nil {
		return nil, err
	}

	result := &AutoEvaluationResult{
		SuccessList: make([]string, 0),
		FailedList:  make([]string, 0),
		SkippedList: make([]string, 0),
	}

	// 1. 获取所有教评任务
	tasks, err := s.GetEvaluationTasks(ctx, uid)
	if err != nil {
		return nil, err
	}

	if len(*tasks) == 0 {
		result.Message = "当前没有可用的教评任务"
		return result, nil
	}

	// 2. 遍历每个任务
	for _, task := range *tasks {
		// 只处理进行中的任务
		if task.CurrentStatus != "进行中" {
			continue
		}

		// 3. 获取该任务下的所有课程
		courses, err := s.GetEvaluationCourses(ctx, uid, task.TaskId)
		if err != nil {
			continue // 跳过获取失败的任务
		}

		result.TotalCourses += len(*courses)

		// 4. 遍历每门课程
		for _, course := range *courses {
			// 跳过已评课程
			if course.HasSubmit == 1 {
				result.SkippedCourses++
				result.EvaluatedCourses++
				result.SkippedList = append(result.SkippedList, fmt.Sprintf("%s-%s", course.CourseName, course.TeacherName))
				continue
			}

			// 5. 获取评教题目
			questions, err := s.GetEvaluationQuestions(ctx, uid, task.IndexId, course.PjCourseType)
			if err != nil {
				result.FailedCourses++
				result.FailedList = append(result.FailedList, fmt.Sprintf("%s-%s(获取题目失败)", course.CourseName, course.TeacherName))
				continue
			}

			// 检查题目是否为空(学校未开放该类型课程的教评)
			if len(*questions) == 0 {
				result.FailedCourses++
				result.FailedList = append(result.FailedList, fmt.Sprintf("%s-%s(该类型课程教评未开放)", course.CourseName, course.TeacherName))
				continue
			}

			// 6. 取该课程的评教结果记录：提交时必须回传 tevaluateResultid 以及每条答案的 id，
			//    否则服务端无法把答案关联到已初始化的结果记录上。
			answerIds := make(map[int]int, len(*questions)) // indexid -> 答案记录 id
			if course.PjjgId > 0 {
				items, err := s.GetEvaluateResultId(ctx, uid, course.PjjgId)
				if err != nil {
					result.FailedCourses++
					result.FailedList = append(result.FailedList, fmt.Sprintf("%s-%s(获取评教结果记录失败)", course.CourseName, course.TeacherName))
					continue
				}
				for _, it := range *items {
					answerIds[it.IndexId] = it.Id
				}
			}

			// 7. 自动生成答案 - 先给所有题满分，然后随机选一题减1分
			evaluateResult := make([]EvaluationAnswer, 0, len(*questions))
			totalScore := 0
			scoreQuestionIndices := make([]int, 0) // 记录打分题的索引

			// 第一遍：给所有题满分
			for i, q := range *questions {
				answer := EvaluationAnswer{
					IndexOrder: q.Ordor,
					Sfbt:       q.IsEmptyed,
					Yjzb:       q.FirstLevlIndex,
					IndexType:  q.Type,
					IndexId:    q.IndexId,
					Id:         answerIds[q.IndexId],
				}

				// 根据题目类型填充答案
				if q.Type == "打分题" && q.IsScored == "是" {
					// 打分题给满分（index_score 与 index_title 都是字符串形式的分数）
					scoreStr := fmt.Sprintf("%.0f", q.Score)
					answer.IndexScore = scoreStr
					answer.IndexTitle = &scoreStr
					totalScore += int(q.Score)
					scoreQuestionIndices = append(scoreQuestionIndices, i) // 记录打分题索引
				} else if q.Type == "问答题" {
					// 问答题：index_score 是数字 0；非必填时 index_title 需为 null
					answer.IndexScore = 0
					if q.IsEmptyed == "否" {
						// 必填问答题给默认好评
						comment := "老师授课认真负责，教学效果好"
						answer.IndexTitle = &comment
					}
				}

				evaluateResult = append(evaluateResult, answer)
			}

			// 第二遍：随机选择一道打分题减1分
			// （教务任务里 sfqxzdzgf=是 即"限制最高分"，全满分容易被判异常，故刻意留 1 分）
			if len(scoreQuestionIndices) > 0 {
				// 随机选择一道打分题
				randomIndex := scoreQuestionIndices[rand.Intn(len(scoreQuestionIndices))]
				q := (*questions)[randomIndex]

				// 减1分
				score99 := q.Score - 1
				if score99 < 0 {
					score99 = 0 // 防止负分
				}
				scoreStr := fmt.Sprintf("%.0f", score99)
				evaluateResult[randomIndex].IndexScore = scoreStr
				evaluateResult[randomIndex].IndexTitle = &scoreStr

				// 调整总分
				totalScore = totalScore - int(q.Score) + int(score99)
			}

			// 8. 构造提交数据
			submitData := []EvaluationSubmitRequest{
				{
					TaskId:            task.TaskId,
					ClassNo:           course.ClassNo,
					CourseCode:        course.CourseCode,
					CourseName:        course.CourseName,
					JobNumber:         course.JobNumber,
					StudentId:         course.StudentId,
					StudentName:       course.StudentName,
					TeacherName:       course.TeacherName,
					YearTerm:          course.YearTerm,
					TotalScore:        totalScore,
					PjCourseType:      course.PjCourseType,
					CourseOrgCode:     course.CourseOrgCode,
					CourseOrgName:     course.CourseOrgName,
					TEvaluateResultId: course.PjjgId,
					EvaluateResult:    evaluateResult,
					CommitTime:        time.Now().Format("2006-01-02 15:04:05"),
				},
			}

			// 9. 提交评教
			err = s.SubmitEvaluation(ctx, uid, submitData)
			if err != nil {
				result.FailedCourses++
				result.FailedList = append(result.FailedList, fmt.Sprintf("%s-%s", course.CourseName, course.TeacherName))
			} else {
				result.SuccessCourses++
				result.EvaluatedCourses++
				result.SuccessList = append(result.SuccessList, fmt.Sprintf("%s-%s", course.CourseName, course.TeacherName))
			}

			// 避免请求过快,休眠一下
			time.Sleep(500 * time.Millisecond)
		}
	}

	// 生成总体结果消息
	if result.TotalCourses == 0 {
		result.Message = "当前没有需要评教的课程"
	} else if result.FailedCourses == 0 {
		result.Message = fmt.Sprintf("自动评教完成！成功评教 %d 门课程，跳过 %d 门已评课程", result.SuccessCourses, result.SkippedCourses)
	} else {
		result.Message = fmt.Sprintf("自动评教完成！成功 %d 门，失败 %d 门，跳过 %d 门", result.SuccessCourses, result.FailedCourses, result.SkippedCourses)
	}

	return result, nil
}

// GetEvaluationStatus 获取评教状态 - 查看所有任务下已评和未评的课程
func (s *evaluationService) GetEvaluationStatus(ctx context.Context, uid int) (*EvaluationStatus, error) {
	user, err := s.userQuery.GetUserByUid(ctx, uid)
	if err != nil {
		return nil, common.NewAppError(common.CodeInternalError, "查询数据库错误")
	}

	// 确保已登录教评系统
	_, _, err = s.getSession(ctx, uid, user.Sid, user.Spwd)
	if err != nil {
		return nil, err
	}

	status := &EvaluationStatus{
		EvaluatedList:   make([]CourseInfo, 0),
		UnevaluatedList: make([]CourseInfo, 0),
		TaskDetails:     make([]TaskStatusDetail, 0),
	}

	// 1. 获取所有教评任务
	tasks, err := s.GetEvaluationTasks(ctx, uid)
	if err != nil {
		return nil, err
	}

	status.TotalTasks = len(*tasks)

	// 2. 遍历每个任务
	for _, task := range *tasks {
		taskDetail := TaskStatusDetail{
			TaskId:        task.TaskId,
			TaskName:      task.TaskName,
			CurrentStatus: task.CurrentStatus,
		}

		// 统计进行中的任务
		if task.CurrentStatus == "进行中" {
			status.OngoingTasks++
		}

		// 3. 获取该任务下的所有课程
		courses, err := s.GetEvaluationCourses(ctx, uid, task.TaskId)
		if err != nil {
			// 获取课程失败，跳过该任务
			continue
		}

		taskDetail.TotalCourses = len(*courses)
		status.TotalCourses += len(*courses)

		// 4. 遍历课程，分类统计
		for _, course := range *courses {
			courseInfo := CourseInfo{
				TaskId:       task.TaskId,
				TaskName:     task.TaskName,
				CourseName:   course.CourseName,
				TeacherName:  course.TeacherName,
				PjCourseType: course.PjCourseType,
				HasSubmit:    course.HasSubmit,
			}

			if course.HasSubmit == 1 {
				// 已评课程
				status.EvaluatedCourses++
				taskDetail.EvaluatedCourses++
				status.EvaluatedList = append(status.EvaluatedList, courseInfo)
			} else {
				// 未评课程
				status.UnevaluatedCourses++
				taskDetail.UnevaluatedCourses++
				status.UnevaluatedList = append(status.UnevaluatedList, courseInfo)
			}
		}

		status.TaskDetails = append(status.TaskDetails, taskDetail)
	}

	return status, nil
}
