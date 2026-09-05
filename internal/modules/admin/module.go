package admin

import (
	"context"
	"spider-go/internal/cache"
	"spider-go/internal/common"
	"spider-go/internal/service"
	"spider-go/internal/shared"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Module 管理员模块
type Module struct {
	handler *Handler
	service Service
}

// NewModule 创建管理员模块
func NewModule(
	db *gorm.DB,
	userQuery shared.UserQuery,
	emailService service.EmailService,
	captchaCache cache.CaptchaCache,
	jwtSecret string,
	jwtIssuer string,
) *Module {
	repo := NewRepository(db)

	// 验证码验证函数：直接复用验证码缓存的验证方法
	captchaVerify := func(ctx context.Context, email, code string) error {
		valid, err := captchaCache.VerifyAndDelete(ctx, email, code)
		if err != nil {
			return err
		}
		if !valid {
			return common.NewAppError(common.CodeInvalidParams, "验证码错误")
		}
		return nil
	}

	svc := NewService(repo, userQuery, emailService, captchaVerify, jwtSecret, jwtIssuer)
	captchaSvc := NewAdminCaptchaService(repo, emailService, captchaCache)
	handler := NewHandler(svc, captchaSvc)

	return &Module{
		handler: handler,
		service: svc,
	}
}

// RegisterRoutes 注册路由
// public: 公开路由组（无需认证）
// authenticated: 需要认证的路由组（管理员认证）
func (m *Module) RegisterRoutes(public *gin.RouterGroup, authenticated *gin.RouterGroup) {
	m.handler.RegisterRoutes(public, authenticated)
}

// GetService 获取服务实例（用于其他模块或初始化）
func (m *Module) GetService() Service {
	return m.service
}
