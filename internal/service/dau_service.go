package service

import (
	"context"
	"spider-go/internal/cache"
	"spider-go/internal/common"
	"sync"
	"time"
)

// DAUService 日活统计服务接口
type DAUService interface {
	// RecordUserActivity 记录用户活跃（智能去重，同一用户同一天只记录一次）
	RecordUserActivity(ctx context.Context, uid int) error

	// GetTodayDAU 获取今日日活数量
	GetTodayDAU(ctx context.Context) (int64, error)

	// GetDAUByDate 获取指定日期的日活数量
	GetDAUByDate(ctx context.Context, date time.Time) (int64, error)

	// GetDAURange 获取指定日期范围的日活统计（用于报表）
	GetDAURange(ctx context.Context, startDate, endDate time.Time) (map[string]int64, error)
}

// dauServiceImpl 日活统计服务实现
type dauServiceImpl struct {
	dauCache      cache.DAUCache
	localCache    map[string]bool // 本地缓存，避免同一用户短时间内重复写 Redis
	cacheMutex    sync.RWMutex
	cacheExpireAt time.Time // 本地缓存过期时间（每天 0 点清空）
}

// NewDAUService 创建日活统计服务
func NewDAUService(dauCache cache.DAUCache) DAUService {
	return &dauServiceImpl{
		dauCache:      dauCache,
		localCache:    make(map[string]bool),
		cacheExpireAt: getNextMidnight(),
	}
}

// RecordUserActivity 记录用户活跃
func (s *dauServiceImpl) RecordUserActivity(ctx context.Context, uid int) error {
	now := time.Now()

	// 1. 检查本地缓存是否需要清空（跨天了）
	s.checkAndResetLocalCache(now)

	// 2. 检查本地缓存，避免重复写 Redis
	cacheKey := s.getLocalCacheKey(uid, now)
	if s.isInLocalCache(cacheKey) {
		return nil // 今天已经记录过了，直接返回
	}

	// 3. 写入 Redis（幂等操作）
	if err := s.dauCache.RecordActiveUser(ctx, uid, now); err != nil {
		return common.NewAppError(common.CodeCacheError, "记录用户活跃失败")
	}

	// 4. 更新本地缓存
	s.addToLocalCache(cacheKey)

	return nil
}

// GetTodayDAU 获取今日日活数量
func (s *dauServiceImpl) GetTodayDAU(ctx context.Context) (int64, error) {
	return s.GetDAUByDate(ctx, time.Now())
}

// GetDAUByDate 获取指定日期的日活数量
func (s *dauServiceImpl) GetDAUByDate(ctx context.Context, date time.Time) (int64, error) {
	count, err := s.dauCache.GetDAU(ctx, date)
	if err != nil {
		return 0, common.NewAppError(common.CodeCacheError, "获取日活数据失败")
	}
	return count, nil
}

// GetDAURange 获取指定日期范围的日活统计
func (s *dauServiceImpl) GetDAURange(ctx context.Context, startDate, endDate time.Time) (map[string]int64, error) {
	result := make(map[string]int64)

	// 遍历日期范围
	for date := startDate; !date.After(endDate); date = date.AddDate(0, 0, 1) {
		count, err := s.dauCache.GetDAU(ctx, date)
		if err != nil {
			return nil, common.NewAppError(common.CodeCacheError, "获取日活数据失败")
		}
		dateStr := date.Format("2006-01-02")
		result[dateStr] = count
	}

	return result, nil
}

// checkAndResetLocalCache 检查并重置本地缓存（跨天清空）
func (s *dauServiceImpl) checkAndResetLocalCache(now time.Time) {
	s.cacheMutex.Lock()
	defer s.cacheMutex.Unlock()

	if now.After(s.cacheExpireAt) {
		// 跨天了，清空本地缓存
		s.localCache = make(map[string]bool)
		s.cacheExpireAt = getNextMidnight()
	}
}

// isInLocalCache 检查是否在本地缓存中
func (s *dauServiceImpl) isInLocalCache(key string) bool {
	s.cacheMutex.RLock()
	defer s.cacheMutex.RUnlock()
	return s.localCache[key]
}

// addToLocalCache 添加到本地缓存
func (s *dauServiceImpl) addToLocalCache(key string) {
	s.cacheMutex.Lock()
	defer s.cacheMutex.Unlock()
	s.localCache[key] = true
}

// getLocalCacheKey 生成本地缓存 Key
// 格式：uid:2024-01-01
func (s *dauServiceImpl) getLocalCacheKey(uid int, date time.Time) string {
	dateStr := date.Format("2006-01-02")
	return dateStr + ":" + string(rune(uid))
}

// getNextMidnight 获取下一个午夜时间
func getNextMidnight() time.Time {
	now := time.Now()
	tomorrow := now.AddDate(0, 0, 1)
	return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, now.Location())
}
