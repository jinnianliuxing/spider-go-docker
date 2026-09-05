package share

import (
	"spider-go/internal/common"

	"github.com/gin-gonic/gin"
)

// Handler 分享HTTP处理器
type Handler struct {
	service Service
}

// NewHandler 创建分享处理器
func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

// RegisterAuthRoutes 注册需要认证的路由
func (h *Handler) RegisterAuthRoutes(r *gin.RouterGroup) {
	share := r.Group("/share")
	{
		share.POST("/course", h.CreateCourseShare) // 创建课程表分享
	}
}

// RegisterPublicRoutes 注册公开路由
func (h *Handler) RegisterPublicRoutes(r *gin.RouterGroup) {
	share := r.Group("/share")
	{
		share.GET("/course/:code", h.GetSharedCourse) // 查看分享的课程表
	}
}

// CreateCourseShare 创建课程表分享
func (h *Handler) CreateCourseShare(c *gin.Context) {
	uid, ok := c.Get("uid")
	if !ok {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	var req CreateShareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	resp, err := h.service.CreateShareToken(c.Request.Context(), uid.(int), &req)
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.ErrorWithAppError(c, appErr)
		} else {
			common.Error(c, common.CodeInternalError, "创建分享失败")
		}
		return
	}

	common.Success(c, resp)
}

// GetSharedCourse 查看分享的课程表
func (h *Handler) GetSharedCourse(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		common.Error(c, common.CodeInvalidParams, "分享码不能为空")
		return
	}

	resp, err := h.service.GetSharedCourse(c.Request.Context(), code)
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.ErrorWithAppError(c, appErr)
		} else {
			common.Error(c, common.CodeInternalError, "获取分享课程表失败")
		}
		return
	}

	common.Success(c, resp)
}
