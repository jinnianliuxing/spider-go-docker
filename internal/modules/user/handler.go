package user

import (
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"spider-go/internal/common"
	servicepkg "spider-go/internal/service"
	"strings"

	"github.com/gin-gonic/gin"
)

// Handler 用户HTTP处理器
type Handler struct {
	service           Service
	captchaService    CaptchaService
	qrLoginService    servicepkg.QrLoginService
	phoneLoginService servicepkg.PhoneLoginService
}

// NewHandler 创建用户处理器
func NewHandler(service Service, captchaService CaptchaService, qrLoginService servicepkg.QrLoginService, phoneLoginService servicepkg.PhoneLoginService) *Handler {
	return &Handler{
		service:           service,
		captchaService:    captchaService,
		qrLoginService:    qrLoginService,
		phoneLoginService: phoneLoginService,
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
	authenticated.POST("/mfa/send", h.SessionMFASend)          // 查询/会话命中多因素认证(40011)时触发短信验证码（spwd 可省略，用已存密码）
	authenticated.POST("/mfa/verify", h.SessionMFAVerify)      // 提交短信验证码，完成教务登录并缓存会话（不改绑定）
	authenticated.GET("/is-bind", h.CheckIsBind)               // 检查绑定状态
	authenticated.GET("/bind-status", h.GetBindStatus)         // 获取绑定状态（包含绑定次数信息）
	authenticated.POST("/wechat/bind", h.WeChatBind)           // 老用户绑定微信
	authenticated.POST("/update-name", h.UpdateName)           // 更新用户名
	authenticated.POST("/update-email", h.UpdateEmail)         // 更新邮箱

	// ===== 扫码绑定（i中南林 App）=====
	// 与密码绑定完全独立的两条流程，互不影响。
	authenticated.POST("/bind/qr/start", h.BindQrStart)       // 出二维码，返回 base64 PNG + session_id
	authenticated.POST("/bind/qr/poll", h.BindQrPoll)         // 轮询扫码状态（status 1/2/3）
	authenticated.POST("/bind/qr/complete", h.BindQrComplete) // 手机确认后完成绑定

	// ===== 手机号验证码绑定（i中南林 App 免密短信登录）=====
	// 两步流程：发码 → 提交验证码。同一个 complete 接口同时服务
	// 「首次绑定」与「会话过期后补一次验证码」，由后端按学号是否一致决定。
	authenticated.POST("/bind/phone/start", h.BindPhoneStart)       // 发送短信验证码，返回 session_id + 脱敏手机号
	authenticated.POST("/bind/phone/complete", h.BindPhoneComplete) // 提交验证码，完成绑定并缓存教务会话
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

// ============================================================================
// 扫码绑定（i中南林 App）
// ============================================================================
//
// 三步流程（与密码绑定完全独立）：
//  1. POST /user/bind/qr/start     → 后端出码，返回 { session_id, qr_image(base64 PNG) }
//  2. POST /user/bind/qr/poll      → 前端每 1~2 秒轮询，返回 { status, message }
//                                    status: 1 待扫 / 2 已扫待确认 / 3 已确认
//  3. POST /user/bind/qr/complete  → status=3 后调用，拿学号姓名并完成绑定
//
// ⚠️ stateKey 只保存在服务端 Redis，绝不返回给前端。
// ⚠️ session_id 与 uid 绑定，防止 A 的码被 B 用（串号）。

// BindQrStart 扫码绑定-生成二维码
// @Summary 扫码绑定-生成二维码
// @Tags User
// @Produce json
// @Success 200 {object} gin.H
// @Router /user/bind/qr/start [post]
func (h *Handler) BindQrStart(c *gin.Context) {
	uid, exists := c.Get("uid")
	if !exists {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	png, sessionID, err := h.qrLoginService.Start(c.Request.Context(), uid.(int))
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.Error(c, appErr.Code, appErr.Message)
		} else {
			common.Error(c, common.CodeInternalError, "生成二维码失败")
		}
		return
	}

	common.Success(c, gin.H{
		"session_id": sessionID,
		"qr_image":   "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
		"expires_in": 300, // 秒
	})
}

// BindQrPoll 扫码绑定-轮询扫码状态
// @Summary 扫码绑定-轮询状态
// @Tags User
// @Accept json
// @Produce json
// @Router /user/bind/qr/poll [post]
func (h *Handler) BindQrPoll(c *gin.Context) {
	uid, exists := c.Get("uid")
	if !exists {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}
	var req BindQrSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	status, message, err := h.qrLoginService.Poll(c.Request.Context(), req.SessionID, uid.(int))
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.Error(c, appErr.Code, appErr.Message)
		} else {
			common.Error(c, common.CodeInternalError, "轮询扫码状态失败")
		}
		return
	}

	common.Success(c, gin.H{"status": status, "message": message})
}

