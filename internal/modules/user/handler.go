package user

import (
	"errors"
	"net/http"
	"spider-go/internal/common"

	"github.com/gin-gonic/gin"
)

// Handler 用户HTTP处理器
type Handler struct {
	service        Service
	captchaService CaptchaService
}

// NewHandler 创建用户处理器
func NewHandler(service Service, captchaService CaptchaService) *Handler {
	return &Handler{
		service:        service,
		captchaService: captchaService,
	}
}

// RegisterRoutes 注册路由
func (h *Handler) RegisterRoutes(public *gin.RouterGroup, authenticated *gin.RouterGroup) {
	// 公开路由（无需认证）
	publicUser := public.Group("/user")
	{
		publicUser.POST("/register", h.Register)                   // 用户注册
		publicUser.POST("/login", h.Login)                         // 用户登录
		publicUser.POST("/login/captcha", h.LoginByCaptcha)        // 邮箱验证码登录
		publicUser.POST("/reset-password", h.ResetPassword)        // 重置密码
		publicUser.POST("/wechat/login", h.WeChatLogin)            // 微信登录/注册,app也可以用，拉起微信
		publicUser.POST("/login/magic-link/send", h.SendMagicLink) // Magic Link：发送登录链接到邮箱
	}

	// Magic Link 回调路由（公开，无需认证，用于邮箱中的链接跳转）
	public.GET("/auth/callback", h.LoginByMagicLink)

	// 验证码路由（公开）
	captcha := public.Group("/captcha")
	{
		captcha.POST("/send", h.SendEmailCaptcha) // 发送邮箱验证码
	}

	authenticated.GET("/info", h.GetUserInfo)                  // 获取用户信息
	authenticated.POST("/bind", h.BindJwc)                     // 绑定教务系统
	authenticated.POST("/unbind", h.UnbindJwc)                 // 注销教务系统绑定
	authenticated.POST("/bind/mfa/send", h.BindJwcMFASend)     // 绑定教务系统-命中短信验证码MFA时调用，触发发送验证码
	authenticated.POST("/bind/mfa/verify", h.BindJwcMFAVerify) // 绑定教务系统-提交短信验证码，完成绑定
	authenticated.GET("/is-bind", h.CheckIsBind)               // 检查绑定状态
	authenticated.GET("/bind-status", h.GetBindStatus)         // 获取绑定状态（包含绑定次数信息）
	authenticated.POST("/wechat/bind", h.WeChatBind)           // 老用户绑定微信
	authenticated.POST("/update-name", h.UpdateName)           // 更新用户名
	authenticated.POST("/update-email", h.UpdateEmail)         // 更新邮箱
}

// Register 用户注册
// @Summary 用户注册
// @Tags User
// @Accept json
// @Produce json
// @Param request body RegisterRequest true "注册请求"
// @Success 200 {object} gin.H
// @Router /user/register [post]
func (h *Handler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	token, err := h.service.Register(c.Request.Context(), req.Email, req.Password, req.Name, req.Captcha)
	if err != nil {
		switch err {
		case ErrEmailAlreadyExists:
			common.Error(c, common.CodeUserAlreadyExists, err.Error())
		case ErrInvalidCaptcha:
			common.Error(c, common.CodeCaptchaInvalid, err.Error())
		default:
			common.Error(c, common.CodeInternalError, "注册失败")
		}
		return
	}

	common.Success(c, gin.H{
		"token":   token,
		"message": "注册成功",
	})
}

// LoginByCaptchaRequest 验证码登录请求
type LoginByCaptchaRequest struct {
	Email   string `json:"email" binding:"required,email"`
	Captcha string `json:"captcha" binding:"required"`
}

// LoginByCaptcha 邮箱验证码登录
// @Summary 邮箱验证码登录
// @Tags User
// @Accept json
// @Produce json
// @Param request body LoginByCaptchaRequest true "验证码登录请求"
// @Success 200 {object} LoginResponse
// @Router /user/login/captcha [post]
func (h *Handler) LoginByCaptcha(c *gin.Context) {
	var req LoginByCaptchaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	token, user, err := h.service.LoginByCaptcha(c.Request.Context(), req.Email, req.Captcha)
	if err != nil {
		switch err {
		case ErrUserNotFound:
			common.Error(c, common.CodeUserNotFound, err.Error())
		case ErrInvalidCaptcha:
			common.Error(c, common.CodeCaptchaInvalid, err.Error())
		default:
			common.Error(c, common.CodeInternalError, "登录失败")
		}
		return
	}

	common.Success(c, LoginResponse{
		Token: token,
		User:  user.ToResponse(),
	})
}

