package admin

import (
	"context"
	"spider-go/internal/common"
	"spider-go/internal/service"
	"spider-go/internal/shared"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = common.NewAppError(common.CodeInvalidPassword, "管理员不存在或密码错误")
	ErrInvalidPassword    = common.NewAppError(common.CodeInvalidPassword, "原密码错误")
	ErrEmailAlreadyExists = common.NewAppError(common.CodeUserAlreadyExists, "该邮箱已被使用")
)

// CaptchaService 管理员验证码发送服务接口
type CaptchaService interface {
	// SendAdminCaptcha 验证邮箱为已注册管理员后发送验证码
	SendAdminCaptcha(ctx context.Context, email string) error
}

// Service 管理员服务接口
type Service interface {
	// 认证相关
	Login(ctx context.Context, email, password string) (token string, admin *Admin, err error)
	LoginByCaptcha(ctx context.Context, email, captcha string) (token string, admin *Admin, err error)
	GetAdminInfo(ctx context.Context, uid int) (*Admin, error)
	ChangePassword(ctx context.Context, uid int, oldPassword, newPassword string) error
	UpdateEmail(ctx context.Context, uid int, newEmail, captcha string) error
	ResetPasswordByCaptcha(ctx context.Context, email, captcha string) error

	// 系统管理
	InitDefaultAdmin(ctx context.Context) error
	BroadcastEmail(ctx context.Context, subject, content string) (successCount, failCount, totalCount int, err error)
	DeleteUser(ctx context.Context, uid int) error
	GetAllUsers(ctx context.Context) ([]*UserWithBindStatus, error)
}

// adminService 管理员服务实现
type adminService struct {
	repo          Repository
	userQuery     shared.UserQuery
	emailService  service.EmailService
	captchaVerify func(ctx context.Context, email, code string) error
	jwtSecret     []byte
	jwtIssuer     string
	jwtExpire     time.Duration
}

// NewService 创建管理员服务
func NewService(
	repo Repository,
	userQuery shared.UserQuery,
	emailService service.EmailService,
	captchaVerify func(ctx context.Context, email, code string) error,
	jwtSecret string,
	jwtIssuer string,
) Service {
	return &adminService{
		repo:          repo,
		userQuery:     userQuery,
		emailService:  emailService,
		captchaVerify: captchaVerify,
		jwtSecret:     []byte(jwtSecret),
		jwtIssuer:     jwtIssuer,
		jwtExpire:     24 * time.Hour, // 管理员token 24小时
	}
}

