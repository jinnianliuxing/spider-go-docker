package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// CaptchaCache 验证码缓存接口
type CaptchaCache interface {
	// SetCaptcha 存储验证码
	// key: 验证码的唯一标识（如手机号、邮箱等）
	// value: 验证码内容
	// expiration: 过期时间（通常为 5 分钟）
	SetCaptcha(ctx context.Context, key string, value string, expiration time.Duration) error

	// GetCaptcha 获取验证码
	// 返回验证码内容，如果不存在或已过期则返回空字符串
	GetCaptcha(ctx context.Context, key string) (string, error)

	// DeleteCaptcha 删除验证码
	// 验证成功后应该立即删除，防止重复使用
	DeleteCaptcha(ctx context.Context, key string) error

	// VerifyAndDelete 验证并删除验证码
	// 保证验证和删除的原子性，防止并发问题
	// 返回值：true-验证成功，false-验证失败
	VerifyAndDelete(ctx context.Context, key string, code string) (bool, error)

	// SetCooldown 设置发送冷却标记
	// 在冷却时间内禁止再次发送验证码
	SetCooldown(ctx context.Context, key string, expiration time.Duration) error

	// HasCooldown 检查是否在冷却中
	// 返回 true 表示还在冷却期内，不可发送
	HasCooldown(ctx context.Context, key string) (bool, error)
}

// RedisCaptchaCache Redis 实现的验证码缓存
type RedisCaptchaCache struct {
	client *redis.Client
}

// NewRedisCaptchaCache 创建 Redis 验证码缓存
func NewRedisCaptchaCache(client *redis.Client) CaptchaCache {
	return &RedisCaptchaCache{
		client: client,
	}
}

// SetCaptcha 存储验证码
func (c *RedisCaptchaCache) SetCaptcha(ctx context.Context, key string, value string, expiration time.Duration) error {
	fullKey := c.getCaptchaKey(key)
	return c.client.Set(ctx, fullKey, value, expiration).Err()
}

// GetCaptcha 获取验证码
func (c *RedisCaptchaCache) GetCaptcha(ctx context.Context, key string) (string, error) {
	fullKey := c.getCaptchaKey(key)
	value, err := c.client.Get(ctx, fullKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", nil // 验证码不存在或已过期
		}
		return "", err
	}
	return value, nil
}

// DeleteCaptcha 删除验证码
func (c *RedisCaptchaCache) DeleteCaptcha(ctx context.Context, key string) error {
	fullKey := c.getCaptchaKey(key)
	return c.client.Del(ctx, fullKey).Err()
}

// VerifyAndDelete 验证并删除验证码（使用 Lua 脚本保证原子性）
func (c *RedisCaptchaCache) VerifyAndDelete(ctx context.Context, key string, code string) (bool, error) {
	fullKey := c.getCaptchaKey(key)

	// Lua 脚本保证原子性：获取、比较、删除在一个事务中完成
	// 返回值：0-不存在或已过期，1-验证成功，2-验证码错误
	script := `
		local value = redis.call('GET', KEYS[1])
		if value == false then
			return 0
		end
		if value == ARGV[1] then
			redis.call('DEL', KEYS[1])
			return 1
		end
		return 2
	`

	result, err := c.client.Eval(ctx, script, []string{fullKey}, code).Int()
	if err != nil {
		return false, err
	}

	switch result {
	case 0:
		return false, fmt.Errorf("验证码不存在或已过期")
	case 1:
		return true, nil // 验证成功
	case 2:
		return false, fmt.Errorf("验证码错误")
	default:
		return false, fmt.Errorf("未知错误")
	}
}

// getCooldownKey 获取冷却标记的 Redis key
func (c *RedisCaptchaCache) getCooldownKey(key string) string {
	return "captcha:cooldown:" + key
}

// SetCooldown 设置发送冷却标记
func (c *RedisCaptchaCache) SetCooldown(ctx context.Context, key string, expiration time.Duration) error {
	fullKey := c.getCooldownKey(key)
	return c.client.Set(ctx, fullKey, "1", expiration).Err()
}

// HasCooldown 检查是否在冷却中
func (c *RedisCaptchaCache) HasCooldown(ctx context.Context, key string) (bool, error) {
	fullKey := c.getCooldownKey(key)
	_, err := c.client.Get(ctx, fullKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return false, nil // 冷却已过，可以发送
		}
		return false, err
	}
	return true, nil // 冷却中
}

// getCaptchaKey 获取验证码的 Redis key（添加前缀以区分不同类型的数据）
func (c *RedisCaptchaCache) getCaptchaKey(key string) string {
	return "captcha:" + key
}
