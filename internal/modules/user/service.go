package user

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"spider-go/internal/cache"
	"spider-go/internal/common"
	"spider-go/internal/service"
	"spider-go/internal/shared"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = common.NewAppError(common.CodeInvalidPassword, "用户不存在或密码错误")
	ErrEmailAlreadyExists = common.NewAppError(common.CodeUserAlreadyExists, "邮箱已被注册")
	ErrInvalidCaptcha     = common.NewAppError(common.CodeCaptchaInvalid, "验证码错误")
	ErrEmptyParams        = common.NewAppError(common.CodeInvalidParams, "参数不能为空")
	ErrWeChatAlreadyBind  = common.NewAppError(common.CodeWeChatAlreadyBind, "该微信已绑定其他账号")
)

// Service 用户服务接口
type Service interface {
	// Register 注册
	Register(ctx context.Context, email, password, name, captcha string) (token string, err error)
	// Login 用户登录
	Login(ctx context.Context, email, password string) (token string, user *User, err error)
	// LoginByCaptcha 邮箱验证码登录
	LoginByCaptcha(ctx context.Context, email, captcha string) (token string, user *User, err error)
	// ResetPassword 重置密码
	ResetPassword(ctx context.Context, email, newPassword, captcha string) error
	// WeChatLogin 微信注册登录相关
	WeChatLogin(ctx context.Context, code string) (token string, user *User, err error)
	// WeChatBind 老用户绑定微信
	WeChatBind(ctx context.Context, uid int, code string) (err error)
	// GetUserInfo 用户信息
	GetUserInfo(ctx context.Context, uid int) (*User, error)

	// BindJwc 教务系统绑定相关
	BindJwc(ctx context.Context, uid int, sid, spwd, ipAddress, userAgent string) error
	// BindJwcStartMFA 绑定时命中短信验证码 MFA，调用这个触发发送验证码
	BindJwcStartMFA(ctx context.Context, uid int, sid, spwd, ipAddress, userAgent string) (challengeID string, maskedPhone string, err error)
	// BindJwcCompleteMFA 提交短信验证码，完成绑定
	BindJwcCompleteMFA(ctx context.Context, challengeID, code string) error
	// CheckIsBind 检查是否绑定教务处
	CheckIsBind(ctx context.Context, uid int) (bool, error)
	// GetBindStatus 获取绑定状态（包含绑定次数信息）
	GetBindStatus(ctx context.Context, uid int) (*BindStatusResponse, error)

	// UpdateName 更新用户名
	UpdateName(ctx context.Context, uid int, name string) error
	// SendMagicLink 发送 Magic Link 登录链接到邮箱
	SendMagicLink(ctx context.Context, email string) error
	// LoginByMagicLink 通过 Magic Link token 登录
	LoginByMagicLink(ctx context.Context, token string) (tokenString string, user *User, err error)
	// UpdateEmail 更新邮箱（需要验证码）
	UpdateEmail(ctx context.Context, uid int, email, captcha string) error
	// UnbindJwc 注销教务系统绑定（清除 sid+spwd+所有同步数据，1天冷却期）
	UnbindJwc(ctx context.Context, uid int, ipAddress, userAgent string) error
}

// pendingBindMFA 教务系统绑定走短信验证码流程时的临时上下文（仅内存，不落库）
type pendingBindMFA struct {
	uid       int
	sid       string
	spwd      string
	ipAddress string
	userAgent string
	expiresAt time.Time
}

// userService 用户服务实现
type userService struct {
	repo            Repository
	sessionService  service.SessionService
	captchaService  CaptchaService
	dauService      service.DAUService
	emailService    service.EmailService
	magicLinkCache  cache.MagicLinkCache
	jwtSecret       []byte
	jwtIssuer       string
	jwtExpire       time.Duration
	appid           string
	appsecret       string
	frontendBaseURL string

	bindMFAMu sync.Mutex
	bindMFA   map[string]*pendingBindMFA // challengeID -> 待完成的绑定请求
}

// NewService 创建用户服务
func NewService(
	repo Repository,
	sessionService service.SessionService,
	captchaService CaptchaService,
	dauService service.DAUService,
	emailService service.EmailService,
	magicLinkCache cache.MagicLinkCache,
	jwtSecret string,
	jwtIssuer string,
	appid string,
	appsecret string,
	frontendBaseURL string,
) Service {
	return &userService{
		repo:            repo,
		sessionService:  sessionService,
		captchaService:  captchaService,
		dauService:      dauService,
		emailService:    emailService,
		magicLinkCache:  magicLinkCache,
		jwtSecret:       []byte(jwtSecret),
		jwtIssuer:       jwtIssuer,
		jwtExpire:       24 * time.Hour, // 24小时
		appid:           appid,
		appsecret:       appsecret,
		frontendBaseURL: frontendBaseURL,
		bindMFA:         make(map[string]*pendingBindMFA),
	}
}

