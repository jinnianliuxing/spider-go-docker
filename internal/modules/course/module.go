package course

import (
	"spider-go/internal/cache"
	"spider-go/internal/service"
	"spider-go/internal/shared"

	"github.com/gin-gonic/gin"
)

// Module 课程模块
type Module struct {
	handler *Handler
	service Service
}

// NewModule 创建课程模块
func NewModule(
	userQuery shared.UserQuery,
	sessionService service.SessionService,
	crawlerService service.CrawlerService,
	userDataCache cache.UserDataCache,
	courseURL string,
) *Module {
	svc := NewService(userQuery, sessionService, crawlerService, userDataCache, courseURL)
	handler := NewHandler(svc)

	return &Module{
		handler: handler,
		service: svc,
	}
}

// RegisterRoutes 注册路由
func (m *Module) RegisterRoutes(r *gin.RouterGroup) {
	m.handler.RegisterRoutes(r)
}

// GetService 获取服务实例（用于跨模块调用）
func (m *Module) GetService() Service {
	return m.service
}