// BindQrComplete 扫码绑定-完成绑定
// @Summary 扫码绑定-完成绑定
// @Tags User
// @Accept json
// @Produce json
// @Router /user/bind/qr/complete [post]
func (h *Handler) BindQrComplete(c *gin.Context) {
	uid, exists := c.Get("uid")
	if !exists {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}
	var req BindQrSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	result, err := h.qrLoginService.Complete(c.Request.Context(), req.SessionID, uid.(int))
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.Error(c, appErr.Code, appErr.Message)
		} else {
			common.Error(c, common.CodeInternalError, "扫码登录失败")
		}
		return
	}

	// 用扫码结果完成绑定，并缓存扫码过程已建立的会话
	if err := h.service.BindJwcByQR(c.Request.Context(), uid.(int), result,
		c.ClientIP(), c.Request.UserAgent()); err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.Error(c, appErr.Code, appErr.Message)
		} else {
			common.Error(c, common.CodeInternalError, "扫码绑定失败")
		}
		return
	}

	common.Success(c, gin.H{
		"message": "扫码绑定成功",
		"sid":     result.Sid,
		"name":    result.Name,
	})
}

// ============================================================================
// 手机号验证码绑定（i中南林 App 免密短信登录）
// ============================================================================
//
// 两步流程（与密码/扫码绑定并列的第三种方式）：
//  1. POST /user/bind/phone/start    → 传手机号（可省略，走库里已绑的），后端发短信，
//     返回 { session_id, masked_phone, expires_in }
//  2. POST /user/bind/phone/complete → 传 { session_id, code }，换票并完成绑定
//
// ⚠️ 与扫码最大的区别：手机号绑定**没有教务密码**，但会话过期后用户只要再补一次
// 验证码即可恢复，因此「重新登录」和「首次绑定」共用同一对接口：
//   学号与已绑一致 → 视为补验证码，只刷新会话；不一致 → 拒绝（防串号）。
// ⚠️ session_id 与 uid 绑定，防止 A 发起的验证码被 B 用。

// BindPhoneStart 手机号绑定-发送短信验证码
// @Summary 手机号绑定-发送验证码
// @Tags User
// @Accept json
// @Produce json
// @Param request body BindPhoneStartRequest false "phone 可省略；省略时用库里已保存的绑定手机号"
// @Success 200 {object} gin.H
// @Router /user/bind/phone/start [post]
func (h *Handler) BindPhoneStart(c *gin.Context) {
	uid, exists := c.Get("uid")
	if !exists {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	var req BindPhoneStartRequest
	// 请求体允许为空（会话过期后的「一键重发」就不带手机号）
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	phone := strings.TrimSpace(req.Phone)
	if phone == "" {
		// 缺省用库里已绑的手机号：会话过期后用户不必再输一遍号码
		user, err := h.service.GetUserInfo(c.Request.Context(), uid.(int))
		if err != nil {
			common.Error(c, common.CodeUserNotFound, "获取用户信息失败")
			return
		}
		phone = strings.TrimSpace(user.BindPhone)
		if phone == "" {
			common.Error(c, common.CodeInvalidParams, "请填写手机号")
			return
		}
	}

	send, err := h.phoneLoginService.Start(c.Request.Context(), uid.(int), phone)
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.Error(c, appErr.Code, appErr.Message)
		} else {
			common.Error(c, common.CodeInternalError, "发送验证码失败")
		}
		return
	}

	common.Success(c, gin.H{
		"session_id":   send.SessionID,
		"masked_phone": send.MaskedPhone,
		"expires_in":   300, // 秒，与后端 phoneSessionTTL 对齐
		"message":      "验证码已发送",
		// hint 是教务端原话（如「短信可能会存在延迟或手机未绑定用户」）——
		// 教务端不校验号码是否真的绑定，这句必须透给前端当预警，否则用户会干等短信
		"hint": send.Hint,
	})
}