// Register 用户注册
func (s *userService) Register(ctx context.Context, email, password, name, captcha string) (string, error) {
	// 检查用户是否已存在
	existing, err := s.repo.FindByEmail(ctx, email)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return "", err
	}
	if existing != nil {
		return "", ErrEmailAlreadyExists
	}

	// 验证验证码
	if err := s.captchaService.VerifyEmailCaptcha(ctx, email, captcha); err != nil {
		return "", ErrInvalidCaptcha
	}

	// 加密密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}

	// 创建用户
	user := &User{
		Name:      name,
		Email:     email,
		Password:  string(hashedPassword),
		CreatedAt: time.Now(),
	}

	if err := s.repo.Create(ctx, user); err != nil {
		return "", err
	}

	// 生成JWT token
	claims := shared.UserClaims{
		Uid:  user.Uid,
		Name: user.Name,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.jwtExpire)),
			Issuer:    s.jwtIssuer,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

// Login 用户登录
func (s *userService) Login(ctx context.Context, email, password string) (string, *User, error) {
	// 查找用户
	user, err := s.repo.FindByEmail(ctx, email)
	if err != nil {
		return "", nil, ErrInvalidCredentials
	}

	// 验证密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return "", nil, ErrInvalidCredentials
	}

	// 记录DAU
	_ = s.dauService.RecordUserActivity(ctx, user.Uid)

	// 生成JWT token
	claims := shared.UserClaims{
		Uid:  user.Uid,
		Name: user.Name,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.jwtExpire)),
			Issuer:    s.jwtIssuer,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", nil, err
	}

	// 实时校验教务系统绑定状态
	user = s.verifyJwcBindingOnLogin(ctx, user)

	return tokenString, user, nil
}

// LoginByCaptcha 邮箱验证码登录
func (s *userService) LoginByCaptcha(ctx context.Context, email, captcha string) (string, *User, error) {
	// 查找用户
	user, err := s.repo.FindByEmail(ctx, email)
	if err != nil {
		return "", nil, ErrUserNotFound
	}

	// 验证验证码
	if err := s.captchaService.VerifyEmailCaptcha(ctx, email, captcha); err != nil {
		return "", nil, ErrInvalidCaptcha
	}

	// 记录DAU
	_ = s.dauService.RecordUserActivity(ctx, user.Uid)

	// 生成JWT token
	claims := shared.UserClaims{
		Uid:  user.Uid,
		Name: user.Name,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.jwtExpire)),
			Issuer:    s.jwtIssuer,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", nil, err
	}

	// 实时校验教务系统绑定状态
	user = s.verifyJwcBindingOnLogin(ctx, user)

	return tokenString, user, nil
}

// ResetPassword 重置密码
func (s *userService) ResetPassword(ctx context.Context, email, newPassword, captcha string) error {
	// 查找用户
	user, err := s.repo.FindByEmail(ctx, email)
	if err != nil {
		return ErrUserNotFound
	}

	// 验证验证码
	if err := s.captchaService.VerifyEmailCaptcha(ctx, email, captcha); err != nil {
		return ErrInvalidCaptcha
	}

	// 加密新密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	// 更新密码
	return s.repo.UpdatePassword(ctx, user.Uid, string(hashedPassword))
}

// GetUserInfo 获取用户信息
func (s *userService) GetUserInfo(ctx context.Context, uid int) (*User, error) {
	user, err := s.repo.FindByID(ctx, uid)
	if err != nil {
		return nil, err
	}

	return user, nil
}

