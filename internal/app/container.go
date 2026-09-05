package app

import (
	"context"
	"fmt"
	"log"
	"spider-go/internal/cache"
	"spider-go/internal/middleware"
	"spider-go/internal/modules/admin"
	"spider-go/internal/modules/config"
	"spider-go/internal/modules/course"
	"spider-go/internal/modules/coursetips"
	"spider-go/internal/modules/evaluation"
	"spider-go/internal/modules/exam"
	"spider-go/internal/modules/grade"
	"spider-go/internal/modules/notice"
	"spider-go/internal/modules/ranking"
	"spider-go/internal/modules/reconciliation"
	"spider-go/internal/modules/share"
	"spider-go/internal/modules/statistics"
	"spider-go/internal/modules/user"
	"spider-go/internal/service"
	"spider-go/internal/shared"
	pkgredis "spider-go/pkg/redis"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Container 依赖注入容器
type Container struct {
	Config *Config
	DB     *gorm.DB

	// Redis 客户端（同一个 Redis 服务器的不同数据库）
	SessionRedis *redis.Client // 会话 Redis (DB 0)
	CaptchaRedis *redis.Client // 验证码 Redis (DB 1)

	// 中间件
	CORSMiddleware gin.HandlerFunc

	// 共享查询服务
	UserQuery shared.UserQuery

	// Caches
	SessionCache    cache.SessionCache
	CaptchaCache    cache.CaptchaCache
	DAUCache        cache.DAUCache
	ConfigCache     cache.ConfigCache
	UserDataCache   cache.UserDataCache
	EvaluationCache cache.EvaluationCache
	MagicLinkCache  cache.MagicLinkCache

	// Services (infrastructure services only)
	RSAKeyService  service.RSAKeyService
	SessionService service.SessionService
	CrawlerService service.CrawlerService
	EmailService   service.EmailService
	DAUService     service.DAUService

	// Modules (new architecture)
	UserModule           *user.Module
	AdminModule          *admin.Module
	GradeModule          *grade.Module
	CourseModule         *course.Module
	ExamModule           *exam.Module
	EvaluationModule     *evaluation.Module
	NoticeModule         *notice.Module
	ConfigModule         *config.Module
	StatisticsModule     *statistics.Module
	RankingModule        *ranking.Module
	ReconciliationModule *reconciliation.Module
	ShareModule          *share.Module
	CourseTipsModule     *coursetips.Module
}

// NewContainer 创建依赖注入容器
func NewContainer(configPath string) (*Container, error) {
	c := &Container{}

	// 加载配置
	if err := c.initConfig(configPath); err != nil {
		return nil, fmt.Errorf("初始化配置失败: %w", err)
	}

	// 初始化数据库
	if err := c.initDB(); err != nil {
		return nil, fmt.Errorf("初始化数据库失败: %w", err)
	}

	// 初始化 Redis
	if err := c.initRedis(); err != nil {
		return nil, fmt.Errorf("初始化Redis失败: %w", err)
	}

	// 初始化共享查询服务
	c.initSharedServices()

	// 初始化 Caches
	c.initCaches()

	// 初始化 Services
	c.initServices()

	// 初始化中间件
	c.initMiddlewares()

	// 初始化 Modules
	c.initModules()

	// 初始化 RSA 公钥（首次获取）
	if err := c.initRSAPublicKey(); err != nil {
		return nil, fmt.Errorf("初始化 RSA 公钥失败: %w", err)
	}

	// 初始化默认管理员（如果不存在）
	if err := c.AdminModule.GetService().InitDefaultAdmin(context.Background()); err != nil {
		return nil, fmt.Errorf("初始化默认管理员失败: %w", err)
	}

	return c, nil
}

// initConfig 初始化配置
func (c *Container) initConfig(configPath string) error {
	config, err := LoadConfigFromPath(configPath)
	if err != nil {
		return err
	}
	c.Config = config
	return nil
}

// initDB 初始化数据库
func (c *Container) initDB() error {
	db, err := InitDBWithConfig(c.Config)
	if err != nil {
		return err
	}
	c.DB = db
	return nil
}

// initRedis 初始化 Redis（同一个 Redis 服务器的不同数据库）
func (c *Container) initRedis() error {
	// 初始化会话 Redis (DB 0) - 用于存储用户登录会话
	sessionClient, err := c.createRedisClient(c.Config.Redis.Session)
	if err != nil {
		return fmt.Errorf("初始化会话Redis失败: %w", err)
	}
	c.SessionRedis = sessionClient

	// 初始化验证码 Redis (DB 1) - 用于存储验证码
	captchaClient, err := c.createRedisClient(c.Config.Redis.Captcha)
	if err != nil {
		return fmt.Errorf("初始化验证码Redis失败: %w", err)
	}
	c.CaptchaRedis = captchaClient

	return nil
}

// createRedisClient 创建 Redis 客户端（使用 pkg/redis）
func (c *Container) createRedisClient(config RedisConfig) (*redis.Client, error) {
	pkgRedisConfig := pkgredis.Config{
		Host: config.Host,
		Port: config.Port,
		Pass: config.Pass,
		DB:   config.DB,
	}
	return pkgredis.NewClient(pkgRedisConfig)
}

// initSharedServices 初始化共享服务
func (c *Container) initSharedServices() {
	c.UserQuery = shared.NewUserQuery(c.DB)
}

// initCaches 初始化 Caches
func (c *Container) initCaches() {
	// 会话缓存（DB 0）
	c.SessionCache = cache.NewRedisSessionCache(c.SessionRedis)
	// 验证码缓存（DB 1）
	c.CaptchaCache = cache.NewRedisCaptchaCache(c.CaptchaRedis)
	// 日活统计缓存（DB 0，与会话共用）
	c.DAUCache = cache.NewRedisDAUCache(c.SessionRedis)
	// 系统配置缓存（DB 0，与会话共用）
	c.ConfigCache = cache.NewRedisConfigCache(c.SessionRedis)
	// 用户数据缓存（DB 0，与会话共用）
	c.UserDataCache = cache.NewRedisUserDataCache(c.SessionRedis)
	// 教评缓存 DB0
	c.EvaluationCache = cache.NewEvaluationCache(c.SessionRedis)
	// Magic Link 缓存（DB 0，与会话共用）
	c.MagicLinkCache = cache.NewRedisMagicLinkCache(c.SessionRedis)
}

// initServices 初始化 Services（仅基础设施服务）
func (c *Container) initServices() {
	// 获取当前模式的配置
	currentMode := c.Config.Jwc.GetCurrentModeConfig()
	log.Printf("教务系统模式: %s", c.Config.Jwc.Mode)

	// RSA Key Service（RSA 公钥服务）
	c.RSAKeyService = service.NewRSAKeyService(c.Config.Jwc.GetRSAKeyURL)

	// Session Service（根据配置模式注入对应的 URL）
	c.SessionService = service.NewJwcSessionService(
		c.SessionCache,
		c.RSAKeyService,
		c.Config.Jwc.Mode, // 注入当前模式
		currentMode.LoginURL,
		currentMode.RedirectURL,
		c.Config.Jwc.WebvpnTokenURL,
		c.Config.Jwc.MFADetectURL,
		c.Config.Jwc.CaptchaURL,
		c.Config.Jwc.CaptchaImageURL,
	)

	// Crawler Service
	c.CrawlerService = service.NewHttpCrawlerService()

	// Email Service（邮件服务）
	c.EmailService = service.NewEmailService(
		c.Config.Email.SMTPHost,
		c.Config.Email.SMTPPort,
		c.Config.Email.Username,
		c.Config.Email.Password,
		c.Config.Email.FromName,
	)

	// DAU Service（日活统计服务）
	c.DAUService = service.NewDAUService(c.DAUCache)
}

// initMiddlewares 初始化中间件
func (c *Container) initMiddlewares() {
	cors := c.Config.CORS
	c.CORSMiddleware = middleware.NewCORSMiddleware(
		cors.AllowOrigins,
		cors.AllowMethods,
		cors.AllowHeaders,
		cors.ExposeHeaders,
		cors.AllowCredentials,
		cors.MaxAge,
	)
}

// initModules 初始化模块
func (c *Container) initModules() {
	// 获取当前模式的配置
	currentMode := c.Config.Jwc.GetCurrentModeConfig()

	// User Module（用户模块）
	c.UserModule = user.NewModule(
		c.DB,
		c.SessionService,
		c.CaptchaCache,
		c.EmailService,
		c.DAUService,
		c.MagicLinkCache,
		c.Config.JWT.Secret,
		c.Config.JWT.Issuer,
		c.Config.Wx.AppId,
		c.Config.Wx.AppSecret,
		c.Config.App.BaseURL,
	)

	// Admin Module（管理员模块）
	c.AdminModule = admin.NewModule(
		c.DB,
		c.UserQuery,
		c.EmailService,
		c.CaptchaCache,
		c.Config.JWT.Secret,
		c.Config.JWT.Issuer,
	)

	// Grade Module（成绩模块）
	c.GradeModule = grade.NewModule(
		c.UserQuery,
		c.SessionService,
		c.CrawlerService,
		c.UserDataCache,
		c.ConfigCache,
		currentMode.GradeURL,
		currentMode.GradeLevelURL,
	)

	// Course Module（课程模块）
	c.CourseModule = course.NewModule(
		c.UserQuery,
		c.SessionService,
		c.CrawlerService,
		c.UserDataCache,
		currentMode.CourseURL,
	)

	// Exam Module（考试模块）
	c.ExamModule = exam.NewModule(
		c.UserQuery,
		c.SessionService,
		c.CrawlerService,
		c.UserDataCache,
		currentMode.ExamURL,
	)

	// Evaluation Module（教评模块）
	c.EvaluationModule = evaluation.NewModule(
		c.UserQuery,
		c.SessionService,
		c.EvaluationCache,
		c.SessionCache, // 添加 SessionCache 用于删除 TGC
		currentMode.EvaluationInfoURL,
		currentMode.EvaluationRedirectURL,
		currentMode.EvaluationDoLoginURL,
	)

	// Notice Module（通知模块）
	c.NoticeModule = notice.NewModule(c.DB)

	// Config Module（配置模块）
	c.ConfigModule = config.NewModule(c.ConfigCache)

	// Statistics Module（统计模块）
	c.StatisticsModule = statistics.NewModule(c.DAUService, c.UserModule.GetRepository())

	// Ranking Module（排名模块）
	c.RankingModule = ranking.NewModule(c.DB)

	// Reconciliation Module（对账模块）
	c.ReconciliationModule = reconciliation.NewModule(
		c.DB,
		c.GradeModule.GetService(),
		c.ExamModule.GetService(),
		c.CourseModule.GetService(),
		c.ConfigCache,
		c.RankingModule.GetService(),
	)

	// Share Module（分享模块）
	c.ShareModule = share.NewModule(c.UserQuery, c.CourseModule.GetService())

	// CourseTips Module（选课提示模块）
	c.CourseTipsModule = coursetips.NewModule(c.DB, c.SessionRedis)

	// 设置 Grade Module 的依赖（避免循环依赖，延迟注入）
	c.GradeModule.SetGradeRepository(c.DB)
	c.GradeModule.SetReconciliationTrigger(c.ReconciliationModule.GetService())

	// 设置 Reconciliation Module 的 UserQuery（用于清除绑定）
	c.ReconciliationModule.SetUserQuery(c.UserQuery)
}

// initRSAPublicKey 初始化 RSA 公钥
func (c *Container) initRSAPublicKey() error {
	log.Println("正在获取 RSA 公钥...")
	if err := c.RSAKeyService.FetchAndUpdate(); err != nil {
		return fmt.Errorf("首次获取 RSA 公钥失败: %w", err)
	}
	log.Println("RSA 公钥初始化成功")
	return nil
}

// Close 关闭资源
func (c *Container) Close() error {
	// 关闭会话 Redis
	if c.SessionRedis != nil {
		if err := c.SessionRedis.Close(); err != nil {
			return fmt.Errorf("关闭会话Redis失败: %w", err)
		}
	}

	// 关闭验证码 Redis
	if c.CaptchaRedis != nil {
		if err := c.CaptchaRedis.Close(); err != nil {
			return fmt.Errorf("关闭验证码Redis失败: %w", err)
		}
	}

	// 关闭数据库
	if c.DB != nil {
		sqlDB, err := c.DB.DB()
		if err != nil {
			return fmt.Errorf("获取数据库连接失败: %w", err)
		}
		if err := sqlDB.Close(); err != nil {
			return fmt.Errorf("关闭数据库失败: %w", err)
		}
	}

	return nil
}
