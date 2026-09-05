package reconciliation

import (
	"spider-go/internal/cache"
	"spider-go/internal/modules/course"
	"spider-go/internal/modules/exam"
	"spider-go/internal/modules/grade"
	"spider-go/internal/modules/ranking"
	"spider-go/internal/modules/user"
	"spider-go/internal/shared"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Module 对账模块
type Module struct {
	handler *Handler
	service *service // 改为具体类型以便调用 SetUserQuery
	repo    Repository
}

// NewModule 创建对账模块
func NewModule(db *gorm.DB, gradeService grade.Service, examService exam.Service, courseService course.Service, configCache cache.ConfigCache, rankingService ranking.Service) *Module {
	repo := NewRepository(db)
	svc := NewService(repo, gradeService, examService, courseService, configCache, rankingService)
	handler := NewHandler(svc, repo)

	return &Module{
		handler: handler,
		service: svc.(*service), // 类型断言
		repo:    repo,
	}
}

// RegisterRoutes 注册路由
func (m *Module) RegisterRoutes(r *gin.RouterGroup) {
	m.handler.RegisterRoutes(r)
}

// RegisterAdminRoutes 注册管理员路由
func (m *Module) RegisterAdminRoutes(r *gin.RouterGroup, userRepo user.Repository) {
	m.handler.SetUserRepo(userRepo)
	m.handler.RegisterAdminRoutes(r)
}

// GetService 获取服务（用于其他模块注入或调度器）
func (m *Module) GetService() Service {
	return m.service
}

// SetUserQuery 设置用户查询接口（用于清除绑定）
func (m *Module) SetUserQuery(userQuery shared.UserQuery) {
	m.service.SetUserQuery(userQuery)
}

// GetRepository 获取 Repository（用于调度器等外部模块）
func (m *Module) GetRepository() Repository {
	return m.repo
}
