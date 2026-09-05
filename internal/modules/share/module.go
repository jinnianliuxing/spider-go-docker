package share

import (
	"spider-go/internal/modules/course"
	"spider-go/internal/shared"

	"github.com/gin-gonic/gin"
)

// Module 分享模块
type Module struct {
	handler *Handler
}

// NewModule 创建分享模块
func NewModule(userQuery shared.UserQuery, courseService course.Service) *Module {
	svc := NewService(userQuery, courseService)
	handler := NewHandler(svc)
	return &Module{handler: handler}
}

// RegisterRoutes 注册路由
func (m *Module) RegisterRoutes(publicGroup *gin.RouterGroup, authGroup *gin.RouterGroup) {
	m.handler.RegisterPublicRoutes(publicGroup)
	m.handler.RegisterAuthRoutes(authGroup)
}
