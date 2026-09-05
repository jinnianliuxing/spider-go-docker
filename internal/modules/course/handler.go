package course

import (
	"spider-go/internal/common"

	"github.com/gin-gonic/gin"
)

// Handler 课程HTTP处理器
type Handler struct {
	service Service
}

// NewHandler 创建课程处理器
func NewHandler(service Service) *Handler {
	return &Handler{
		service: service,
	}
}

// RegisterRoutes 注册路由
func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	courses := r.Group("/courses")
	{
		courses.GET("", h.GetCourseTable) // 获取课程表
	}
}

// GetCourseTable 获取课程表
// @Summary 获取课程表
// @Tags Course
// @Produce json
// @Param week query int true "周次" minimum(1) maximum(20)
// @Param term query string true "学期" example(2024-2025-1)
// @Success 200 {object} WeekSchedule
// @Router /courses [get]
func (h *Handler) GetCourseTable(c *gin.Context) {
	uid, ok := c.Get("uid")
	if !ok {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	var req GetCourseTableRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	schedule, err := h.service.GetCourseTableByWeek(c.Request.Context(), uid.(int), req.Week, req.Term)
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.ErrorWithAppError(c, appErr)
		} else {
			common.Error(c, common.CodeInternalError, "获取课程表失败")
		}
		return
	}

	common.Success(c, schedule)
}
