package tasks

import (
	"context"
	"spider-go/internal/service"
)

// RSARefreshTask RSA 公钥刷新任务
type RSARefreshTask struct {
	rsaKeyService service.RSAKeyService
}

// NewRSARefreshTask 创建 RSA 公钥刷新任务
func NewRSARefreshTask(rsaKeyService service.RSAKeyService) *RSARefreshTask {
	return &RSARefreshTask{
		rsaKeyService: rsaKeyService,
	}
}

// Name 任务名称
func (t *RSARefreshTask) Name() string {
	return "RSA公钥刷新"
}

// Cron Cron 表达式（每小时执行一次）
func (t *RSARefreshTask) Cron() string {
	return "0 * * * *"
}

// Run 执行任务
func (t *RSARefreshTask) Run(ctx context.Context) error {
	return t.rsaKeyService.FetchAndUpdate()
}