// BindJwc 绑定教务系统（学号一旦绑定不可更换）
func (s *userService) BindJwc(ctx context.Context, uid int, sid, spwd, ipAddress, userAgent string) error {
	if spwd == "" {
		return ErrEmptyParams
	}

	// 如果前端没有传学号，自动从数据库获取已绑定的学号
	if sid == "" {
		user, err := s.repo.FindByID(ctx, uid)
		if err != nil {
			return common.NewAppError(common.CodeUserNotFound, "获取用户信息失败")
		}
		if user.Sid == "" {
			return common.NewAppError(common.CodeInvalidParams, "未找到已绑定的学号，请传入学号进行首次绑定")
		}
		sid = user.Sid
	}

	// 判断教务系统密码含有大小写字符，数字
	hasUpper := regexp.MustCompile(`[A-Z]`).MatchString(spwd)
	hasLower := regexp.MustCompile(`[a-z]`).MatchString(spwd)
	hasDigit := regexp.MustCompile(`\d`).MatchString(spwd)
	if !(hasUpper && hasLower && hasDigit) {
		return common.NewAppError(common.CodeInvalidParams, "教务系统密码需包含大写字母、小写字母和数字（请使用i中南林APP账号密码）")
	}

	// 1. 查询用户当前绑定状态
	user, err := s.repo.FindByID(ctx, uid)
	if err != nil {
		return err
	}

	// 2. 检查是否已绑定学号（学号一旦绑定不可更换）
	if user.Sid != "" && user.Sid != sid {
		_ = s.logBindAttempt(ctx, uid, user.Sid, sid, BindStatusFailedLimit, "学号已绑定，不允许更换", ipAddress, userAgent)
		return common.NewAppError(common.CodeBindLimitExceeded, "学号已绑定，不允许更换。如需更换请联系管理员")
	}

	// 3. 判断是否为相同学号（只修改密码）
	isSameSid := (user.Sid != "" && user.Sid == sid)

	// 4. 验证教务系统账号
	if err := s.sessionService.LoginCheck(ctx, sid, spwd); err != nil {
		// 命中短信验证码 MFA：这不算账号密码错误，原样把错误透传给前端，
		// 前端看到这个错误码后应该改用 BindJwcStartMFA / /user/bind/mfa/send 走短信验证流程
		if appErr, ok := err.(*common.AppError); ok && appErr.Code == common.CodeJwcMFARequired {
			return appErr
		}
		// 记录失败日志
		_ = s.logBindAttempt(ctx, uid, user.Sid, sid, BindStatusFailedAuth, "教务系统账号或密码错误", ipAddress, userAgent)
		return common.NewAppError(common.CodeJwcLoginFailed, "用户名或密码错误")
	}

	// 5. 开启事务：更新绑定信息
	tx := s.repo.(*repository).db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 5.1 更新用户表
	oldSid := user.Sid
	now := time.Now()

	// 基础更新字段
	updates := map[string]interface{}{
		"sid":          sid,
		"spwd":         spwd,
		"last_bind_at": now,
	}

	// 首次绑定时记录绑定次数
	if !isSameSid {
		updates["total_bind_count"] = user.TotalBindCount + 1
	}

	if err := tx.WithContext(ctx).Model(&User{}).Where("uid = ?", uid).Updates(updates).Error; err != nil {
		tx.Rollback()
		_ = s.logBindAttempt(ctx, uid, oldSid, sid, BindStatusFailedOther, fmt.Sprintf("更新数据库失败: %v", err), ipAddress, userAgent)
		return common.NewAppError(common.CodeDatabaseError, "绑定失败，请稍后重试")
	}

	// 5.2 记录绑定日志
	bindLog := &JwcBindLog{
		Uid:        uid,
		OldSid:     oldSid,
		NewSid:     sid,
		BindStatus: BindStatusSuccess,
		IpAddress:  ipAddress,
		UserAgent:  userAgent,
		CreatedAt:  now,
	}
	if err := tx.WithContext(ctx).Create(bindLog).Error; err != nil {
		tx.Rollback()
		return common.NewAppError(common.CodeDatabaseError, "记录日志失败")
	}

	// 5.3 提交事务
	if err := tx.Commit().Error; err != nil {
		return common.NewAppError(common.CodeDatabaseError, "提交事务失败")
	}

	// 6. 清除旧的教务系统会话缓存
	_ = s.sessionService.InvalidateSession(ctx, uid)

	// 7. 主动登录并缓存会话，避免查询时重新登录失败
	if err := s.sessionService.LoginAndCache(ctx, uid, sid, spwd); err != nil {
		log.Printf("[BindJwc] 缓存会话失败，但不影响绑定结果：uid=%d, err=%v", uid, err)
		// 不返回错误，绑定已经成功
	}

	return nil
}

// logBindAttempt 记录绑定尝试日志（辅助方法）
func (s *userService) logBindAttempt(ctx context.Context, uid int, oldSid, newSid string, status int, errMsg, ipAddress, userAgent string) error {
	log := &JwcBindLog{
		Uid:        uid,
		OldSid:     oldSid,
		NewSid:     newSid,
		BindStatus: status,
		ErrorMsg:   errMsg,
		IpAddress:  ipAddress,
		UserAgent:  userAgent,
		CreatedAt:  time.Now(),
	}
	return s.repo.(*repository).db.WithContext(ctx).Create(log).Error
}

