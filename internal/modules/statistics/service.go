package statistics

import (
	"context"
	"spider-go/internal/common"
	"spider-go/internal/modules/user"
	"spider-go/internal/service"
	"time"
)

// Service 统计服务接口
type Service interface {
	// GetTodayDAU 获取今日DAU
	GetTodayDAU(ctx context.Context) (int64, error)
	// GetDAUByDate 获取指定日期的DAU
	GetDAUByDate(ctx context.Context, date time.Time) (int64, error)
	// GetDAURange 获取指定日期范围的DAU
	GetDAURange(ctx context.Context, startDate, endDate time.Time) (map[string]int64, error)
	// GetUserCount 获取用户总数
	GetUserCount(ctx context.Context) (int64, error)
	// GetNewUserCount 获取新增用户数（按日期或日期范围）
	GetNewUserCount(ctx context.Context, date *time.Time, startDate *time.Time, endDate *time.Time) (int64, error)
}

type statisticsService struct {
	dauService service.DAUService
	userRepo   user.Repository
}

// NewService 创建统计服务
func NewService(dauService service.DAUService, userRepo user.Repository) Service {
	return &statisticsService{
		dauService: dauService,
		userRepo:   userRepo,
	}
}

// GetTodayDAU 获取今日DAU
func (s *statisticsService) GetTodayDAU(ctx context.Context) (int64, error) {
	count, err := s.dauService.GetTodayDAU(ctx)
	if err != nil {
		return 0, common.NewAppError(common.CodeInternalError, "获取今日DAU失败")
	}
	return count, nil
}

// GetDAUByDate 获取指定日期的DAU
func (s *statisticsService) GetDAUByDate(ctx context.Context, date time.Time) (int64, error) {
	count, err := s.dauService.GetDAUByDate(ctx, date)
	if err != nil {
		return 0, common.NewAppError(common.CodeInternalError, "获取DAU失败")
	}
	return count, nil
}

// GetDAURange 获取指定日期范围的DAU
func (s *statisticsService) GetDAURange(ctx context.Context, startDate, endDate time.Time) (map[string]int64, error) {
	// 验证日期范围（最多查询31天）
	if endDate.Sub(startDate) > 31*24*time.Hour {
		return nil, common.NewAppError(common.CodeInvalidParams, "日期范围不能超过31天")
	}

	// 验证开始日期不能晚于结束日期
	if startDate.After(endDate) {
		return nil, common.NewAppError(common.CodeInvalidParams, "开始日期不能晚于结束日期")
	}

	data, err := s.dauService.GetDAURange(ctx, startDate, endDate)
	if err != nil {
		return nil, common.NewAppError(common.CodeInternalError, "获取DAU范围失败")
	}
	return data, nil
}

// GetUserCount 获取用户总数
func (s *statisticsService) GetUserCount(ctx context.Context) (int64, error) {
	count, err := s.userRepo.CountUsers(ctx)
	if err != nil {
		return 0, common.NewAppError(common.CodeInternalError, "获取用户总数失败")
	}
	return count, nil
}

// GetNewUserCount 获取新增用户数（按日期或日期范围）
func (s *statisticsService) GetNewUserCount(ctx context.Context, date *time.Time, startDate *time.Time, endDate *time.Time) (int64, error) {
	// 优先使用日期范围查询
	if startDate != nil && endDate != nil {
		// 验证日期范围（最多查询365天）
		if endDate.Sub(*startDate) > 365*24*time.Hour {
			return 0, common.NewAppError(common.CodeInvalidParams, "日期范围不能超过365天")
		}

		// 验证开始日期不能晚于结束日期
		if startDate.After(*endDate) {
			return 0, common.NewAppError(common.CodeInvalidParams, "开始日期不能晚于结束日期")
		}

		count, err := s.userRepo.CountNewUsersByDateRange(ctx, *startDate, *endDate)
		if err != nil {
			return 0, common.NewAppError(common.CodeInternalError, "获取新增用户数失败")
		}
		return count, nil
	}

	// 如果没有日期范围，使用单日查询
	if date != nil {
		count, err := s.userRepo.CountNewUsersByDate(ctx, *date)
		if err != nil {
			return 0, common.NewAppError(common.CodeInternalError, "获取新增用户数失败")
		}
		return count, nil
	}

	// 如果既没有日期范围也没有单日，返回今日新增
	today := time.Now()
	count, err := s.userRepo.CountNewUsersByDate(ctx, today)
	if err != nil {
		return 0, common.NewAppError(common.CodeInternalError, "获取今日新增用户数失败")
	}
	return count, nil
}
