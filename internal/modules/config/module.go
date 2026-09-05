package config

import (
	"spider-go/internal/cache"

	"github.com/gin-gonic/gin"
)

// Module 配置模块
type Module struct {
	service Service
	handler *Handler
}

// NewModule 创建配置模块
func NewModule(configCache cache.ConfigCache) *Module {
	service := NewService(configCache)
	handler := NewHandler(service)

	return &Module{
		service: service,
		handler: handler,
	}
}

// RegisterRoutes 注册路由
// publicGroup: /api/config - 公开接口（查询）
// adminGroup: /api/admin/config - 管理员接口（设置）
func (m *Module) RegisterRoutes(publicGroup, adminGroup *gin.RouterGroup) {
	// 公开接口 - 查询配置
	if publicGroup != nil {
		publicGroup.GET("/term", m.handler.GetCurrentTerm)
		publicGroup.GET("/semester-dates", m.handler.GetSemesterDates)
	}

	// 管理员接口 - 设置配置
	if adminGroup != nil {
		adminGroup.POST("/term", m.handler.SetCurrentTerm)
		adminGroup.POST("/semester-dates", m.handler.SetSemesterDates)
	}
}

// GetService 获取服务（供其他模块使用）
func (m *Module) GetService() Service {
	return m.service
}