// cleanExpiredBindMFA 清理过期的待验证绑定请求，调用前必须已持有 bindMFAMu 锁
func (s *userService) cleanExpiredBindMFA() {
	now := time.Now()
	for id, p := range s.bindMFA {
		if now.After(p.expiresAt) {
			delete(s.bindMFA, id)
		}
	}
}

// BindJwcStartMFA 绑定教务系统时如果命中短信验证码 MFA（即 BindJwc 返回了 CodeJwcMFARequired），
// 前端改调用这个接口触发发送短信验证码，拿到 challengeID 后再调 BindJwcCompleteMFA 完成绑定。
// 注意：这一步还不会写数据库，账号密码是否真的正确要等验证码校验通过、走完完整登录后才知道。
func (s *userService) BindJwcStartMFA(ctx context.Context, uid int, sid, spwd, ipAddress, userAgent string) (string, string, error) {
	if spwd == "" {
		return "", "", ErrEmptyParams
	}

	// 如果前端没有传学号，自动从数据库获取已绑定的学号
	if sid == "" {
		user, err := s.repo.FindByID(ctx, uid)
		if err != nil {
			return "", "", common.NewAppError(common.CodeUserNotFound, "获取用户信息失败")
		}
		if user.Sid == "" {
			return "", "", common.NewAppError(common.CodeInvalidParams, "未找到已绑定的学号，请传入学号进行首次绑定")
		}
		sid = user.Sid
	}

	// 判断教务系统密码含有大小写字符、数字（和 BindJwc 保持一致的校验）
	hasUpper := regexp.MustCompile(`[A-Z]`).MatchString(spwd)
	hasLower := regexp.MustCompile(`[a-z]`).MatchString(spwd)
	hasDigit := regexp.MustCompile(`\d`).MatchString(spwd)
	if !(hasUpper && hasLower && hasDigit) {
		return "", "", common.NewAppError(common.CodeInvalidParams, "请绑定i中南林APP账号")
	}

	user, err := s.repo.FindByID(ctx, uid)
	if err != nil {
		return "", "", err
	}

	// 学号一旦绑定不可更换
	if user.Sid != "" && user.Sid != sid {
		_ = s.logBindAttempt(ctx, uid, user.Sid, sid, BindStatusFailedLimit, "学号已绑定，不允许更换", ipAddress, userAgent)
		return "", "", common.NewAppError(common.CodeBindLimitExceeded, "学号已绑定，不允许更换。如需更换请联系管理员")
	}

	challengeID, maskedPhone, err := s.sessionService.StartPhoneMFALogin(ctx, uid, sid, spwd)
	if err != nil {
		_ = s.logBindAttempt(ctx, uid, user.Sid, sid, BindStatusFailedAuth, fmt.Sprintf("发起短信验证失败: %v", err), ipAddress, userAgent)
		if appErr, ok := err.(*common.AppError); ok {
			return "", "", appErr
		}
		return "", "", common.NewAppError(common.CodeJwcLoginFailed, "发起短信验证失败")
	}

	s.bindMFAMu.Lock()
	s.cleanExpiredBindMFA()
	s.bindMFA[challengeID] = &pendingBindMFA{
		uid:       uid,
		sid:       sid,
		spwd:      spwd,
		ipAddress: ipAddress,
		userAgent: userAgent,
		expiresAt: time.Now().Add(5 * time.Minute),
	}
	s.bindMFAMu.Unlock()

	return challengeID, maskedPhone, nil
}

