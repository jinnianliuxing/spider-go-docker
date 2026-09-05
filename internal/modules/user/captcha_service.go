package user

import (
	"context"
	"fmt"
	"math/rand"
	"spider-go/internal/cache"
	"spider-go/internal/common"
	"spider-go/internal/service"
	"time"
)

// CaptchaService 验证码服务接口
type CaptchaService interface {
	// SendEmailCaptcha 发送邮箱验证码
	SendEmailCaptcha(ctx context.Context, email string) error

	// VerifyEmailCaptcha 验证邮箱验证码
	VerifyEmailCaptcha(ctx context.Context, email string, code string) error
}

// captchaService 验证码服务实现
type captchaService struct {
	captchaCache cache.CaptchaCache
	emailService service.EmailService
}

// NewCaptchaService 创建验证码服务
func NewCaptchaService(captchaCache cache.CaptchaCache, emailService service.EmailService) CaptchaService {
	return &captchaService{
		captchaCache: captchaCache,
		emailService: emailService,
	}
}

// SendEmailCaptcha 发送邮箱验证码
func (s *captchaService) SendEmailCaptcha(ctx context.Context, email string) error {
	// 1. 检查冷却期（60 秒内不可重复发送）
	cooldown, err := s.captchaCache.HasCooldown(ctx, email)
	if err != nil {
		return common.NewAppError(common.CodeCacheError, "检查发送频率失败")
	}
	if cooldown {
		return common.NewAppError(common.CodeInvalidParams, "发送过于频繁，请 60 秒后再试")
	}

	// 2. 设置冷却标记（60 秒），先于发送以防止并发绕过
	if err := s.captchaCache.SetCooldown(ctx, email, 60*time.Second); err != nil {
		return common.NewAppError(common.CodeCacheError, "设置发送冷却失败")
	}

	// 3. 生成 6 位数字验证码
	code := s.generateCode(6)

	// 4. 存储到 Redis，5 分钟过期
	if err := s.captchaCache.SetCaptcha(ctx, email, code, 5*time.Minute); err != nil {
		return common.NewAppError(common.CodeCacheError, "存储验证码失败")
	}

	// 5. 发送邮件（验证码有效期 5 分钟）
	if err := s.emailService.SendVerificationCode(ctx, email, code, 5); err != nil {
		// 发送失败，清理已存储的验证码和冷却标记
		_ = s.captchaCache.DeleteCaptcha(ctx, email)
		_ = s.captchaCache.SetCooldown(ctx, email, 0) // TTL=0 立即删除冷却标记
		return err
	}

	return nil
}

// VerifyEmailCaptcha 验证邮箱验证码
func (s *captchaService) VerifyEmailCaptcha(ctx context.Context, email string, code string) error {
	// 使用原子操作验证并删除验证码
	valid, err := s.captchaCache.VerifyAndDelete(ctx, email, code)
	if err != nil {
		return common.NewAppError(common.CodeInvalidParams, err.Error())
	}

	if !valid {
		return common.NewAppError(common.CodeInvalidParams, "验证码错误")
	}

	return nil
}

// generateCode 生成指定位数的数字验证码
func (s *captchaService) generateCode(length int) string {
	rand.Seed(time.Now().UnixNano())
	code := ""
	for i := 0; i < length; i++ {
		code += fmt.Sprintf("%d", rand.Intn(10))
	}
	return code
}
