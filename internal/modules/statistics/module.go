package statistics

import (
	"spider-go/internal/modules/user"
	"spider-go/internal/service"

	"github.com/gin-gonic/gin"
)

// Module 统计模块
type Module struct {
	service Service
	handler *Handler
}

// NewModule 创建统计模块
func NewModule(dauService service.DAUService, userRepo user.Repository) *Module {
	svc := NewService(dauService, userRepo)
	handler := NewHandler(svc)

	return &Module{
		service: svc,
		handler: handler,
	}
}

// RegisterRoutes 注册路由
// adminGroup: /api/admin/statistics - 管理员统计接口
func (m *Module) RegisterRoutes(adminGroup *gin.RouterGroup) {
	// 管理员统计接口
	adminGroup.GET("/dau", m.handler.GetTodayDAU)
	adminGroup.GET("/dau/range", m.handler.GetDAURange)
	adminGroup.GET("/user/count", m.handler.GetUserCount)
	adminGroup.GET("/user/new", m.handler.GetNewUserCount)
}

// GetService 获取服务（供其他模块使用）
func (m *Module) GetService() Service {
	return m.service
}
