package evaluation

import (
	"spider-go/internal/common"
	"strconv"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service Service
}

// NewHandler 创建考试处理器
func NewHandler(service Service) *Handler {
	return &Handler{
		service: service,
	}
}

// RegisterRoutes 注册路由
func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	evaluation := r.Group("/evaluation")
	{
		evaluation.GET("/tasks", h.GetEvaluationTasks)         // 获取教评任务列表
		evaluation.GET("/courses", h.GetEvaluationCourses)     // 获取待评课程列表
		evaluation.GET("/questions", h.GetEvaluationQuestions) // 获取评教题目
		evaluation.POST("/submit", h.SubmitEvaluation)         // 提交评教
		evaluation.POST("/auto", h.AutoEvaluation)             // 自动评教
		evaluation.GET("/status", h.GetEvaluationStatus)       // 查看评教状态
	}
}

func (h *Handler) GetEvaluationInfo(c *gin.Context) {
	uidValue, ok := c.Get("uid")
	if !ok {
		common.Error(c, common.CodeUnauthorized, "未授权，请登录")
		return
	}

	uid := uidValue.(int)

	data, err := h.service.GetEvaluationInfo(c, uid)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}
	common.Success(c, data)
}

// GetEvaluationTasks 获取教评任务列表
func (h *Handler) GetEvaluationTasks(c *gin.Context) {
	uidValue, ok := c.Get("uid")
	if !ok {
		common.Error(c, common.CodeUnauthorized, "未授权，请登录")
		return
	}

	uid := uidValue.(int)

	data, err := h.service.GetEvaluationTasks(c, uid)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}
	common.Success(c, data)
}

// GetEvaluationCourses 获取待评课程列表
func (h *Handler) GetEvaluationCourses(c *gin.Context) {
	uidValue, ok := c.Get("uid")
	if !ok {
		common.Error(c, common.CodeUnauthorized, "未授权，请登录")
		return
	}

	uid := uidValue.(int)

	// 获取任务ID
	taskIdStr := c.Query("taskid")
	if taskIdStr == "" {
		common.Error(c, common.CodeInvalidParams, "缺少任务ID")
		return
	}

	taskId, err := strconv.Atoi(taskIdStr)
	if err != nil {
		common.Error(c, common.CodeInvalidParams, "任务ID格式错误")
		return
	}

	data, err := h.service.GetEvaluationCourses(c, uid, taskId)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}
	common.Success(c, data)
}

// GetEvaluationQuestions 获取评教题目
func (h *Handler) GetEvaluationQuestions(c *gin.Context) {
	uidValue, ok := c.Get("uid")
	if !ok {
		common.Error(c, common.CodeUnauthorized, "未授权，请登录")
		return
	}

	uid := uidValue.(int)

	// 获取参数
	indexId := c.Query("indexid")
	pjCourseType := c.Query("pjcoursetype")

	if indexId == "" || pjCourseType == "" {
		common.Error(c, common.CodeInvalidParams, "缺少必要参数")
		return
	}

	data, err := h.service.GetEvaluationQuestions(c, uid, indexId, pjCourseType)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}
	common.Success(c, data)
}

// SubmitEvaluation 提交评教
func (h *Handler) SubmitEvaluation(c *gin.Context) {
	uidValue, ok := c.Get("uid")
	if !ok {
		common.Error(c, common.CodeUnauthorized, "未授权，请登录")
		return
	}

	uid := uidValue.(int)

	// 绑定请求体
	var submitData []EvaluationSubmitRequest
	if err := c.ShouldBindJSON(&submitData); err != nil {
		common.Error(c, common.CodeInvalidParams, "请求参数错误")
		return
	}

	if len(submitData) == 0 {
		common.Error(c, common.CodeInvalidParams, "提交数据不能为空")
		return
	}

	err := h.service.SubmitEvaluation(c, uid, submitData)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.Success(c, gin.H{"message": "提交成功"})
}

// AutoEvaluation 自动评教
func (h *Handler) AutoEvaluation(c *gin.Context) {
	uidValue, ok := c.Get("uid")
	if !ok {
		common.Error(c, common.CodeUnauthorized, "未授权，请登录")
		return
	}

	uid := uidValue.(int)

	// 执行自动评教
	result, err := h.service.AutoEvaluation(c, uid)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.Success(c, result)
}

// GetEvaluationStatus 查看评教状态
func (h *Handler) GetEvaluationStatus(c *gin.Context) {
	uidValue, ok := c.Get("uid")
	if !ok {
		common.Error(c, common.CodeUnauthorized, "未授权，请登录")
		return
	}

	uid := uidValue.(int)

	// 获取评教状态
	status, err := h.service.GetEvaluationStatus(c, uid)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.Success(c, status)
}
