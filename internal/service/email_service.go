package service

import (
	"context"
	pkgemail "spider-go/pkg/email"
	pkgerrors "spider-go/pkg/errors"
)

// EmailService 邮件服务接口（适配层）
type EmailService interface {
	// SendVerificationCode 发送验证码邮件
	// expireMinutes: 验证码有效期（分钟），会显示在邮件正文中
	SendVerificationCode(ctx context.Context, to string, code string, expireMinutes int) error

	// SendEmail 发送普通邮件
	SendEmail(ctx context.Context, to string, subject string, body string) error

	// SendMagicLink 发送 Magic Link 登录链接
	SendMagicLink(ctx context.Context, to string, link string, expireMinutes int) error
}

// emailServiceAdapter 邮件服务适配器
type emailServiceAdapter struct {
	emailService pkgemail.EmailService
}

// NewEmailService 创建邮件服务（适配 pkg/email）
func NewEmailService(smtpHost string, smtpPort int, username, password, fromName string) EmailService {
	return &emailServiceAdapter{
		emailService: pkgemail.NewEmailService(smtpHost, smtpPort, username, password, fromName),
	}
}

// SendVerificationCode 发送验证码邮件
func (a *emailServiceAdapter) SendVerificationCode(ctx context.Context, to string, code string, expireMinutes int) error {
	if err := a.emailService.SendVerificationCode(ctx, to, code, expireMinutes); err != nil {
		return pkgerrors.NewAppError(pkgerrors.CodeInternalError, err.Error())
	}
	return nil
}

// SendEmail 发送普通邮件
func (a *emailServiceAdapter) SendEmail(ctx context.Context, to string, subject string, body string) error {
	if err := a.emailService.SendEmail(ctx, to, subject, body); err != nil {
		return pkgerrors.NewAppError(pkgerrors.CodeInternalError, err.Error())
	}
	return nil
}

// SendMagicLink 发送 Magic Link 登录链接
func (a *emailServiceAdapter) SendMagicLink(ctx context.Context, to string, link string, expireMinutes int) error {
	if err := a.emailService.SendMagicLink(ctx, to, link, expireMinutes); err != nil {
		return pkgerrors.NewAppError(pkgerrors.CodeInternalError, err.Error())
	}
	return nil
}
