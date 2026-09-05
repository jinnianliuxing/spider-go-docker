package coursetips

import (
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Module 选课提示模块
type Module struct {
	handler *Handler
}

// NewModule 创建选课提示模块，注入 DB 和 Redis 依赖
func NewModule(db *gorm.DB, redisClient *redis.Client) *Module {
	repo := NewRepository(db)
	service := NewService(repo, redisClient)
	handler := NewHandler(service)
	return &Module{handler: handler}
}

// RegisterRoutes 注册路由到认证路由组
func (m *Module) RegisterRoutes(authGroup *gin.RouterGroup) {
	m.handler.RegisterRoutes(authGroup)
}
