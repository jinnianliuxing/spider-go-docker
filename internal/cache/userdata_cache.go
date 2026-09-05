package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// UserDataCache 用户数据缓存接口
type UserDataCache interface {
	// CacheGrades 缓存成绩数据
	CacheGrades(ctx context.Context, uid int, term string, data interface{}, expiration time.Duration) error
	// GetGrades 获取成绩缓存
	GetGrades(ctx context.Context, uid int, term string, target interface{}) error

	// CacheCourseTable 缓存课表数据
	CacheCourseTable(ctx context.Context, uid int, term string, week int, data interface{}, expiration time.Duration) error
	// GetCourseTable 获取课表缓存
	GetCourseTable(ctx context.Context, uid int, term string, week int, target interface{}) error

	// CacheExams 缓存考试安排
	CacheExams(ctx context.Context, uid int, term string, data interface{}, expiration time.Duration) error
	// GetExams 获取考试安排缓存
	GetExams(ctx context.Context, uid int, term string, target interface{}) error
	// CacheRegularGrades 缓存平时成绩
	CacheRegularGrades(ctx context.Context, uid int, term string, data interface{}, expiration time.Duration) error
	// GetRegularGrades 获取平时成绩
	GetRegularGrades(ctx context.Context, uid int, term string, target interface{}) error

	// CacheStudentInfo 缓存学生信息（年级、学院、专业、班级）
	CacheStudentInfo(ctx context.Context, uid int, data interface{}, expiration time.Duration) error
	// GetStudentInfo 获取学生信息缓存
	GetStudentInfo(ctx context.Context, uid int, target interface{}) error
}

// RedisUserDataCache Redis 实现的用户数据缓存
type RedisUserDataCache struct {
	client *redis.Client
}

// NewRedisUserDataCache 创建 Redis 用户数据缓存
func NewRedisUserDataCache(client *redis.Client) UserDataCache {
	return &RedisUserDataCache{
		client: client,
	}
}

// CacheGrades 缓存成绩数据
func (c *RedisUserDataCache) CacheGrades(ctx context.Context, uid int, term string, data interface{}, expiration time.Duration) error {
	key := c.getGradesKey(uid, term)
	bytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, key, bytes, expiration).Err()
}

// GetGrades 获取成绩缓存
func (c *RedisUserDataCache) GetGrades(ctx context.Context, uid int, term string, target interface{}) error {
	key := c.getGradesKey(uid, term)
	bytes, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes, target)
}

// CacheCourseTable 缓存课表数据
func (c *RedisUserDataCache) CacheCourseTable(ctx context.Context, uid int, term string, week int, data interface{}, expiration time.Duration) error {
	key := c.getCourseKey(uid, term, week)
	bytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, key, bytes, expiration).Err()
}

// GetCourseTable 获取课表缓存
func (c *RedisUserDataCache) GetCourseTable(ctx context.Context, uid int, term string, week int, target interface{}) error {
	key := c.getCourseKey(uid, term, week)
	bytes, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes, target)
}

// CacheExams 缓存考试安排
func (c *RedisUserDataCache) CacheExams(ctx context.Context, uid int, term string, data interface{}, expiration time.Duration) error {
	key := c.getExamKey(uid, term)
	bytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, key, bytes, expiration).Err()
}

// GetExams 获取考试安排缓存
func (c *RedisUserDataCache) GetExams(ctx context.Context, uid int, term string, target interface{}) error {
	key := c.getExamKey(uid, term)
	bytes, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes, target)
}

// CacheRegularGrades 缓存平时成绩 (使用 Hash 存储)
func (c *RedisUserDataCache) CacheRegularGrades(ctx context.Context, uid int, term string, data interface{}, expiration time.Duration) error {
	key := c.getRegularGradesKey(uid, term)

	// 将 data 转换为 map[string]interface{}
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("data must be map[string]interface{}")
	}

	// 使用 HSet 存储哈希表
	if err := c.client.HSet(ctx, key, dataMap).Err(); err != nil {
		return err
	}

	// 设置过期时间
	return c.client.Expire(ctx, key, expiration).Err()
}

// GetRegularGrades 获取平时成绩缓存
func (c *RedisUserDataCache) GetRegularGrades(ctx context.Context, uid int, term string, target interface{}) error {
	key := c.getRegularGradesKey(uid, term)

	// 获取整个哈希表
	result, err := c.client.HGetAll(ctx, key).Result()
	if err != nil {
		return err
	}

	// 检查是否为空
	if len(result) == 0 {
		return redis.Nil
	}

	// 将 result 转换为 target 类型
	bytes, err := json.Marshal(result)
	if err != nil {
		return err
	}

	return json.Unmarshal(bytes, target)
}

// 键生成辅助方法
func (c *RedisUserDataCache) getGradesKey(uid int, term string) string {
	if term == "" {
		return fmt.Sprintf("data:grades:%d:all", uid)
	}
	return fmt.Sprintf("data:grades:%d:%s", uid, term)
}

func (c *RedisUserDataCache) getCourseKey(uid int, term string, week int) string {
	return fmt.Sprintf("data:course:%d:%s:%d", uid, term, week)
}

func (c *RedisUserDataCache) getExamKey(uid int, term string) string {
	return fmt.Sprintf("data:exam:%d:%s", uid, term)
}

func (c *RedisUserDataCache) getRegularGradesKey(uid int, term string) string {
	if term == "" {
		return fmt.Sprintf("data:regular:%d:all", uid)
	}
	return fmt.Sprintf("data:regular:%d:%s", uid, term)
}

// CacheStudentInfo 缓存学生信息
func (c *RedisUserDataCache) CacheStudentInfo(ctx context.Context, uid int, data interface{}, expiration time.Duration) error {
	key := c.getStudentInfoKey(uid)
	bytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, key, bytes, expiration).Err()
}

// GetStudentInfo 获取学生信息缓存
func (c *RedisUserDataCache) GetStudentInfo(ctx context.Context, uid int, target interface{}) error {
	key := c.getStudentInfoKey(uid)
	bytes, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes, target)
}

func (c *RedisUserDataCache) getStudentInfoKey(uid int) string {
	return fmt.Sprintf("data:student_info:%d", uid)
}
