package app

import (
	"fmt"
	"log"
	"os"
	"spider-go/internal/modules/admin"
	"spider-go/internal/modules/notice"
	"spider-go/internal/modules/ranking"
	"spider-go/internal/modules/reconciliation"
	"spider-go/internal/modules/user"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// InitDBWithConfig 使用配置初始化数据库
func InitDBWithConfig(config *Config) (*gorm.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		config.Database.User, config.Database.Pass, config.Database.Host, config.Database.Port, config.Database.Name)

	// 根据环境配置 GORM 日志
	gormLogger := configureGormLogger(config.App.Env)

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: gormLogger,
	})
	if err != nil {
		return nil, err
	}

	// 自动迁移（使用新模块中的模型）
	// 注意：先迁移被引用的表（User），再迁移引用它的表（UserWeChatMiniProgram）
	if err := db.AutoMigrate(
		&user.User{},
		&notice.Notice{},
		&admin.Admin{},
		&user.UserWeChatMiniProgram{},
		&notice.Introduction{},
		&user.JwcBindLog{}, // 绑定日志表
		// 对账/同步模块表
		&reconciliation.SyncTask{},
		&reconciliation.SyncLog{},
		&reconciliation.Grade{},
		&reconciliation.RegularGrade{},
		&reconciliation.Exam{},
		&reconciliation.LevelExam{},
		&reconciliation.Course{},
		&reconciliation.UserSyncStatus{},
		&ranking.StudentGPA{}, // 学生GPA数据（排名实时计算）
	); err != nil {
		return nil, err
	}

	return db, nil
}

// configureGormLogger 根据环境配置 GORM 日志
func configureGormLogger(env string) logger.Interface {
	var logLevel logger.LogLevel
	var slowThreshold time.Duration

	if env == "production" {
		// 生产环境：只打印 Warn 级别日志，慢查询阈值 1 秒
		logLevel = logger.Warn
		slowThreshold = 1 * time.Second
	} else {
		// 开发环境：打印 Info 级别日志，慢查询阈值 200ms
		logLevel = logger.Info
		slowThreshold = 200 * time.Millisecond
	}

	return logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             slowThreshold,       // 慢查询阈值
			LogLevel:                  logLevel,            // 日志级别
			IgnoreRecordNotFoundError: true,                // 忽略记录未找到错误
			Colorful:                  env != "production", // 开发环境彩色输出
		},
	)
}
