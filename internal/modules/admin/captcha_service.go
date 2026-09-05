package admin

import (
	"context"
	"fmt"
	"math/rand"
	"spider-go/internal/cache"
	"spider-go/internal/common"
	"spider-go/internal/service"
	"time"
)

// adminCaptchaService 管理员验证码发送服务实现
// 在发送验证码前先验证邮箱是否为已注册管理员，防止滥用
type adminCaptchaService struct {
	repo         Repository
	emailService service.EmailService
	captchaCache cache.CaptchaCache
}

// NewAdminCaptchaService 创建管理员验证码发送服务
func NewAdminCaptchaService(
	repo Repository,
	emailService service.EmailService,
	captchaCache cache.CaptchaCache,
) CaptchaService {
	return &adminCaptchaService{
		repo:         repo,
		emailService: emailService,
		captchaCache: captchaCache,
	}
}

// SendAdminCaptcha 验证邮箱为已注册管理员后发送验证码
func (s *adminCaptchaService) SendAdminCaptcha(ctx context.Context, email string) error {
	// 1. 检查邮箱是否为已注册管理员
	if _, err := s.repo.FindByEmail(ctx, email); err != nil {
		return common.NewAppError(common.CodeAdminNotFound, "该邮箱不是管理员账号")
	}

	// 2. 检查冷却期（60 秒内不可重复发送）
	cooldown, err := s.captchaCache.HasCooldown(ctx, email)
	if err != nil {
		return common.NewAppError(common.CodeCacheError, "检查发送频率失败")
	}
	if cooldown {
		return common.NewAppError(common.CodeInvalidParams, "发送过于频繁，请 60 秒后再试")
	}

	// 3. 设置冷却标记（60 秒）
	if err := s.captchaCache.SetCooldown(ctx, email, 60*time.Second); err != nil {
		return common.NewAppError(common.CodeCacheError, "设置发送冷却失败")
	}

	// 4. 生成 6 位数字验证码
	code := s.generateCode(6)

	// 5. 存储到 Redis，5 分钟过期
	if err := s.captchaCache.SetCaptcha(ctx, email, code, 5*time.Minute); err != nil {
		return common.NewAppError(common.CodeCacheError, "存储验证码失败")
	}

	// 6. 发送邮件
	if err := s.emailService.SendVerificationCode(ctx, email, code, 5); err != nil {
		// 发送失败，清理已存储的验证码和冷却标记
		_ = s.captchaCache.DeleteCaptcha(ctx, email)
		_ = s.captchaCache.SetCooldown(ctx, email, 0)
		return err
	}

	return nil
}

// generateCode 生成指定位数的数字验证码
func (s *adminCaptchaService) generateCode(length int) string {
	rand.Seed(time.Now().UnixNano())
	code := ""
	for i := 0; i < length; i++ {
		code += fmt.Sprintf("%d", rand.Intn(10))
	}
	return code
}
