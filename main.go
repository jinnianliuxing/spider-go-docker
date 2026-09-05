package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"spider-go/internal/api"
	"spider-go/internal/app"
	"spider-go/internal/scheduler"
	"spider-go/internal/scheduler/tasks"
	"strconv"
	"syscall"

	"github.com/gin-gonic/gin"
)

func main() {
	// 0. 解析命令行参数
	env := flag.String("env", "", "运行环境 (dev/production)，默认从 GO_ENV 环境变量读取，未设置则为 dev")
	flag.Parse()

	// 设置环境变量（命令行优先级高于环境变量）
	if *env != "" {
		os.Setenv("GO_ENV", *env)
	}

	// 1. 创建依赖注入容器（自动完成所有初始化，包括 RSA 公钥）
	container, err := app.NewContainer("./config")
	if err != nil {
		log.Fatalf("初始化容器失败: %v", err)
	}
	defer func() {
		if err := container.Close(); err != nil {
			log.Printf("关闭资源失败: %v", err)
		}
	}()

	// 输出当前运行环境
	log.Printf("运行环境: %s", container.Config.App.Env)

	// 2. 启动定时任务调度器
	scheduler := initScheduler(container)
	scheduler.Start()
	defer scheduler.Stop()

	// 3. 创建 Gin 引擎
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// 3.1 应用 CORS 中间件（全局）
	r.Use(container.CORSMiddleware)

	// 4. 设置路由
	api.SetupRoutes(r, container)

	// 5. 启动服务器
	port := container.Config.App.Port
	addr := ":" + strconv.Itoa(port)

	log.Printf("服务器启动在端口: %d\n", port)

	// 6. 优雅关闭
	go func() {
		if err := r.Run(addr); err != nil {
			log.Fatalf("服务器启动失败: %v", err)
		}
	}()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\n正在关闭服务器...")
}

// initScheduler 初始化定时任务调度器
func initScheduler(container *app.Container) *scheduler.Scheduler {
	s := scheduler.NewScheduler()

	// 添加 RSA 公钥刷新任务
	s.AddTask(tasks.NewRSARefreshTask(container.RSAKeyService))

	// 添加重置绑定计数任务（每月1号凌晨执行）
	s.AddTask(tasks.NewResetBindCountTask(container.DB))

	// 添加用户数据自动同步任务（每月1号凌晨2点执行）
	s.AddTask(tasks.NewUserSyncTask(
		container.UserModule.GetRepository(),
		container.SessionService,
		container.ReconciliationModule.GetService(),
	))

	// 添加同步日志清理任务（每天凌晨3点执行）
	s.AddTask(tasks.NewSyncLogCleanupTask(
		container.ReconciliationModule.GetRepository(),
	))

	return s
}
