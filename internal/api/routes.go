package api

import (
	"spider-go/internal/app"
	"spider-go/internal/middleware"

	"github.com/gin-gonic/gin"
)

// SetupRoutes 设置路由
func SetupRoutes(r *gin.Engine, container *app.Container) {
	// JWT secret
	secret := []byte(container.Config.JWT.Secret)

	// API 根路由组
	api := r.Group("/api")
	{
		// ========== 公开接口 ==========
		// 配置查询（公开）
		configPublic := api.Group("/config")

		// ========== 用户路由 ==========
		// 需要认证的用户接口
		userAuth := api.Group("/user")
		userAuth.Use(middleware.AuthMiddleWare(secret, container.DAUService))

		// 注册用户模块路由（包含公开和认证路由）
		container.UserModule.RegisterRoutes(api, userAuth)

		// ========== 管理员路由 ==========
		// 需要管理员认证的接口
		adminAuth := api.Group("/admin")
		adminAuth.Use(middleware.AdminAuthMiddleware(secret))

		// 注册管理员模块路由（包含公开和认证路由）
		container.AdminModule.RegisterRoutes(api, adminAuth)

		// 管理员配置管理
		adminConfig := adminAuth.Group("/config")

		// 注册配置模块路由（公开+管理员）
		container.ConfigModule.RegisterRoutes(configPublic, adminConfig)

		// 管理员统计查询
		adminStats := adminAuth.Group("/statistics")
		container.StatisticsModule.RegisterRoutes(adminStats)

		// 管理员同步模块
		container.ReconciliationModule.RegisterAdminRoutes(adminAuth, container.UserModule.GetRepository())

		// ========== 业务模块路由 ==========
		// 成绩模块
		container.GradeModule.RegisterRoutes(userAuth)

		// 课程模块
		container.CourseModule.RegisterRoutes(userAuth)

		// 考试模块
		container.ExamModule.RegisterRoutes(userAuth)

		// 通知模块（包含公开和管理员路由）
		container.NoticeModule.RegisterRoutes(api, adminAuth)

		// 教评模块
		container.EvaluationModule.RegisterRoutes(userAuth)

		// 排名模块
		container.RankingModule.RegisterRoutes(userAuth)

		// 对账/同步模块
		container.ReconciliationModule.RegisterRoutes(userAuth)

		// 分享模块（公开 + 认证）
		container.ShareModule.RegisterRoutes(api, userAuth)

		// 选课提示模块
		container.CourseTipsModule.RegisterRoutes(userAuth)
	}
}