// SendMagicLinkRequest 发送 Magic Link 请求
type SendMagicLinkRequest struct {
	Email string `json:"email" binding:"required,email"`
}

// SendMagicLink 发送登录链接到邮箱
func (h *Handler) SendMagicLink(c *gin.Context) {
	var req SendMagicLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	if err := h.service.SendMagicLink(c.Request.Context(), req.Email); err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.Error(c, appErr.Code, appErr.Message)
		} else {
			common.Error(c, common.CodeInternalError, err.Error())
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "登录链接已发送到邮箱，请查收",
	})
}

// LoginByMagicLink 通过 Magic Link token 登录
func (h *Handler) LoginByMagicLink(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		common.Error(c, common.CodeInvalidParams, "缺少 token 参数")
		return
	}

	tokenString, user, err := h.service.LoginByMagicLink(c.Request.Context(), token)
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.Error(c, appErr.Code, appErr.Message)
		} else {
			common.Error(c, common.CodeInternalError, "登录失败")
		}
		return
	}

	common.Success(c, LoginResponse{
		Token: tokenString,
		User:  user.ToResponse(),
	})
}

// Login 用户登录
// @Summary 用户登录
// @Tags User
// @Accept json
// @Produce json
// @Param request body LoginRequest true "登录请求"
// @Success 200 {object} LoginResponse
// @Router /user/login [post]
func (h *Handler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	token, user, err := h.service.Login(c.Request.Context(), req.Email, req.Password)
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
		User:  user.ToResponse(),
	})
}

// ResetPassword 重置密码
// @Summary 重置密码
// @Tags User
// @Accept json
// @Produce json
// @Param request body ResetPasswordRequest true "重置密码请求"
// @Success 200 {object} gin.H
// @Router /user/reset-password [post]
func (h *Handler) ResetPassword(c *gin.Context) {
	var req ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	if err := h.service.ResetPassword(c.Request.Context(), req.Email, req.Password, req.Captcha); err != nil {
		switch err {
		case ErrUserNotFound:
			common.Error(c, common.CodeUserNotFound, err.Error())
		case ErrInvalidCaptcha:
			common.Error(c, common.CodeCaptchaInvalid, err.Error())
		default:
			common.Error(c, common.CodeInternalError, "重置密码失败")
		}
		return
	}

	common.Success(c, gin.H{"message": "密码重置成功"})
}

