package tasks

import (
	"context"
	"log"
	"spider-go/internal/modules/reconciliation"
	"time"
)

const (
	retentionDays = 30
	batchSize     = 1000
	batchDelay    = 100 * time.Millisecond
)

// SyncLogCleanupTask 同步日志清理任务
type SyncLogCleanupTask struct {
	repo reconciliation.Repository
}

// NewSyncLogCleanupTask 创建同步日志清理任务
func NewSyncLogCleanupTask(repo reconciliation.Repository) *SyncLogCleanupTask {
	return &SyncLogCleanupTask{repo: repo}
}

// Name 任务名称
func (t *SyncLogCleanupTask) Name() string {
	return "同步日志清理"
}

// Cron 表达式（每天凌晨3点）
func (t *SyncLogCleanupTask) Cron() string {
	return "0 3 * * *"
}

// Run 执行清理任务
func (t *SyncLogCleanupTask) Run(ctx context.Context) error {
	before := time.Now().AddDate(0, 0, -retentionDays)
	log.Printf("[SyncLogCleanup] 开始清理 %s 之前的同步日志...", before.Format("2006-01-02"))

	// 清理 sync_tasks
	totalTasks := int64(0)
	for {
		deleted, err := t.repo.DeleteOldSyncTasks(ctx, before, batchSize)
		if err != nil {
			log.Printf("[SyncLogCleanup] 删除 sync_tasks 批次失败: %v", err)
			break
		}
		totalTasks += deleted
		if deleted < int64(batchSize) {
			break
		}
		time.Sleep(batchDelay)
	}

	// 清理 sync_logs
	totalLogs := int64(0)
	for {
		deleted, err := t.repo.DeleteOldSyncLogs(ctx, before, batchSize)
		if err != nil {
			log.Printf("[SyncLogCleanup] 删除 sync_logs 批次失败: %v", err)
			break
		}
		totalLogs += deleted
		if deleted < int64(batchSize) {
			break
		}
		time.Sleep(batchDelay)
	}

	log.Printf("[SyncLogCleanup] 清理完成: 删除 sync_tasks %d 条, sync_logs %d 条", totalTasks, totalLogs)
	return nil
}
