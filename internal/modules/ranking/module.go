package ranking

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Module 排名模块
type Module struct {
	handler *Handler
	service Service
}

// NewModule 创建排名模块
func NewModule(db *gorm.DB) *Module {
	repo := NewRepository(db)
	service := NewService(repo)
	handler := NewHandler(service)

	return &Module{
		handler: handler,
		service: service,
	}
}

// RegisterRoutes 注册路由
func (m *Module) RegisterRoutes(r *gin.RouterGroup) {
	m.handler.RegisterRoutes(r)
}

// GetService 获取服务（用于其他模块注入）
func (m *Module) GetService() Service {
	return m.service
}