// BindJwcCompleteMFA 提交短信验证码，校验通过后完成真正的绑定（写数据库、记日志）
func (s *userService) BindJwcCompleteMFA(ctx context.Context, challengeID, code string) error {
	s.bindMFAMu.Lock()
	pending, ok := s.bindMFA[challengeID]
	if ok && time.Now().After(pending.expiresAt) {
		delete(s.bindMFA, challengeID)
		ok = false
	}
	s.bindMFAMu.Unlock()

	if !ok {
		return common.NewAppError(common.CodeJwcLoginFailed, "验证会话不存在或已过期，请重新发起绑定")
	}

	// 1. 校验短信验证码、完成最终的教务系统登录
	if err := s.sessionService.CompletePhoneMFALogin(ctx, challengeID, code); err != nil {
		// 验证码错误等情况，允许用户在过期时间内重试，不清理 pending
		return err
	}

	uid := pending.uid
	sid := pending.sid
	ipAddress := pending.ipAddress
	userAgent := pending.userAgent

	user, err := s.repo.FindByID(ctx, uid)
	if err != nil {
		return err
	}

	isSameSid := user.Sid != "" && user.Sid == sid

	// 2. 开启事务：更新绑定信息（和 BindJwc 第 5 步逻辑一致）
	tx := s.repo.(*repository).db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	oldSid := user.Sid
	now := time.Now()

	updates := map[string]interface{}{
		"sid":          sid,
		"spwd":         pending.spwd,
		"last_bind_at": now,
	}
	if !isSameSid {
		updates["total_bind_count"] = user.TotalBindCount + 1
	}

	if err := tx.WithContext(ctx).Model(&User{}).Where("uid = ?", uid).Updates(updates).Error; err != nil {
		tx.Rollback()
		_ = s.logBindAttempt(ctx, uid, oldSid, sid, BindStatusFailedOther, fmt.Sprintf("更新数据库失败: %v", err), ipAddress, userAgent)
		return common.NewAppError(common.CodeDatabaseError, "绑定失败，请稍后重试")
	}

	log := &JwcBindLog{
		Uid:        uid,
		OldSid:     oldSid,
		NewSid:     sid,
		BindStatus: BindStatusSuccess,
		IpAddress:  ipAddress,
		UserAgent:  userAgent,
		CreatedAt:  now,
	}
	if err := tx.WithContext(ctx).Create(log).Error; err != nil {
		tx.Rollback()
		return common.NewAppError(common.CodeDatabaseError, "记录日志失败")
	}

	if err := tx.Commit().Error; err != nil {
		return common.NewAppError(common.CodeDatabaseError, "提交事务失败")
	}

	// 3. 清除旧的教务系统会话缓存
	_ = s.sessionService.InvalidateSession(ctx, uid)

	// 4. 清理这次的临时绑定上下文
	s.bindMFAMu.Lock()
	delete(s.bindMFA, challengeID)
	s.bindMFAMu.Unlock()

	return nil
}

// GetBindStatus 获取绑定状态
func (s *userService) GetBindStatus(ctx context.Context, uid int) (*BindStatusResponse, error) {
	user, err := s.repo.FindByID(ctx, uid)
	if err != nil {
		return nil, err
	}

	return &BindStatusResponse{
		IsBound:        user.Sid != "" && user.Spwd != "",
		CurrentSid:     user.Sid,
		TotalBindCount: user.TotalBindCount,
		LastBindAt:     user.LastBindAt,
		CanChangeSid:   user.Sid == "", // 只有未绑定时才能更换学号
	}, nil
}

// CheckIsBind 检查是否绑定教务系统
func (s *userService) CheckIsBind(ctx context.Context, uid int) (bool, error) {
	user, err := s.repo.FindByID(ctx, uid)
	if err != nil {
		return false, err
	}

	return user.Sid != "" && user.Spwd != "", nil
}

// verifyJwcBindingOnLogin 登录时实时校验教务系统绑定状态
// 缓存过期时尝试重新登录，只有真正的密码错误才标记为未绑定
func (s *userService) verifyJwcBindingOnLogin(ctx context.Context, user *User) *User {
	if user.Sid == "" || user.Spwd == "" {
		return user // 未绑定或不完整的绑定，不做校验
	}

	// 尝试从缓存获取 cookies
	cookies, err := s.sessionService.GetCachedCookies(ctx, user.Uid)
	if err == nil && len(cookies) > 0 {
		return user // 缓存有效，绑定正常
	}

	// 缓存过期或不存在，尝试重新登录教务系统
	if err := s.sessionService.LoginAndCache(ctx, user.Uid, user.Sid, user.Spwd); err != nil {
		// 判断是否为真正的认证错误
		if appErr, ok := err.(*common.AppError); ok {
			if appErr.Code == common.CodeJwcLoginFailed {
				// 真正的教务系统登录失败（密码错误等）→ 标记为未绑定
				// 仅影响本次响应，不写数据库
				user.Spwd = ""
				log.Printf("[verifyJwcBindingOnLogin] 教务系统登录失败，标记为未绑定：uid=%d, sid=%s, err=%v", user.Uid, user.Sid, err)
			}
			// 其他错误（服务器错误500、超时、MFA等）→ 保持绑定状态不变
		}
	}
	// 登录成功，绑定正常
	return user
}

