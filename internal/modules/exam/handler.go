package exam

import (
	"spider-go/internal/common"

	"github.com/gin-gonic/gin"
)

// Handler 考试HTTP处理器
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
	exams := r.Group("/exams")
	{
		exams.GET("", h.GetExams) // 获取考试安排
	}
}

// GetExams 获取考试安排
// @Summary 获取考试安排
// @Tags Exam
// @Produce JSON
// @Param term query string true "学期" example(2024-2025-1)
// @Success 200 {array} ExamArrangement
// @Router /exams [get]
func (h *Handler) GetExams(c *gin.Context) {
	uid, ok := c.Get("uid")
	if !ok {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	var req GetExamsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	exams, err := h.service.GetAllExams(c.Request.Context(), uid.(int), req.Term)
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.ErrorWithAppError(c, appErr)
		} else {
			common.Error(c, common.CodeInternalError, "获取考试安排失败")
		}
		return
	}

	common.Success(c, exams)
}
