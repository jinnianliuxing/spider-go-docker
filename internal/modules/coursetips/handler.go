package coursetips

import (
	"spider-go/internal/common"

	"github.com/gin-gonic/gin"
)

// Handler 选课提示HTTP处理器
type Handler struct {
	service Service
}

// NewHandler 创建选课提示处理器
func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes 注册路由到认证路由组
func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/course-tips", h.GetTeacherStats)
}

// GetTeacherStats 获取体育选修课教师统计
// GET /course-tips?course_name=体育选项课I
func (h *Handler) GetTeacherStats(c *gin.Context) {
	var req GetTeacherStatsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, "课程名称参数不能为空")
		return
	}

	resp, err := h.service.GetTeacherStats(c.Request.Context(), req.CourseName)
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.ErrorWithAppError(c, appErr)
		} else {
			common.Error(c, common.CodeInternalError, "获取统计数据失败")
		}
		return
	}

	common.Success(c, resp)
}
