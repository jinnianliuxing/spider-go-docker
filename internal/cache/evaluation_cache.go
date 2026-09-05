package cache

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type EvaluationCache interface {
	// GetCookies 获取用户的 cookies
	GetCookies(ctx context.Context, uid int) ([]*http.Cookie, error)
	// SetCookies 设置用户的 cookies
	SetCookies(ctx context.Context, uid int, cookies []*http.Cookie, expiration time.Duration) error
	// DeleteCookies 删除用户的 cookies
	DeleteCookies(ctx context.Context, uid int) error
	// HasCookies 检查用户是否有缓存的 cookies
	HasCookies(ctx context.Context, uid int) (bool, error)
	// GetAccessToken 获取用户的 accessToken
	GetAccessToken(ctx context.Context, uid int) (string, error)
	// SetAccessToken 设置用户的 accessToken
	SetAccessToken(ctx context.Context, uid int, accessToken string, expiration time.Duration) error
	// DeleteAccessToken 删除用户的 accessToken
	DeleteAccessToken(ctx context.Context, uid int) error
}

type RedisEvaluationCache struct {
	client *redis.Client
}

func NewEvaluationCache(client *redis.Client) EvaluationCache {
	return &RedisEvaluationCache{
		client: client,
	}
}
func (rc *RedisEvaluationCache) getUserKey(uid int) string {
	return "evaluation:" + strconv.Itoa(uid)
}

func (rc *RedisEvaluationCache) GetCookies(ctx context.Context, uid int) ([]*http.Cookie, error) {
	key := rc.getUserKey(uid)
	data, err := rc.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	var cookies []*http.Cookie
	if err := json.Unmarshal(data, &cookies); err != nil {
		return nil, err
	}

	return cookies, nil
}

func (rc *RedisEvaluationCache) SetCookies(ctx context.Context, uid int, cookies []*http.Cookie, expiration time.Duration) error {
	key := rc.getUserKey(uid)
	data, err := json.Marshal(cookies)
	if err != nil {
		return err
	}

	return rc.client.Set(ctx, key, data, expiration).Err()
}

func (rc *RedisEvaluationCache) DeleteCookies(ctx context.Context, uid int) error {
	key := rc.getUserKey(uid)
	return rc.client.Del(ctx, key).Err()
}

func (rc *RedisEvaluationCache) HasCookies(ctx context.Context, uid int) (bool, error) {
	key := rc.getUserKey(uid)
	count, err := rc.client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (rc *RedisEvaluationCache) getAccessTokenKey(uid int) string {
	return "evaluation:token:" + strconv.Itoa(uid)
}

func (rc *RedisEvaluationCache) GetAccessToken(ctx context.Context, uid int) (string, error) {
	key := rc.getAccessTokenKey(uid)
	token, err := rc.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return "", nil
		}
		return "", err
	}
	return token, nil
}

func (rc *RedisEvaluationCache) SetAccessToken(ctx context.Context, uid int, accessToken string, expiration time.Duration) error {
	key := rc.getAccessTokenKey(uid)
	return rc.client.Set(ctx, key, accessToken, expiration).Err()
}

func (rc *RedisEvaluationCache) DeleteAccessToken(ctx context.Context, uid int) error {
	key := rc.getAccessTokenKey(uid)
	return rc.client.Del(ctx, key).Err()
}