// WeChatLogin 微信登录/注册
func (s *userService) WeChatLogin(ctx context.Context, code string) (string, *User, error) {
	// 1. 使用code换取openid和unionid
	wxInfo, err := s.code2Session(ctx, code)
	if err != nil {
		return "", nil, err
	}

	// 2. 查找是否存在该openid的绑定
	user, err := s.repo.FindByWeChatOpenID(ctx, s.appid, wxInfo.OpenID)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return "", nil, err
	}

	// 3. 如果不存在，创建新用户并绑定
	if user == nil {
		user, err = s.createUserFromWeChat(ctx, wxInfo)
		if err != nil {
			return "", nil, err
		}
	} else {
		// 更新最后登录时间和unionid（如果有）
		if err := s.updateWeChatLoginInfo(ctx, user.Uid, wxInfo); err != nil {
			return "", nil, err
		}
	}

	// 记录DAU
	_ = s.dauService.RecordUserActivity(ctx, user.Uid)

	// 生成JWT token
	claims := shared.UserClaims{
		Uid:  user.Uid,
		Name: user.Name,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.jwtExpire)),
			Issuer:    s.jwtIssuer,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", nil, err
	}

	return tokenString, user, nil
}

// WeChatBind 老用户绑定微信
func (s *userService) WeChatBind(ctx context.Context, uid int, code string) error {
	// 1. 使用code换取openid和unionid
	wxInfo, err := s.code2Session(ctx, code)
	if err != nil {
		return err
	}

	// 2. 检查openid是否已被其他用户绑定
	existingUser, err := s.repo.FindByWeChatOpenID(ctx, s.appid, wxInfo.OpenID)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return err
	}
	if existingUser != nil && existingUser.Uid != uid {
		return ErrWeChatAlreadyBind
	}

	// 3. 检查当前用户是否已绑定微信
	existingBind, err := s.repo.FindWeChatBindByUID(ctx, uid, s.appid)
	if err == nil && existingBind != nil {
		// 已存在绑定，更新信息
		existingBind.OpenID = wxInfo.OpenID
		existingBind.UnionID = wxInfo.UnionID
		existingBind.LastLogin = time.Now()
		existingBind.UpdatedAt = time.Now()
		return s.repo.UpdateWeChatBind(ctx, existingBind)
	}

	// 4. 创建新的绑定关系
	bind := &UserWeChatMiniProgram{
		Uid:       uid,
		AppID:     s.appid,
		OpenID:    wxInfo.OpenID,
		UnionID:   wxInfo.UnionID,
		LastLogin: time.Now(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	return s.repo.CreateWeChatBind(ctx, bind)
}

// CheckIsWeChatBind 检查用户是否绑定微信
func (s *userService) CheckIsWeChatBind(ctx context.Context, uid int) (bool, error) {
	isExistUser, err := s.repo.FindWeChatBindByUID(ctx, uid, s.appid)
	if err != nil {
		return false, err
	}
	return isExistUser != nil, nil
}

// WeChatSessionResponse 微信登录响应
type WeChatSessionResponse struct {
	OpenID     string `json:"openid"`
	SessionKey string `json:"session_key"`
	UnionID    string `json:"unionid"`
	ErrCode    int    `json:"errcode"`
	ErrMsg     string `json:"errmsg"`
}

// code2Session 使用code换取openid和unionid
func (s *userService) code2Session(ctx context.Context, code string) (*WeChatSessionResponse, error) {
	url := fmt.Sprintf(
		"https://api.weixin.qq.com/sns/jscode2session?appid=%s&secret=%s&js_code=%s&grant_type=authorization_code",
		s.appid, s.appsecret, code,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, common.NewAppError(common.CodeHttpRequestFailed, "创建请求失败")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, common.NewAppError(common.CodeWeChatLoginFailed, "请求微信接口失败")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, common.NewAppError(common.CodeWeChatLoginFailed, "读取响应失败")
	}

	var wxResp WeChatSessionResponse
	if err := json.Unmarshal(body, &wxResp); err != nil {
		return nil, common.NewAppError(common.CodeInvalidResponse, "解析微信响应失败")
	}

	if wxResp.ErrCode != 0 {
		return nil, common.NewAppError(common.CodeWeChatLoginFailed, fmt.Sprintf("微信接口返回错误: %d - %s", wxResp.ErrCode, wxResp.ErrMsg))
	}

	if wxResp.OpenID == "" {
		return nil, common.NewAppError(common.CodeWeChatLoginFailed, "未获取到OpenID")
	}

	return &wxResp, nil
}

// createUserFromWeChat 从微信信息创建用户
func (s *userService) createUserFromWeChat(ctx context.Context, wxInfo *WeChatSessionResponse) (*User, error) {
	// 生成默认用户名
	defaultName := fmt.Sprintf("微信用户_%s", wxInfo.OpenID[len(wxInfo.OpenID)-8:])
	defaultEmail := fmt.Sprintf("wx_%s@wechat.local", wxInfo.OpenID)

	user := &User{
		Name:      defaultName,
		Email:     defaultEmail,
		Password:  "",
		CreatedAt: time.Now(),
	}

	if err := s.repo.Create(ctx, user); err != nil {
		return nil, common.NewAppError(common.CodeDatabaseError, "创建用户失败")
	}

	bind := &UserWeChatMiniProgram{
		Uid:       user.Uid,
		AppID:     s.appid,
		OpenID:    wxInfo.OpenID,
		UnionID:   wxInfo.UnionID,
		LastLogin: time.Now(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.repo.CreateWeChatBind(ctx, bind); err != nil {
		return nil, common.NewAppError(common.CodeWeChatBindFailed, "创建微信绑定失败")
	}

	return user, nil
}

// updateWeChatLoginInfo 更新微信登录信息
func (s *userService) updateWeChatLoginInfo(ctx context.Context, uid int, wxInfo *WeChatSessionResponse) error {
	bind, err := s.repo.FindWeChatBindByUID(ctx, uid, s.appid)
	if err != nil {
		return err
	}

	bind.UnionID = wxInfo.UnionID
	bind.LastLogin = time.Now()
	bind.UpdatedAt = time.Now()

	return s.repo.UpdateWeChatBind(ctx, bind)
}

// UpdateName 更新用户名
func (s *userService) UpdateName(ctx context.Context, uid int, name string) error {
	if name == "" {
		return ErrEmptyParams
	}

	// 获取用户
	user, err := s.repo.FindByID(ctx, uid)
	if err != nil {
		return err
	}

	// 更新用户名
	user.Name = name
	return s.repo.Update(ctx, user)
}

// UpdateEmail 更新邮箱（需要验证码）
func (s *userService) UpdateEmail(ctx context.Context, uid int, email, captcha string) error {
	if email == "" {
		return ErrEmptyParams
	}

	// 验证验证码
	if err := s.captchaService.VerifyEmailCaptcha(ctx, email, captcha); err != nil {
		return ErrInvalidCaptcha
	}

	// 检查新邮箱是否已被使用
	existingUser, err := s.repo.FindByEmail(ctx, email)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return err
	}
	if existingUser != nil && existingUser.Uid != uid {
		return ErrEmailAlreadyExists
	}

	// 获取当前用户
	user, err := s.repo.FindByID(ctx, uid)
	if err != nil {
		return err
	}

	// 更新邮箱
	user.Email = email
	return s.repo.Update(ctx, user)
}

// MagicLinkExpireMinutes Magic Link 过期时间（分钟）
const MagicLinkExpireMinutes = 10

// SendMagicLink 发送 Magic Link 登录链接到邮箱
func (s *userService) SendMagicLink(ctx context.Context, email string) error {
	// 1. 查找用户是否存在
	_, err := s.repo.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return common.NewAppError(common.CodeUserNotFound, "该邮箱未注册")
		}
		return err
	}

	// 2. 生成一次性 token
	token, err := s.magicLinkCache.CreateToken(ctx, email, MagicLinkExpireMinutes*time.Minute)
	if err != nil {
		return common.NewAppError(common.CodeInternalError, "生成登录链接失败")
	}

	// 3. 构造登录链接
	link := fmt.Sprintf("%s/auth/callback?token=%s", s.frontendBaseURL, token)

	// 4. 发送邮件
	if err := s.emailService.SendMagicLink(ctx, email, link, MagicLinkExpireMinutes); err != nil {
		// 发送失败，清理 token
		_ = s.magicLinkCache.DeleteByEmail(ctx, email)
		return common.NewAppError(common.CodeInternalError, "发送邮件失败")
	}

	log.Printf("[SendMagicLink] 登录链接已发送: email=%s", email)
	return nil
}