// GetUserInfo 获取用户信息
// @Summary 获取用户信息
// @Tags User
// @Produce json
// @Success 200 {object} UserResponse
// @Router /user/info [get]
func (h *Handler) GetUserInfo(c *gin.Context) {
	uid, exists := c.Get("uid")
	if !exists {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	user, err := h.service.GetUserInfo(c.Request.Context(), uid.(int))
	if err != nil {
		common.Error(c, common.CodeUserNotFound, "获取用户信息失败")
		return
	}

	common.Success(c, user.ToResponse())
}

// BindJwc 绑定教务系统
// @Summary 绑定教务系统
// @Tags User
// @Accept JSON
// @Produce JSON
// @Param request body BindJwcRequest true "绑定请求"
// @Success 200 {object} gin.H
// @Router /user/bind [post]
func (h *Handler) BindJwc(c *gin.Context) {
	uid, exists := c.Get("uid")
	if !exists {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	var req BindJwcRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	// 获取客户端IP和User-Agent
	ipAddress := c.ClientIP()
	userAgent := c.Request.UserAgent()

	if err := h.service.BindJwc(c.Request.Context(), uid.(int), req.Sid, req.Spwd, ipAddress, userAgent); err != nil {
		// 使用AppError统一处理错误响应
		if appErr, ok := err.(*common.AppError); ok {
			common.Error(c, appErr.Code, appErr.Message)
		} else {
			common.Error(c, common.CodeInternalError, err.Error())
		}
		return
	}

	common.Success(c, gin.H{"message": "绑定成功"})
}

// UnbindJwc 注销教务系统绑定
// @Summary 注销教务系统绑定（清除学号+密码+所有同步数据，1天冷却期）
// @Tags User
// @Produce json
// @Success 200 {object} gin.H
// @Router /user/unbind [post]
func (h *Handler) UnbindJwc(c *gin.Context) {
	uid, exists := c.Get("uid")
	if !exists {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	ipAddress := c.ClientIP()
	userAgent := c.Request.UserAgent()

	if err := h.service.UnbindJwc(c.Request.Context(), uid.(int), ipAddress, userAgent); err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.Error(c, appErr.Code, appErr.Message)
		} else {
			common.Error(c, common.CodeInternalError, err.Error())
		}
		return
	}

	common.Success(c, gin.H{"message": "注销成功，所有教务系统数据已清除"})
}

// BindJwcMFASendRequest 触发短信验证码请求
type BindJwcMFASendRequest struct {
	Sid  string `json:"sid"` // 学号（不传则自动填充已绑定的学号）
	Spwd string `json:"spwd" binding:"required"`
}

// BindJwcMFAVerifyRequest 提交短信验证码请求
type BindJwcMFAVerifyRequest struct {
	ChallengeID string `json:"challenge_id" binding:"required"`
	Code        string `json:"code" binding:"required"`
}

// BindJwcMFASend 绑定教务系统时命中短信验证码 MFA，触发发送验证码
// 前端流程：先调 /user/bind，如果返回错误码 40011（需要多因素认证），
// 就改调这个接口，传同样的 sid/spwd，拿到 challenge_id 和打码手机号，提示用户去查收短信
// @Summary 绑定教务系统-发送短信验证码
// @Tags User
// @Accept JSON
// @Produce JSON
// @Param request body BindJwcMFASendRequest true "发送验证码请求"
// @Success 200 {object} gin.H
// @Router /user/bind/mfa/send [post]
func (h *Handler) BindJwcMFASend(c *gin.Context) {
	uid, exists := c.Get("uid")
	if !exists {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	var req BindJwcMFASendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	ipAddress := c.ClientIP()
	userAgent := c.Request.UserAgent()

	challengeID, maskedPhone, err := h.service.BindJwcStartMFA(c.Request.Context(), uid.(int), req.Sid, req.Spwd, ipAddress, userAgent)
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.Error(c, appErr.Code, appErr.Message)
		} else {
			common.Error(c, common.CodeInternalError, err.Error())
		}
		return
	}

	common.Success(c, gin.H{
		"challenge_id": challengeID,
		"masked_phone": maskedPhone,
		"message":      "验证码已发送",
	})
}

// BindJwcMFAVerify 提交短信验证码，完成教务系统绑定
// @Summary 绑定教务系统-提交短信验证码
// @Tags User
// @Accept JSON
// @Produce JSON
// @Param request body BindJwcMFAVerifyRequest true "提交验证码请求"
// @Success 200 {object} gin.H
// @Router /user/bind/mfa/verify [post]
func (h *Handler) BindJwcMFAVerify(c *gin.Context) {
	if _, exists := c.Get("uid"); !exists {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	var req BindJwcMFAVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	if err := h.service.BindJwcCompleteMFA(c.Request.Context(), req.ChallengeID, req.Code); err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.Error(c, appErr.Code, appErr.Message)
		} else {
			common.Error(c, common.CodeInternalError, err.Error())
		}
		return
	}

	common.Success(c, gin.H{"message": "绑定成功"})
}