// BindPhoneComplete 手机号绑定-提交验证码
// @Summary 手机号绑定-提交验证码
// @Tags User
// @Accept json
// @Produce json
// @Param request body BindPhoneCompleteRequest true "提交验证码请求"
// @Success 200 {object} gin.H
// @Router /user/bind/phone/complete [post]
func (h *Handler) BindPhoneComplete(c *gin.Context) {
	uid, exists := c.Get("uid")
	if !exists {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	var req BindPhoneCompleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	result, err := h.phoneLoginService.Complete(c.Request.Context(), req.SessionID, uid.(int), req.Code)
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.Error(c, appErr.Code, appErr.Message)
		} else {
			common.Error(c, common.CodeInternalError, "手机号登录失败")
		}
		return
	}

	// 用登录结果完成绑定（学号一致时等同"只刷新会话"，见 BindJwcByPhone 注释）
	if err := h.service.BindJwcByPhone(c.Request.Context(), uid.(int), result,
		c.ClientIP(), c.Request.UserAgent()); err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.Error(c, appErr.Code, appErr.Message)
		} else {
			common.Error(c, common.CodeInternalError, "手机号绑定失败")
		}
		return
	}

	common.Success(c, gin.H{
		"message": "手机号绑定成功",
		"sid":     result.Sid,
		"name":    result.Name,
		"phone":   result.Phone,
	})
}

// UnbindJwc 注销教务系统绑定
// @Summary 注销教务系统绑定（清除学号+密码+所有同步数据）
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

// SessionMFASendRequest 查询/会话场景触发短信验证码请求
// Spwd 可省略：省略时后端自动使用数据库里已保存的教务密码
type SessionMFASendRequest struct {
	Spwd string `json:"spwd"`
}

// SessionMFAVerifyRequest 查询/会话场景提交短信验证码请求
type SessionMFAVerifyRequest struct {
	ChallengeID string `json:"challenge_id" binding:"required"`
	Code        string `json:"code" binding:"required"`
}

// SessionMFASend 在任意查询页面命中 40011（教务系统要求多因素认证，如服务器/异地 IP 登录）时，
// 前端调用本接口触发短信验证码。spwd 不传则自动使用已保存的教务密码，
// 用户因此只需输入收到的短信码，不必再输一次密码。
// @Summary 教务会话-发送短信验证码（多因素认证）
// @Tags User
// @Accept JSON
// @Produce JSON
// @Param request body SessionMFASendRequest false "可省略；不传则用已保存的教务密码"
// @Success 200 {object} gin.H
// @Router /user/mfa/send [post]
func (h *Handler) SessionMFASend(c *gin.Context) {
	uid, exists := c.Get("uid")
	if !exists {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	var req SessionMFASendRequest
	// 请求体允许为空（前端自动发送时就不带密码）
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	challengeID, maskedPhone, err := h.service.SessionMFASend(c.Request.Context(), uid.(int), req.Spwd, c.ClientIP(), c.Request.UserAgent())
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

// SessionMFAVerify 提交短信验证码，完成教务系统登录并缓存会话。
// 与 /user/bind/mfa/verify 的区别：不修改绑定关系、也不清除刚拿到的会话缓存，
// 校验通过后前端只要重试原来的查询即可正常返回数据。
// @Summary 教务会话-提交短信验证码（多因素认证）
// @Tags User
// @Accept JSON
// @Produce JSON
// @Param request body SessionMFAVerifyRequest true "提交验证码请求"
// @Success 200 {object} gin.H
// @Router /user/mfa/verify [post]
func (h *Handler) SessionMFAVerify(c *gin.Context) {
	if _, exists := c.Get("uid"); !exists {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	var req SessionMFAVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, err.Error())
		return
	}

	if err := h.service.SessionMFAVerify(c.Request.Context(), req.ChallengeID, req.Code); err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.Error(c, appErr.Code, appErr.Message)
		} else {
			common.Error(c, common.CodeInternalError, err.Error())
		}
		return
	}

	common.Success(c, gin.H{"message": "验证成功"})
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
