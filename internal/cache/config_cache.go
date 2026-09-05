package cache

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
)

// ConfigCache 系统配置缓存接口
type ConfigCache interface {
	// GetCurrentTerm 获取当前学期
	GetCurrentTerm(ctx context.Context) (string, error)
	// SetCurrentTerm 设置当前学期（管理员）
	SetCurrentTerm(ctx context.Context, term string) error
	// GetPreviousTerms 获取前 N 个学期
	GetPreviousTerms(ctx context.Context, count int) ([]string, error)
	// SetSemesterDates 设置学期开学和放假时间（管理员）
	SetSemesterDates(ctx context.Context, term string, startDate string, endDate string) error
	// GetSemesterDates 获取学期开学和放假时间
	GetSemesterDates(ctx context.Context, term string) (startDate string, endDate string, err error)
}

// RedisConfigCache Redis 实现的配置缓存
type RedisConfigCache struct {
	client *redis.Client
}

// NewRedisConfigCache 创建 Redis 配置缓存
func NewRedisConfigCache(client *redis.Client) ConfigCache {
	return &RedisConfigCache{
		client: client,
	}
}

const currentTermKey = "config:current_term"

// GetCurrentTerm 获取当前学期
func (c *RedisConfigCache) GetCurrentTerm(ctx context.Context) (string, error) {
	term, err := c.client.Get(ctx, currentTermKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// 如果没有设置，返回默认值
			return "", fmt.Errorf("当前学期未设置，请联系管理员配置")
		}
		return "", err
	}
	return term, nil
}

// SetCurrentTerm 设置当前学期
func (c *RedisConfigCache) SetCurrentTerm(ctx context.Context, term string) error {
	// 验证学期格式：2024-2025-1
	if !c.isValidTerm(term) {
		return fmt.Errorf("学期格式错误，应为：YYYY-YYYY-[1|2]，例如：2024-2025-1")
	}

	// 永久存储（不设置过期时间）
	return c.client.Set(ctx, currentTermKey, term, 0).Err()
}

// GetPreviousTerms 获取前 N 个学期
// 例如：当前学期 2024-2025-2，获取前2个学期：[2024-2025-1, 2023-2024-2]
func (c *RedisConfigCache) GetPreviousTerms(ctx context.Context, count int) ([]string, error) {
	currentTerm, err := c.GetCurrentTerm(ctx)
	if err != nil {
		return nil, err
	}

	terms := []string{currentTerm}

	// 解析当前学期
	parts := strings.Split(currentTerm, "-")
	if len(parts) != 3 {
		return nil, fmt.Errorf("学期格式错误")
	}

	startYear, _ := strconv.Atoi(parts[0])
	endYear, _ := strconv.Atoi(parts[1])
	semester, _ := strconv.Atoi(parts[2])

	// 向前推算 count-1 个学期
	for i := 1; i < count; i++ {
		if semester == 2 {
			// 当前是第2学期，前一个是第1学期（同学年）
			semester = 1
		} else {
			// 当前是第1学期，前一个是上一学年的第2学期
			semester = 2
			startYear--
			endYear--
		}

		prevTerm := fmt.Sprintf("%d-%d-%d", startYear, endYear, semester)
		terms = append(terms, prevTerm)
	}

	return terms, nil
}

// SetSemesterDates 设置学期开学和放假时间
func (c *RedisConfigCache) SetSemesterDates(ctx context.Context, term string, startDate string, endDate string) error {
	// 验证学期格式
	if !c.isValidTerm(term) {
		return fmt.Errorf("学期格式错误，应为：YYYY-YYYY-[1|2]，例如：2024-2025-1")
	}

	// 验证日期格式：YYYY-MM-DD
	if !c.isValidDate(startDate) {
		return fmt.Errorf("开学时间格式错误，应为：YYYY-MM-DD，例如：2024-09-01")
	}
	if !c.isValidDate(endDate) {
		return fmt.Errorf("放假时间格式错误，应为：YYYY-MM-DD，例如：2025-01-15")
	}

	// 验证开学时间必须早于放假时间
	if startDate >= endDate {
		return fmt.Errorf("开学时间必须早于放假时间")
	}

	// 存储到 Redis Hash 中
	key := fmt.Sprintf("config:semester_dates:%s", term)
	pipe := c.client.Pipeline()
	pipe.HSet(ctx, key, "start_date", startDate)
	pipe.HSet(ctx, key, "end_date", endDate)
	_, err := pipe.Exec(ctx)

	return err
}

// GetSemesterDates 获取学期开学和放假时间
func (c *RedisConfigCache) GetSemesterDates(ctx context.Context, term string) (string, string, error) {
	// 验证学期格式
	if !c.isValidTerm(term) {
		return "", "", fmt.Errorf("学期格式错误，应为：YYYY-YYYY-[1|2]，例如：2024-2025-1")
	}

	key := fmt.Sprintf("config:semester_dates:%s", term)
	result, err := c.client.HGetAll(ctx, key).Result()
	if err != nil {
		return "", "", err
	}

	// 如果没有设置，返回错误
	if len(result) == 0 {
		return "", "", fmt.Errorf("学期 %s 的开学和放假时间未设置", term)
	}

	startDate := result["start_date"]
	endDate := result["end_date"]

	if startDate == "" || endDate == "" {
		return "", "", fmt.Errorf("学期 %s 的开学和放假时间数据不完整", term)
	}

	return startDate, endDate, nil
}

// isValidTerm 验证学期格式
func (c *RedisConfigCache) isValidTerm(term string) bool {
	parts := strings.Split(term, "-")
	if len(parts) != 3 {
		return false
	}

	// 验证年份
	startYear, err1 := strconv.Atoi(parts[0])
	endYear, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}

	// 学年必须连续
	if endYear != startYear+1 {
		return false
	}

	// 学期只能是 1 或 2
	semester, err3 := strconv.Atoi(parts[2])
	if err3 != nil || (semester != 1 && semester != 2) {
		return false
	}

	return true
}

// isValidDate 验证日期格式：YYYY-MM-DD
func (c *RedisConfigCache) isValidDate(date string) bool {
	parts := strings.Split(date, "-")
	if len(parts) != 3 {
		return false
	}

	// 验证年份
	year, err := strconv.Atoi(parts[0])
	if err != nil || year < 2000 || year > 2100 {
		return false
	}

	// 验证月份
	month, err := strconv.Atoi(parts[1])
	if err != nil || month < 1 || month > 12 {
		return false
	}

	// 验证日期
	day, err := strconv.Atoi(parts[2])
	if err != nil || day < 1 || day > 31 {
		return false
	}

	return true
}
