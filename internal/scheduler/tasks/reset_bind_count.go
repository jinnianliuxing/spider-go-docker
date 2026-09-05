package tasks

import (
	"context"
	"log"
	"time"

	"gorm.io/gorm"
)

// ResetBindCountTask 重置绑定计数任务（每月1号凌晨执行）
type ResetBindCountTask struct {
	db *gorm.DB
}

// NewResetBindCountTask 创建重置绑定计数任务
func NewResetBindCountTask(db *gorm.DB) *ResetBindCountTask {
	return &ResetBindCountTask{
		db: db,
	}
}

// Name 任务名称
func (t *ResetBindCountTask) Name() string {
	return "重置绑定计数"
}

// Cron Cron 表达式（每月1号凌晨0点执行）
func (t *ResetBindCountTask) Cron() string {
	return "0 0 1 * *"
}

// Run 执行任务
func (t *ResetBindCountTask) Run(ctx context.Context) error {
	currentMonth := time.Now().Format("2006-01")

	// 重置所有非当前月份的计数
	result := t.db.WithContext(ctx).
		Table("users").
		Where("bind_month != ? OR bind_month IS NULL", currentMonth).
		Updates(map[string]interface{}{
			"bind_count_current_month": 0,
			"bind_month":               currentMonth,
		})

	if result.Error != nil {
		log.Printf("[%s] 重置绑定计数失败: %v", t.Name(), result.Error)
		return result.Error
	}

	log.Printf("[%s] 重置绑定计数成功，影响 %d 条记录", t.Name(), result.RowsAffected)
	return nil
}
