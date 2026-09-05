package scheduler

import (
	"context"
	"log"

	"github.com/robfig/cron/v3"
)

// Scheduler 任务调度器
type Scheduler struct {
	cron  *cron.Cron
	tasks []Task
}

// Task 定时任务接口
type Task interface {
	// Name 任务名称
	Name() string
	// Cron Cron 表达式
	Cron() string
	// Run 执行任务
	Run(ctx context.Context) error
}

// NewScheduler 创建调度器
func NewScheduler() *Scheduler {
	return &Scheduler{
		cron:  cron.New(),
		tasks: make([]Task, 0),
	}
}

// AddTask 添加任务
func (s *Scheduler) AddTask(task Task) {
	s.tasks = append(s.tasks, task)
}

// Start 启动调度器
func (s *Scheduler) Start() {
	for _, task := range s.tasks {
		t := task // 避免闭包问题
		_, err := s.cron.AddFunc(t.Cron(), func() {
			log.Printf("触发定时任务: %s", t.Name())
			if err := t.Run(context.Background()); err != nil {
				log.Printf("任务 %s 执行失败: %v", t.Name(), err)
			}
		})
		if err != nil {
			log.Fatalf("添加定时任务 %s 失败: %v", t.Name(), err)
		}
	}

	s.cron.Start()
	log.Println("定时任务调度器已启动")
}

// Stop 停止调度器
func (s *Scheduler) Stop() {
	if s.cron != nil {
		s.cron.Stop()
		log.Println("定时任务调度器已停止")
	}
}
