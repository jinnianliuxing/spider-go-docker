package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// DAUCache 日活统计缓存接口 (Daily Active Users)
type DAUCache interface {
	// RecordActiveUser 记录活跃用户（幂等操作，同一用户同一天多次调用只记录一次）
	RecordActiveUser(ctx context.Context, uid int, date time.Time) error

	// GetDAU 获取指定日期的日活数量
	GetDAU(ctx context.Context, date time.Time) (int64, error)

	// GetActiveUsers 获取指定日期的所有活跃用户ID列表
	GetActiveUsers(ctx context.Context, date time.Time) ([]string, error)

	// IsUserActiveToday 检查用户今天是否已经活跃过（用于本地缓存判断）
	IsUserActiveToday(ctx context.Context, uid int, date time.Time) (bool, error)
}

// RedisDAUCache Redis 实现的日活统计缓存
type RedisDAUCache struct {
	client *redis.Client
}

// NewRedisDAUCache 创建 Redis 日活统计缓存
func NewRedisDAUCache(client *redis.Client) DAUCache {
	return &RedisDAUCache{
		client: client,
	}
}

// RecordActiveUser 记录活跃用户
// 使用 Redis Set 数据结构，自动去重
// 数据保留 30 天
func (c *RedisDAUCache) RecordActiveUser(ctx context.Context, uid int, date time.Time) error {
	key := c.getDAUKey(date)

	// 使用管道批量执行
	pipe := c.client.Pipeline()

	// 添加用户到集合
	pipe.SAdd(ctx, key, uid)

	// 设置 30 天过期时间（只在第一次创建时设置）
	pipe.Expire(ctx, key, 30*24*time.Hour)

	_, err := pipe.Exec(ctx)
	return err
}

// GetDAU 获取指定日期的日活数量
func (c *RedisDAUCache) GetDAU(ctx context.Context, date time.Time) (int64, error) {
	key := c.getDAUKey(date)
	// 使用 SCARD 命令获取 Set 中的元素数量
	return c.client.SCard(ctx, key).Result()
}

// GetActiveUsers 获取指定日期的所有活跃用户ID列表
func (c *RedisDAUCache) GetActiveUsers(ctx context.Context, date time.Time) ([]string, error) {
	key := c.getDAUKey(date)
	// 使用 SMEMBERS 命令获取 Set 中的所有成员
	return c.client.SMembers(ctx, key).Result()
}

// IsUserActiveToday 检查用户今天是否已经活跃过
func (c *RedisDAUCache) IsUserActiveToday(ctx context.Context, uid int, date time.Time) (bool, error) {
	key := c.getDAUKey(date)
	// 使用 SISMEMBER 命令检查成员是否存在
	return c.client.SIsMember(ctx, key, uid).Result()
}

// getDAUKey 生成日活统计的 Redis Key
// 格式：dau:2024-01-01
func (c *RedisDAUCache) getDAUKey(date time.Time) string {
	dateStr := date.Format("2006-01-02")
	return fmt.Sprintf("dau:%s", dateStr)
}
