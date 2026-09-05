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
	"time"
)

type Service interface {
	GetEvaluationInfo(ctx context.Context, uid int) (*[]EvaluationInfo, error)
	LoginAndCacheEvaluation(ctx context.Context, uid int, sid, spwd string) error
	// 新增接口
	GetEvaluationTasks(ctx context.Context, uid int) (*[]EvaluationTask, error)
	GetEvaluationCourses(ctx context.Context, uid int, taskId int) (*[]EvaluationCourse, error)
	GetEvaluationQuestions(ctx context.Context, uid int, indexId, pjCourseType string) (*[]EvaluationQuestion, error)
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
	timeout           time.Duration
	cacheExpire       time.Duration
}

func NewService(
	userQuery shared.UserQuery,
	sessionService service.SessionService,
	evaluationCache cache.EvaluationCache,
	sessionCache cache.SessionCache,
	evaluationInfoURL string,
	casRedirectURL string,
	doLoginURL string,
) Service {
	return &evaluationService{
		userQuery:         userQuery,
		sessionService:    sessionService,
		evaluationCache:   evaluationCache,
		sessionCache:      sessionCache,
		evaluationInfoURL: evaluationInfoURL,
		casRedirectURL:    casRedirectURL,
		doLoginURL:        doLoginURL,
		timeout:           30 * time.Second,
		cacheExpire:       30 * time.Minute, // 教评 accessToken 缓存 30 分钟
	}
}

func (s *evaluationService) GetEvaluationInfo(ctx context.Context, uid int) (*[]EvaluationInfo, error) {
	user, err := s.userQuery.GetUserByUid(ctx, uid)
	if err != nil {
		return nil, common.NewAppError(common.CodeInternalError, "查询数据库错误")
	}

	accessToken, err := s.getAccessTokenOrLogin(ctx, uid, user.Sid, user.Spwd)
	if err != nil {
		return nil, err
	}

	// 使用 accessToken 请求教评信息
	body, err := s.fetchWithAccessToken(ctx, "POST", s.evaluationInfoURL, accessToken, nil)
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

	// 7. 调用 doLogin 获取 accessToken
	doLoginFullURL := fmt.Sprintf("%s?userToken=%s", s.doLoginURL, url.QueryEscape(userToken))

	req, err := http.NewRequest("POST", doLoginFullURL, nil)
	if err != nil {
		return common.NewAppError(common.CodeJwcLoginFailed, "构造 doLogin 请求失败")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Origin", "https://jxzlpt.csuft.edu.cn")

	resp, err := client.Do(req)
	if err != nil {
		return common.NewAppError(common.CodeJwcLoginFailed, "doLogin 请求失败")
	}
	defer resp.Body.Close()

	// 解析响应获取 accessToken
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return common.NewAppError(common.CodeJwcParseFailed, "读取 doLogin 响应失败")
	}

	var loginResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}

	if err := json.Unmarshal(bodyBytes, &loginResp); err != nil {
		return common.NewAppError(common.CodeJwcParseFailed, fmt.Sprintf("解析 doLogin 响应失败: %v", err))
	}

	if loginResp.Data.AccessToken == "" {
		return common.NewAppError(common.CodeJwcLoginFailed, "未获取到 accessToken")
	}
	// 8. 缓存 accessToken
	if err := s.evaluationCache.SetAccessToken(ctx, uid, loginResp.Data.AccessToken, s.cacheExpire); err != nil {
		return common.NewAppError(common.CodeCacheError, "缓存 accessToken 失败")
	}

	// 9. 成功获取 accessToken 后，立即删除 TGC（一次性使用）
	_ = s.sessionCache.DeleteTGC(ctx, uid)

	return nil
}

// getAccessTokenOrLogin 获取缓存的 accessToken 或登录
func (s *evaluationService) getAccessTokenOrLogin(ctx context.Context, uid int, sid, spwd string) (string, error) {
	// 先尝试从缓存中获取 accessToken
	accessToken, err := s.evaluationCache.GetAccessToken(ctx, uid)
	if err != nil {
		return "", common.NewAppError(common.CodeCacheError, "缓存错误")
	}

	if accessToken != "" {
		return accessToken, nil
	}

	// 如果没有缓存，则登录教评系统
	if err := s.LoginAndCacheEvaluation(ctx, uid, sid, spwd); err != nil {
		return "", err
	}

	// 重新获取 accessToken
	accessToken, err = s.evaluationCache.GetAccessToken(ctx, uid)
	if err != nil || accessToken == "" {
		return "", common.NewAppError(common.CodeJwcLoginFailed, "获取教评系统会话失败")
	}

	return accessToken, nil
}