// LoginByMagicLink 通过 Magic Link token 登录
func (s *userService) LoginByMagicLink(ctx context.Context, token string) (string, *User, error) {
	// 1. 验证 token
	email, err := s.magicLinkCache.VerifyAndConsume(ctx, token)
	if err != nil {
		return "", nil, common.NewAppError(common.CodeInvalidParams, "链接无效或已过期")
	}

	// 2. 查找用户
	user, err := s.repo.FindByEmail(ctx, email)
	if err != nil {
		return "", nil, ErrUserNotFound
	}

	// 3. 记录DAU
	_ = s.dauService.RecordUserActivity(ctx, user.Uid)

	// 4. 生成JWT token
	claims := shared.UserClaims{
		Uid:  user.Uid,
		Name: user.Name,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.jwtExpire)),
			Issuer:    s.jwtIssuer,
		},
	}

	tokenString := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := tokenString.SignedString(s.jwtSecret)
	if err != nil {
		return "", nil, err
	}

	// 5. 实时校验教务系统绑定状态
	user = s.verifyJwcBindingOnLogin(ctx, user)

	return tokenStr, user, nil
}

// unbindCooldown 注销冷却期
const unbindCooldown = 24 * time.Hour

// UnbindJwc 注销教务系统绑定：清除 sid+spwd+所有同步数据，1天冷却期
func (s *userService) UnbindJwc(ctx context.Context, uid int, ipAddress, userAgent string) error {
	// 1. 检查用户是否已绑定
	user, err := s.repo.FindByID(ctx, uid)
	if err != nil {
		return err
	}
	if user.Sid == "" {
		return common.NewAppError(common.CodeJwcNotBound, "当前未绑定教务系统")
	}

	// 2. 检查冷却期
	lastUnbind, err := s.repo.GetLastUnbindTime(ctx, uid)
	if err != nil {
		return common.NewAppError(common.CodeInternalError, "查询注销记录失败")
	}
	if lastUnbind != nil && time.Since(*lastUnbind) < unbindCooldown {
		remaining := unbindCooldown - time.Since(*lastUnbind)
		hours := int(remaining.Hours())
		minutes := int(remaining.Minutes()) % 60
		msg := fmt.Sprintf("注销冷却期未到，请 %d小时%d分钟 后再试", hours, minutes)
		return common.NewAppError(common.CodeUnbindCooldown, msg)
	}

	// 3. 开启事务：清除绑定信息 + 软删除数据 + 记录日志
	tx := s.repo.(*repository).db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	oldSid := user.Sid
	now := time.Now()

	// 3.1 清除 sid + spwd + 绑定计数
	if err := tx.WithContext(ctx).Model(&User{}).Where("uid = ?", uid).Updates(map[string]interface{}{
		"sid":                      "",
		"spwd":                     "",
		"bind_count_current_month": 0,
		"bind_month":               "",
		"total_bind_count":         0,
	}).Error; err != nil {
		tx.Rollback()
		return common.NewAppError(common.CodeDatabaseError, "清除绑定信息失败")
	}

	// 3.2 软删除所有同步数据
	tables := []string{"grades", "regular_grades", "exams", "level_exams", "courses"}
	for _, table := range tables {
		if err := tx.WithContext(ctx).Table(table).Where("uid = ?", uid).Update("is_deleted", true).Error; err != nil {
			tx.Rollback()
			return common.NewAppError(common.CodeDatabaseError, "清除同步数据失败")
		}
	}

	// 3.3 重置同步状态
	if err := tx.WithContext(ctx).Table("user_sync_status").Where("uid = ?", uid).Updates(map[string]interface{}{
		"grade_last_sync_at": nil, "grade_sync_version": 0,
		"regular_grade_last_sync_at": nil, "regular_grade_sync_version": 0,
		"exam_last_sync_at": nil, "exam_sync_version": 0,
		"level_exam_last_sync_at": nil, "level_exam_sync_version": 0,
		"course_last_sync_at": nil, "course_sync_version": 0,
	}).Error; err != nil {
		tx.Rollback()
		return common.NewAppError(common.CodeDatabaseError, "重置同步状态失败")
	}

	// 3.4 记录注销日志
	unbindLog := &JwcBindLog{
		Uid:        uid,
		OldSid:     oldSid,
		NewSid:     "",
		BindStatus: BindStatusUnbind,
		IpAddress:  ipAddress,
		UserAgent:  userAgent,
		CreatedAt:  now,
	}
	if err := tx.WithContext(ctx).Create(unbindLog).Error; err != nil {
		tx.Rollback()
		return common.NewAppError(common.CodeDatabaseError, "记录注销日志失败")
	}

	if err := tx.Commit().Error; err != nil {
		return common.NewAppError(common.CodeDatabaseError, "提交事务失败")
	}

	// 4. 清除缓存的教务系统会话
	_ = s.sessionService.InvalidateSession(ctx, uid)

	log.Printf("[UnbindJwc] 用户 %d 已注销学号 %s", uid, oldSid)
	return nil
}
