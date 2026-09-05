package tasks

import (
	"context"
	"log"
	"spider-go/internal/modules/reconciliation"
	"spider-go/internal/modules/user"
	"spider-go/internal/service"
	"time"
)

// UserSyncTask 用户数据同步任务
type UserSyncTask struct {
	userRepo              user.Repository
	sessionService        service.SessionService
	reconciliationService reconciliation.Service
}

// NewUserSyncTask 创建用户数据同步任务
func NewUserSyncTask(
	userRepo user.Repository,
	sessionService service.SessionService,
	reconciliationService reconciliation.Service,
) *UserSyncTask {
	return &UserSyncTask{
		userRepo:              userRepo,
		sessionService:        sessionService,
		reconciliationService: reconciliationService,
	}
}

// Name 任务名称
func (t *UserSyncTask) Name() string {
	return "用户数据自动同步"
}

// Cron Cron 表达式（每月1号凌晨2点执行）
func (t *UserSyncTask) Cron() string {
	return "0 2 1 * *"
}

// Run 执行任务
func (t *UserSyncTask) Run(ctx context.Context) error {
	log.Println("[UserSyncTask] 开始执行用户数据同步任务...")

	// 1. 获取所有已绑定教务系统的用户
	users, err := t.userRepo.FindAllBoundUsers(ctx)
	if err != nil {
		log.Printf("[UserSyncTask] 获取已绑定用户列表失败: %v", err)
		return err
	}

	log.Printf("[UserSyncTask] 共有 %d 个已绑定用户需要同步", len(users))

	successCount := 0
	failCount := 0
	unbindCount := 0

	for _, u := range users {
		// 检查用户是否已绑定
		if u.Sid == "" || u.Spwd == "" {
			continue
		}

		// 2. 尝试登录验证密码是否有效
		err := t.sessionService.LoginCheck(ctx, u.Sid, u.Spwd)
		if err != nil {
			// 登录失败，密码无效，清除绑定信息
			log.Printf("[UserSyncTask] 用户 %d (学号: %s) 登录失败，清除绑定信息: %v", u.Uid, u.Sid, err)
			if clearErr := t.userRepo.ClearJwcBinding(ctx, u.Uid); clearErr != nil {
				log.Printf("[UserSyncTask] 清除用户 %d 绑定信息失败: %v", u.Uid, clearErr)
			} else {
				unbindCount++
			}
			failCount++
			continue
		}

		// 3. 登录成功，执行数据同步（使用 SyncUser 方法同步所有数据）
		task, err := t.reconciliationService.SyncUser(ctx, u.Uid, reconciliation.TaskTypeAll, reconciliation.TriggerTypeScheduled)
		if err != nil {
			log.Printf("[UserSyncTask] 用户 %d 数据同步失败: %v", u.Uid, err)
			failCount++
			continue
		}

		log.Printf("[UserSyncTask] 用户 %d 数据同步任务已启动，任务ID: %s", u.Uid, task.TaskID)
		successCount++

		// 每个用户之间间隔一段时间，避免对教务系统造成压力
		time.Sleep(2 * time.Second)
	}

	log.Printf("[UserSyncTask] 用户数据同步任务完成，成功: %d, 失败: %d, 解绑: %d", successCount, failCount, unbindCount)
	return nil
}
