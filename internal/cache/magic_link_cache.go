package cache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// MagicLinkCache Magic Link 令牌缓存接口
type MagicLinkCache interface {
	// CreateToken 创建一个一次性 token，关联 email，设置过期时间
	CreateToken(ctx context.Context, email string, expiration time.Duration) (token string, err error)
	// VerifyAndConsume 验证并消费 token（原子操作，一次性）
	// 返回 token 关联的 email，如果无效/已过期/已使用则返回错误
	VerifyAndConsume(ctx context.Context, token string) (email string, err error)
	// DeleteByEmail 删除某个邮箱的所有未使用 token（用于取消操作）
	DeleteByEmail(ctx context.Context, email string) error
}

// RedisMagicLinkCache Redis 实现的 Magic Link 缓存
type RedisMagicLinkCache struct {
	client *redis.Client
}

// NewRedisMagicLinkCache 创建 Redis Magic Link 缓存
func NewRedisMagicLinkCache(client *redis.Client) MagicLinkCache {
	return &RedisMagicLinkCache{client: client}
}

const (
	magicTokenPrefix = "magiclink:token:" // token -> email
	magicEmailPrefix = "magiclink:email:" // email -> count (用于限制频率)
	magicTokenLength = 32                 // 32 bytes = 64 hex chars
)

// CreateToken 生成一次性 Magic Link token
func (c *RedisMagicLinkCache) CreateToken(ctx context.Context, email string, expiration time.Duration) (string, error) {
	b := make([]byte, magicTokenLength)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成 token 失败: %w", err)
	}
	token := hex.EncodeToString(b)

	tokenKey := magicTokenPrefix + token
	if err := c.client.Set(ctx, tokenKey, email, expiration).Err(); err != nil {
		return "", fmt.Errorf("存储 token 失败: %w", err)
	}

	// 记录该邮箱的 token 数量（用于限制每个邮箱的并发 token 数）
	emailKey := magicEmailPrefix + email
	c.client.Incr(ctx, emailKey)
	c.client.Expire(ctx, emailKey, expiration)

	return token, nil
}

// VerifyAndConsume 验证并消费 token
func (c *RedisMagicLinkCache) VerifyAndConsume(ctx context.Context, token string) (string, error) {
	tokenKey := magicTokenPrefix + token

	// 使用 Lua 脚本保证原子性：获取并删除
	script := `
		local email = redis.call('GET', KEYS[1])
		if email == false then
			return nil
		end
		redis.call('DEL', KEYS[1])
		return email
	`

	email, err := c.client.Eval(ctx, script, []string{tokenKey}).Result()
	if err != nil {
		return "", fmt.Errorf("验证 token 失败: %w", err)
	}
	if email == nil {
		return "", errors.New("链接无效或已过期")
	}

	emailStr, ok := email.(string)
	if !ok || emailStr == "" {
		return "", errors.New("链接无效")
	}

	return emailStr, nil
}

// DeleteByEmail 删除某个邮箱的所有未使用 token
func (c *RedisMagicLinkCache) DeleteByEmail(ctx context.Context, email string) error {
	emailKey := magicEmailPrefix + email
	// 删除计数 key
	return c.client.Del(ctx, emailKey).Err()
}
