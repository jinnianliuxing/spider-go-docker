package evaluation

import (
	"spider-go/internal/cache"
	"spider-go/internal/service"
	"spider-go/internal/shared"

	"github.com/gin-gonic/gin"
)

// Module 教评模块
type Module struct {
	service Service
	handler *Handler
}

// NewModule 创建教评模块
func NewModule(
	userQuery shared.UserQuery,
	sessionService service.SessionService,
	evaluationCache cache.EvaluationCache,
	sessionCache cache.SessionCache,
	evaluationInfoURL string,
	casRedirectURL string,
	doLoginURL string,
) *Module {
	// 初始化服务
	svc := NewService(
		userQuery,
		sessionService,
		evaluationCache,
		sessionCache,
		evaluationInfoURL,
		casRedirectURL,
		doLoginURL,
	)

	// 初始化处理器
	handler := NewHandler(svc)

	return &Module{
		service: svc,
		handler: handler,
	}
}

// RegisterRoutes 注册路由
func (m *Module) RegisterRoutes(r *gin.RouterGroup) {
	m.handler.RegisterRoutes(r)
}

// GetService 获取服务
func (m *Module) GetService() Service {
	return m.service
}