// GetBindStatus 获取绑定状态
// @Summary 获取绑定状态
// @Tags User
// @Produce json
// @Success 200 {object} BindStatusResponse
// @Router /user/bind-status [get]
func (h *Handler) GetBindStatus(c *gin.Context) {
	uid, exists := c.Get("uid")
	if !exists {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	status, err := h.service.GetBindStatus(c.Request.Context(), uid.(int))
	if err != nil {
		common.Error(c, common.CodeInternalError, "获取绑定状态失败")
		return
	}

	common.Success(c, status)
}

// CheckIsBind 检查绑定状态
// @Summary 检查绑定状态
// @Tags User
// @Produce json
// @Success 200 {object} gin.H
// @Router /user/is-bind [get]
func (h *Handler) CheckIsBind(c *gin.Context) {
	uid, exists := c.Get("uid")
	if !exists {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	isBind, err := h.service.CheckIsBind(c.Request.Context(), uid.(int))
	if err != nil {
		common.Error(c, common.CodeUserNotFound, "获取绑定状态失败")
		return
	}

	c.JSON(http.StatusOK, gin.H{"is_bind": isBind})
}

// SendEmailCaptcha 发送邮箱验证码
// @Summary 发送邮箱验证码
// @Tags Captcha
// @Accept json
// @Produce json
// @Param request body SendEmailCaptchaRequest true "发送验证码请求"
// @Success 200 {object} gin.H
// @Router /captcha/send [post]
func (h *Handler) SendEmailCaptcha(c *gin.Context) {
	var req SendEmailCaptchaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	if err := h.captchaService.SendEmailCaptcha(c.Request.Context(), req.Email); err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.ErrorWithAppError(c, appErr)
		} else {
			common.Error(c, common.CodeInternalError, "发送验证码失败")
		}
		return
	}

	common.Success(c, gin.H{"message": "验证码已发送"})
}

// WeChatLogin 微信登录/注册
// @Summary 微信登录/注册
// @Tags User
// @Accept json
// @Produce json
// @Param request body WeChatLoginRequest true "微信登录请求"
// @Success 200 {object} LoginResponse
// @Router /user/wechat/login [post]
func (h *Handler) WeChatLogin(c *gin.Context) {
	var req WeChatLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	token, user, err := h.service.WeChatLogin(c.Request.Context(), req.Code)
	if err != nil {
		common.Error(c, common.CodeWeChatLoginFailed, err.Error())
		return
	}

	common.Success(c, LoginResponse{
		Token: token,
		User:  user.ToResponse(),
	})
}

// WeChatBind 老用户绑定微信
// @Summary 老用户绑定微信
// @Tags User
// @Accept JSON
// @Produce JSON
// @Param request body WeChatLoginRequest true "微信绑定请求"
// @Success 200 {object} gin.H
// @Router /user/wechat/bind [post]
func (h *Handler) WeChatBind(c *gin.Context) {
	uid, exists := c.Get("uid")
	if !exists {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	var req WeChatLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	if err := h.service.WeChatBind(c.Request.Context(), uid.(int), req.Code); err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.Error(c, appErr.Code, appErr.Message)
		} else {
			common.Error(c, common.CodeWeChatBindFailed, err.Error())
		}
		return
	}

	common.Success(c, gin.H{"message": "绑定微信成功"})
}

// UpdateName 更新用户名
// @Summary 更新用户名
// @Tags User
// @Accept json
// @Produce json
// @Param request body UpdateNameRequest true "更新用户名请求"
// @Success 200 {object} gin.H
// @Router /update-name [post]
func (h *Handler) UpdateName(c *gin.Context) {
	uid, exists := c.Get("uid")
	if !exists {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	var req UpdateNameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	if err := h.service.UpdateName(c.Request.Context(), uid.(int), req.Name); err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.Error(c, appErr.Code, appErr.Message)
		} else {
			common.Error(c, common.CodeInternalError, "更新用户名失败")
		}
		return
	}

	common.Success(c, gin.H{"message": "更新用户名成功"})
}

// UpdateEmail 更新邮箱
// @Summary 更新邮箱
// @Tags User
// @Accept json
// @Produce json
// @Param request body UpdateEmailRequest true "更新邮箱请求"
// @Success 200 {object} gin.H
// @Router /update-email [post]
func (h *Handler) UpdateEmail(c *gin.Context) {
	uid, exists := c.Get("uid")
	if !exists {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	var req UpdateEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	if err := h.service.UpdateEmail(c.Request.Context(), uid.(int), req.Email, req.Captcha); err != nil {
		var appErr *common.AppError
		switch {
		case err == ErrInvalidCaptcha:
			common.Error(c, common.CodeCaptchaInvalid, err.Error())
		case err == ErrEmailAlreadyExists:
			common.Error(c, common.CodeUserAlreadyExists, err.Error())
		case errors.As(err, &appErr):
			common.Error(c, appErr.Code, appErr.Message)
		default:
			common.Error(c, common.CodeInternalError, "更新邮箱失败")
		}
		return
	}

	common.Success(c, gin.H{"message": "更新邮箱成功"})
}
