package admin

import (
	"spider-go/internal/common"
	"strconv"

	"github.com/gin-gonic/gin"
)

// Handler 管理员HTTP处理器
type Handler struct {
	service    Service
	captchaSvc CaptchaService // 管理员验证码发送服务（验证邮箱+发送验证码）
}

// NewHandler 创建管理员处理器
func NewHandler(service Service, captchaSvc CaptchaService) *Handler {
	return &Handler{
		service:    service,
		captchaSvc: captchaSvc,
	}
}

// RegisterRoutes 注册路由
func (h *Handler) RegisterRoutes(public *gin.RouterGroup, authenticated *gin.RouterGroup) {
	// 公开路由（无需认证）
	publicAdmin := public.Group("/admin")
	{
		publicAdmin.POST("/login", h.Login)                      // 管理员密码登录
		publicAdmin.POST("/login/captcha", h.LoginByCaptcha)     // 管理员验证码登录
		publicAdmin.POST("/reset-password", h.ResetPassword)     // 重置密码（需验证码验证）
		publicAdmin.POST("/captcha/send", h.SendAdminCaptcha)    // 管理员发送验证码（验证邮箱归属）
	}

	// 需要认证的路由（authenticated 已经是 /api/admin 了，不需要再加 /admin）
	authenticated.GET("/info", h.GetInfo)                    // 获取管理员信息
	authenticated.POST("/reset", h.ChangePassword)           // 修改密码
	authenticated.POST("/update-email", h.UpdateEmail)       // 更新管理员邮箱
	authenticated.POST("/broadcast-email", h.BroadcastEmail) // 群发邮件
	authenticated.GET("/users", h.GetAllUsers)               // 获取所有用户
	authenticated.DELETE("/user/:uid", h.DeleteUser)         // 删除用户
}

// CaptchaLoginRequest 管理员验证码登录请求
type CaptchaLoginRequest struct {
	Email   string `json:"email" binding:"required,email"`
	Captcha string `json:"captcha" binding:"required"`
}

// LoginByCaptcha 管理员验证码登录
func (h *Handler) LoginByCaptcha(c *gin.Context) {
	var req CaptchaLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	token, admin, err := h.service.LoginByCaptcha(c.Request.Context(), req.Email, req.Captcha)
	if err != nil {
		if err == ErrInvalidCredentials {
			common.Error(c, common.CodeInvalidPassword, err.Error())
		} else {
			common.Error(c, common.CodeInternalError, err.Error())
		}
		return
	}

	common.Success(c, LoginResponse{
		Token: token,
		Admin: admin.ToResponse(),
	})
}

// Login 管理员登录
// @Summary 管理员登录
// @Tags Admin
// @Accept json
// @Produce json
// @Param request body LoginRequest true "登录请求"
// @Success 200 {object} LoginResponse
// @Router /admin/login [post]
func (h *Handler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	token, admin, err := h.service.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		if err == ErrInvalidCredentials {
			common.Error(c, common.CodeInvalidPassword, err.Error())
		} else {
			common.Error(c, common.CodeInternalError, "登录失败")
		}
		return
	}

	common.Success(c, LoginResponse{
		Token: token,
		Admin: admin.ToResponse(),
	})
}

