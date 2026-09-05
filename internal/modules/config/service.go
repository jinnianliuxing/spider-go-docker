package config

import (
	"context"
	"spider-go/internal/cache"
	"spider-go/internal/common"
)

// Service 配置服务接口
type Service interface {
	// GetCurrentTerm 获取当前学期
	GetCurrentTerm(ctx context.Context) (string, error)
	// SetCurrentTerm 设置当前学期（管理员）
	SetCurrentTerm(ctx context.Context, term string) error
	// GetSemesterDates 获取学期日期
	GetSemesterDates(ctx context.Context, term string) (startDate, endDate string, err error)
	// SetSemesterDates 设置学期日期（管理员）
	SetSemesterDates(ctx context.Context, term, startDate, endDate string) error
}

type service struct {
	configCache cache.ConfigCache
}

// NewService 创建配置服务
func NewService(configCache cache.ConfigCache) Service {
	return &service{
		configCache: configCache,
	}
}

// GetCurrentTerm 获取当前学期
func (s *service) GetCurrentTerm(ctx context.Context) (string, error) {
	term, err := s.configCache.GetCurrentTerm(ctx)
	if err != nil {
		return "", common.NewAppError(common.CodeInternalError, err.Error())
	}
	return term, nil
}

// SetCurrentTerm 设置当前学期
func (s *service) SetCurrentTerm(ctx context.Context, term string) error {
	if err := s.configCache.SetCurrentTerm(ctx, term); err != nil {
		return common.NewAppError(common.CodeInvalidParams, err.Error())
	}
	return nil
}

// GetSemesterDates 获取学期日期
func (s *service) GetSemesterDates(ctx context.Context, term string) (string, string, error) {
	startDate, endDate, err := s.configCache.GetSemesterDates(ctx, term)
	if err != nil {
		return "", "", common.NewAppError(common.CodeInternalError, err.Error())
	}
	return startDate, endDate, nil
}

// SetSemesterDates 设置学期日期
func (s *service) SetSemesterDates(ctx context.Context, term, startDate, endDate string) error {
	if err := s.configCache.SetSemesterDates(ctx, term, startDate, endDate); err != nil {
		return common.NewAppError(common.CodeInvalidParams, err.Error())
	}
	return nil
}
