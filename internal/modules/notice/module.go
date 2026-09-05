package notice

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Module 通知模块
type Module struct {
	handler *Handler
	service Service
}

// NewModule 创建通知模块
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
// r: 普通用户路由组
// adminGroup: 管理员路由组
func (m *Module) RegisterRoutes(r *gin.RouterGroup, adminGroup *gin.RouterGroup) {
	m.handler.RegisterRoutes(r, adminGroup)
}

// GetService 获取服务实例（用于跨模块调用）
func (m *Module) GetService() Service {
	return m.service
}