// GetInfo 获取管理员信息
// @Summary 获取管理员信息
// @Tags Admin
// @Produce json
// @Success 200 {object} AdminResponse
// @Router /admin/info [get]
func (h *Handler) GetInfo(c *gin.Context) {
	aid, exists := c.Get("aid")
	if !exists {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	admin, err := h.service.GetAdminInfo(c.Request.Context(), aid.(int))
	if err != nil {
		common.Error(c, common.CodeUserNotFound, "获取管理员信息失败")
		return
	}

	common.Success(c, admin.ToResponse())
}

// UpdateEmailRequest 更新管理员邮箱请求
type UpdateEmailRequest struct {
	NewEmail string `json:"new_email" binding:"required,email"`
	Captcha  string `json:"captcha" binding:"required"`
}

// ResetPasswordRequest 重置密码请求
type ResetPasswordRequest struct {
	Email   string `json:"email" binding:"required,email"`
	Captcha string `json:"captcha" binding:"required"`
}

// ResetPassword 通过验证码验证后重置密码为123456
func (h *Handler) ResetPassword(c *gin.Context) {
	var req ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	if err := h.service.ResetPasswordByCaptcha(c.Request.Context(), req.Email, req.Captcha); err != nil {
		if err == ErrInvalidCredentials {
			common.Error(c, common.CodeInvalidPassword, "管理员不存在")
		} else if appErr, ok := err.(*common.AppError); ok {
			common.Error(c, appErr.Code, appErr.Message)
		} else {
			common.Error(c, common.CodeInternalError, "重置密码失败")
		}
		return
	}

	common.Success(c, gin.H{"message": "密码已重置为123456"})
}

// UpdateEmail 更新管理员登录邮箱
func (h *Handler) UpdateEmail(c *gin.Context) {
	aid, exists := c.Get("aid")
	if !exists {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	var req UpdateEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	if err := h.service.UpdateEmail(c.Request.Context(), aid.(int), req.NewEmail, req.Captcha); err != nil {
		if err == ErrEmailAlreadyExists {
			common.Error(c, common.CodeUserAlreadyExists, "该邮箱已被其他管理员使用")
		} else if appErr, ok := err.(*common.AppError); ok {
			common.Error(c, appErr.Code, appErr.Message)
		} else {
			common.Error(c, common.CodeInternalError, "更新邮箱失败")
		}
		return
	}

	common.Success(c, gin.H{"message": "邮箱更新成功"})
}

// SendAdminCaptchaRequest 管理员发送验证码请求
type SendAdminCaptchaRequest struct {
	Email string `json:"email" binding:"required,email"`
}

// SendAdminCaptcha 管理员发送验证码（先验证邮箱是否为已注册管理员）
func (h *Handler) SendAdminCaptcha(c *gin.Context) {
	var req SendAdminCaptchaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	if err := h.captchaSvc.SendAdminCaptcha(c.Request.Context(), req.Email); err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.ErrorWithAppError(c, appErr)
		} else {
			common.Error(c, common.CodeInternalError, "发送验证码失败")
		}
		return
	}

	common.Success(c, gin.H{"message": "验证码已发送"})
}

// ChangePassword 修改密码
// @Summary 修改密码
// @Tags Admin
// @Accept json
// @Produce json
// @Param request body ChangePwdRequest true "修改密码请求"
// @Success 200 {object} gin.H
// @Router /admin/reset [post]
func (h *Handler) ChangePassword(c *gin.Context) {
	aid, exists := c.Get("aid")
	if !exists {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	var req ChangePwdRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	if err := h.service.ChangePassword(c.Request.Context(), aid.(int), req.OldPassword, req.NewPassword); err != nil {
		if err == ErrInvalidPassword {
			common.Error(c, common.CodeInvalidPassword, err.Error())
		} else {
			common.Error(c, common.CodeInternalError, "修改密码失败")
		}
		return
	}

	common.Success(c, gin.H{"message": "密码修改成功"})
}

// BroadcastEmail 群发邮件
// @Summary 群发邮件给所有用户
// @Tags Admin
// @Accept json
// @Produce json
// @Param request body BroadcastEmailRequest true "群发邮件请求"
// @Success 200 {object} BroadcastEmailResponse
// @Router /admin/broadcast-email [post]
func (h *Handler) BroadcastEmail(c *gin.Context) {
	var req BroadcastEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	successCount, failCount, totalCount, err := h.service.BroadcastEmail(c.Request.Context(), req.Subject, req.Content)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.Success(c, BroadcastEmailResponse{
		SuccessCount: successCount,
		FailCount:    failCount,
		TotalCount:   totalCount,
	})
}

// GetAllUsers 获取所有用户
func (h *Handler) GetAllUsers(c *gin.Context) {
	users, err := h.service.GetAllUsers(c.Request.Context())
	if err != nil {
		common.Error(c, common.CodeInternalError, "获取用户列表失败")
		return
	}

	common.Success(c, users)
}

// DeleteUser 删除用户
func (h *Handler) DeleteUser(c *gin.Context) {
	uidStr := c.Param("uid")
	if uidStr == "" {
		common.Error(c, common.CodeInvalidParams, "请提供用户ID")
		return
	}

	uid, err := strconv.Atoi(uidStr)
	if err != nil {
		common.Error(c, common.CodeInvalidParams, "无效的用户ID")
		return
	}

	if err := h.service.DeleteUser(c.Request.Context(), uid); err != nil {
		common.Error(c, common.CodeInternalError, "删除用户失败")
		return
	}

	common.Success(c, gin.H{"message": "用户已删除"})
}