// LoginByCaptcha 管理员验证码登录
func (s *adminService) LoginByCaptcha(ctx context.Context, email, captcha string) (string, *Admin, error) {
	// 查找管理员
	admin, err := s.repo.FindByEmail(ctx, email)
	if err != nil {
		return "", nil, ErrInvalidCredentials
	}

	// 验证验证码
	if err := s.captchaVerify(ctx, email, captcha); err != nil {
		return "", nil, common.NewAppError(common.CodeInvalidParams, "验证码错误")
	}

	// 生成JWT token
	claims := shared.AdminClaims{
		Uid:     admin.Uid,
		Name:    admin.Name,
		IsAdmin: true,
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

	return tokenString, admin, nil
}

// Login 管理员登录
func (s *adminService) Login(ctx context.Context, email, password string) (string, *Admin, error) {
	// 查找管理员
	admin, err := s.repo.FindByEmail(ctx, email)
	if err != nil {
		return "", nil, ErrInvalidCredentials
	}

	// 验证密码
	if err := bcrypt.CompareHashAndPassword([]byte(admin.Password), []byte(password)); err != nil {
		return "", nil, ErrInvalidCredentials
	}

	// 生成JWT token
	claims := shared.AdminClaims{
		Uid:     admin.Uid,
		Name:    admin.Name,
		IsAdmin: true,
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

	return tokenString, admin, nil
}

// GetAdminInfo 获取管理员信息
func (s *adminService) GetAdminInfo(ctx context.Context, uid int) (*Admin, error) {
	admin, err := s.repo.FindByID(ctx, uid)
	if err != nil {
		return nil, err
	}

	return admin, nil
}

// UpdateEmail 更新管理员登录邮箱（需验证新邮箱的验证码）
func (s *adminService) UpdateEmail(ctx context.Context, uid int, newEmail, captcha string) error {
	// 1. 验证验证码（发给新邮箱的）
	if err := s.captchaVerify(ctx, newEmail, captcha); err != nil {
		return common.NewAppError(common.CodeInvalidParams, "验证码错误")
	}

	// 2. 检查新邮箱是否已被其他管理员使用
	existing, err := s.repo.FindByEmail(ctx, newEmail)
	if err == nil && existing != nil && existing.Uid != uid {
		return ErrEmailAlreadyExists
	}

	// 3. 更新邮箱
	return s.repo.UpdateEmail(ctx, uid, newEmail)
}

// ChangePassword 修改密码
func (s *adminService) ChangePassword(ctx context.Context, uid int, oldPassword, newPassword string) error {
	// 查找管理员
	admin, err := s.repo.FindByID(ctx, uid)
	if err != nil {
		return ErrAdminNotFound
	}

	// 验证原密码
	if err := bcrypt.CompareHashAndPassword([]byte(admin.Password), []byte(oldPassword)); err != nil {
		return ErrInvalidPassword
	}

	// 加密新密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	// 更新密码
	return s.repo.UpdatePassword(ctx, uid, string(hashedPassword))
}

// ResetPasswordByCaptcha 通过验证码验证后重置密码为123456
func (s *adminService) ResetPasswordByCaptcha(ctx context.Context, email, captcha string) error {
	// 查找管理员
	admin, err := s.repo.FindByEmail(ctx, email)
	if err != nil {
		return ErrInvalidCredentials
	}

	// 验证验证码（发给当前绑定邮箱的）
	if err := s.captchaVerify(ctx, admin.Email, captcha); err != nil {
		return common.NewAppError(common.CodeInvalidParams, "验证码错误")
	}

	// 重置密码为123456
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte("123456"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	return s.repo.UpdatePassword(ctx, admin.Uid, string(hashedPassword))
}

// InitDefaultAdmin 初始化默认管理员
func (s *adminService) InitDefaultAdmin(ctx context.Context) error {
	// 检查是否已存在管理员
	exists, err := s.repo.CheckExists(ctx)
	if err != nil {
		return err
	}

	if exists {
		return nil // 已存在管理员，不需要初始化
	}

	// 创建默认管理员
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte("123456"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	admin := &Admin{
		Email:     "admin@spider-go.com",
		Name:      "Haruka",
		Password:  string(hashedPassword),
		CreatedAt: time.Now(),
	}

	return s.repo.Create(ctx, admin)
}

// DeleteUser 删除用户
func (s *adminService) DeleteUser(ctx context.Context, uid int) error {
	return s.repo.DeleteUser(ctx, uid)
}

// GetAllUsers 获取所有用户
func (s *adminService) GetAllUsers(ctx context.Context) ([]*UserWithBindStatus, error) {
	users, err := s.userQuery.GetAllUsers(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]*UserWithBindStatus, 0, len(users))
	for _, u := range users {
		status := &UserWithBindStatus{
			Uid:    u.Uid,
			Email:  u.Email,
			Name:   u.Name,
			Sid:    u.Sid,
			IsBind: u.Sid != "" && u.Spwd != "",
		}
		result = append(result, status)
	}
	return result, nil
}

// BroadcastEmail 群发邮件给所有用户
func (s *adminService) BroadcastEmail(ctx context.Context, subject, content string) (int, int, int, error) {
	// 获取所有用户的邮箱
	emails, err := s.userQuery.GetAllUserEmails(ctx)
	if err != nil {
		return 0, 0, 0, common.NewAppError(common.CodeDatabaseError, "获取用户邮箱列表失败")
	}

	if len(emails) == 0 {
		return 0, 0, 0, common.NewAppError(common.CodeNotFound, "没有用户可以发送邮件")
	}

	// 群发邮件
	successCount := 0
	failCount := 0

	for _, email := range emails {
		err := s.emailService.SendEmail(ctx, email, subject, content)
		if err != nil {
			failCount++
		} else {
			successCount++
		}
	}

	return successCount, failCount, len(emails), nil
}