// fetchWithAccessToken 使用 accessToken 发起请求
func (s *evaluationService) fetchWithAccessToken(ctx context.Context, method, targetURL string, accessToken string, formData url.Values) (io.ReadCloser, error) {
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

	client := &http.Client{
		Timeout: s.timeout,
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

	accessToken, err := s.getAccessTokenOrLogin(ctx, uid, user.Sid, user.Spwd)
	if err != nil {
		return nil, err
	}

	// 请求教评任务列表
	taskURL := "https://jxzlpt.csuft.edu.cn/api/xspj/xspj/getXspjtask"
	body, err := s.fetchWithAccessToken(ctx, "POST", taskURL, accessToken, nil)
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

	accessToken, err := s.getAccessTokenOrLogin(ctx, uid, user.Sid, user.Spwd)
	if err != nil {
		return nil, err
	}

	// 请求评教课程列表
	courseURL := fmt.Sprintf("https://jxzlpt.csuft.edu.cn/api/xspj/xspj/getXspjStudentCourses?taskid=%d", taskId)
	body, err := s.fetchWithAccessToken(ctx, "POST", courseURL, accessToken, nil)
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

	accessToken, err := s.getAccessTokenOrLogin(ctx, uid, user.Sid, user.Spwd)
	if err != nil {
		return nil, err
	}

	// 请求评教题目
	questionURL := fmt.Sprintf("https://jxzlpt.csuft.edu.cn/api/xspj/xspj/getXspjTindexSystem?indexid=%s&pjcoursetype=%s",
		indexId, url.QueryEscape(pjCourseType))
	body, err := s.fetchWithAccessToken(ctx, "POST", questionURL, accessToken, nil)
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

// SubmitEvaluation 提交评教
func (s *evaluationService) SubmitEvaluation(ctx context.Context, uid int, submitData []EvaluationSubmitRequest) error {
	user, err := s.userQuery.GetUserByUid(ctx, uid)
	if err != nil {
		return common.NewAppError(common.CodeInternalError, "查询数据库错误")
	}

	accessToken, err := s.getAccessTokenOrLogin(ctx, uid, user.Sid, user.Spwd)
	if err != nil {
		return err
	}

	// 序列化提交数据
	jsonData, err := json.Marshal(submitData)
	if err != nil {
		return common.NewAppError(common.CodeInvalidParams, "序列化提交数据失败")
	}

	// 构造请求
	submitURL := "https://jxzlpt.csuft.edu.cn/api/xspj/xspj/saveStudentComment"
	req, err := http.NewRequestWithContext(ctx, "POST", submitURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return common.NewAppError(common.CodeHttpRequestFailed, "创建请求失败")
	}

	// 设置请求头
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Authorization", "Bearer"+accessToken)
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")

	client := &http.Client{
		Timeout: s.timeout,
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
	_, err = s.getAccessTokenOrLogin(ctx, uid, user.Sid, user.Spwd)
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

			// 6. 自动生成答案 - 先给所有题满分，然后随机选一题减1分
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
				}

				// 根据题目类型填充答案
				if q.Type == "打分题" && q.IsScored == "是" {
					// 打分题给满分
					scoreStr := fmt.Sprintf("%.0f", q.Score)
					answer.IndexScore = scoreStr
					answer.IndexTitle = scoreStr
					totalScore += int(q.Score)
					scoreQuestionIndices = append(scoreQuestionIndices, i) // 记录打分题索引
				} else if q.Type == "问答题" {
					// 问答题可以为空或给默认好评
					answer.IndexScore = "0"
					if q.IsEmptyed == "否" {
						// 必填问答题给默认好评
						answer.IndexTitle = "老师授课认真负责，教学效果好"
					} else {
						answer.IndexTitle = ""
					}
				}

				evaluateResult = append(evaluateResult, answer)
			}

			// 第二遍：随机选择一道打分题减1分
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
				evaluateResult[randomIndex].IndexTitle = scoreStr

				// 调整总分
				totalScore = totalScore - int(q.Score) + int(score99)
			}

			// 7. 构造提交数据
			submitData := []EvaluationSubmitRequest{
				{
					TaskId:         task.TaskId,
					ClassNo:        course.ClassNo,
					CourseCode:     course.CourseCode,
					CourseName:     course.CourseName,
					JobNumber:      course.JobNumber,
					StudentId:      course.StudentId,
					StudentName:    course.StudentName,
					TeacherName:    course.TeacherName,
					YearTerm:       course.YearTerm,
					TotalScore:     totalScore,
					PjCourseType:   course.PjCourseType,
					CourseOrgCode:  course.CourseOrgCode,
					CourseOrgName:  course.CourseOrgName,
					EvaluateResult: evaluateResult,
					CommitTime:     time.Now().Format("2006-01-02 15:04:05"),
				},
			}

			// 8. 提交评教
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
	_, err = s.getAccessTokenOrLogin(ctx, uid, user.Sid, user.Spwd)
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
