package cache

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// QrSessionState 扫码登录会话状态
//
// 扫码是有状态的跨请求流程（出码 → 轮询 → 换票），中间要保持 CAS 的 SESSION cookie
// 和 comet 下发的 stateKey。存在 Redis 而不是内存，理由有二：
//  1. 出码与轮询是两个独立 HTTP 请求，必须能跨请求取回状态；
//  2. stateKey 与 SESSION 是一对一绑定的，放内存 map 在多副本部署下会串号。
//
// ⚠️ stateKey 必须服务端保存、绝不回传给前端 —— 它就是换票凭据，
// 泄露等于把别人的登录会话送出去。
type QrSessionState struct {
	UID       int            `json:"uid"` // 创建该扫码会话的用户；轮询/换票时校验，防串号
	Base      string         `json:"base"`
	Cookies   []*http.Cookie `json:"cookies"`
	StateKey  string         `json:"state_key"`
	FpVisitor string         `json:"fp_visitor"`
	LoginURL  string         `json:"login_url"`
}

// PhoneSessionState 手机号验证码登录会话状态
//
// 手机号绑定是**跨请求**的两步流程（发码 → 提交验证码换票），中间必须保住：
//   - CAS 的 SESSION cookie（发码与换票要在同一个 CAS 会话上）
//   - 本次登录的 login_url（含与 auth/start 会话匹配的 service 参数）
//   - 登录页里手机表单的 execution（CSRF/流程令牌，换票时必须原样提交）
//
// 与 QrSessionState 同理存 Redis：既要跨请求，也要支持多副本部署。
type PhoneSessionState struct {
	UID       int            `json:"uid"`   // 创建该会话的用户；提交验证码时校验，防串号
	Phone     string         `json:"phone"` // 本次登录用的手机号
	Base      string         `json:"base"`
	Cookies   []*http.Cookie `json:"cookies"`
	LoginURL  string         `json:"login_url"`
	Execution string         `json:"execution"`
	FpVisitor string         `json:"fp_visitor"`
}

// SessionCache 会话缓存接口
type SessionCache interface {
	// GetCookies 获取用户的 cookies
	GetCookies(ctx context.Context, uid int) ([]*http.Cookie, error)
	// SetCookies 设置用户的 cookies
	SetCookies(ctx context.Context, uid int, cookies []*http.Cookie, expiration time.Duration) error
	// DeleteCookies 删除用户的 cookies
	DeleteCookies(ctx context.Context, uid int) error
	// HasCookies 检查用户是否有缓存的 cookies
	HasCookies(ctx context.Context, uid int) (bool, error)
	// GetTGC 获取用户的 CAS TGC cookie
	GetTGC(ctx context.Context, uid int) (*http.Cookie, error)
	// SetTGC 设置用户的 CAS TGC cookie
	SetTGC(ctx context.Context, uid int, tgc *http.Cookie, expiration time.Duration) error
	// DeleteTGC 删除用户的 CAS TGC cookie
	DeleteTGC(ctx context.Context, uid int) error
	// ---- 扫码登录会话（与用户 uid 无关，用一次性 sessionID 关联）----
	// SetQrSession 保存扫码会话，key 为 sessionID
	SetQrSession(ctx context.Context, sessionID string, state *QrSessionState, expiration time.Duration) error
	// GetQrSession 取扫码会话，不存在返回 (nil, nil)
	GetQrSession(ctx context.Context, sessionID string) (*QrSessionState, error)
	// DeleteQrSession 删除扫码会话（登录完成或过期后清理）
	DeleteQrSession(ctx context.Context, sessionID string) error
	// ---- 手机号验证码登录会话（同上，一次性 sessionID）----
	// SetPhoneSession 保存手机号验证码会话，key 为 sessionID
	SetPhoneSession(ctx context.Context, sessionID string, state *PhoneSessionState, expiration time.Duration) error
	// GetPhoneSession 取手机号验证码会话，不存在返回 (nil, nil)
	GetPhoneSession(ctx context.Context, sessionID string) (*PhoneSessionState, error)
	// DeletePhoneSession 删除手机号验证码会话（换票成功或过期后清理）
	DeletePhoneSession(ctx context.Context, sessionID string) error
}

// RedisSessionCache Redis 实现的会话缓存
type RedisSessionCache struct {
	client *redis.Client
}

// NewRedisSessionCache 创建 Redis 会话缓存
func NewRedisSessionCache(client *redis.Client) SessionCache {
	return &RedisSessionCache{
		client: client,
	}
}

// GetCookies 获取用户的 cookies
func (c *RedisSessionCache) GetCookies(ctx context.Context, uid int) ([]*http.Cookie, error) {
	key := c.getUserKey(uid)
	data, err := c.client.Get(ctx, key).Bytes()
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

// SetCookies 设置用户的 cookies
func (c *RedisSessionCache) SetCookies(ctx context.Context, uid int, cookies []*http.Cookie, expiration time.Duration) error {
	key := c.getUserKey(uid)
	data, err := json.Marshal(cookies)
	if err != nil {
		return err
	}

	return c.client.Set(ctx, key, data, expiration).Err()
}

// DeleteCookies 删除用户的 cookies
func (c *RedisSessionCache) DeleteCookies(ctx context.Context, uid int) error {
	key := c.getUserKey(uid)
	return c.client.Del(ctx, key).Err()
}

// HasCookies 检查用户是否有缓存的 cookies
func (c *RedisSessionCache) HasCookies(ctx context.Context, uid int) (bool, error) {
	key := c.getUserKey(uid)
	count, err := c.client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// getUserKey 获取用户的 Redis key
func (c *RedisSessionCache) getUserKey(uid int) string {
	return "session:" + strconv.Itoa(uid)
}

// getTGCKey 获取用户的 TGC Redis key
func (c *RedisSessionCache) getTGCKey(uid int) string {
	return "session:tgc:" + strconv.Itoa(uid)
}

// GetTGC 获取用户的 CAS TGC cookie
func (c *RedisSessionCache) GetTGC(ctx context.Context, uid int) (*http.Cookie, error) {
	key := c.getTGCKey(uid)
	data, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	var tgc http.Cookie
	if err := json.Unmarshal(data, &tgc); err != nil {
		return nil, err
	}

	return &tgc, nil
}

// SetTGC 设置用户的 CAS TGC cookie
func (c *RedisSessionCache) SetTGC(ctx context.Context, uid int, tgc *http.Cookie, expiration time.Duration) error {
	key := c.getTGCKey(uid)
	data, err := json.Marshal(tgc)
	if err != nil {
		return err
	}

	return c.client.Set(ctx, key, data, expiration).Err()
}

// DeleteTGC 删除用户的 CAS TGC cookie
func (c *RedisSessionCache) DeleteTGC(ctx context.Context, uid int) error {
	key := c.getTGCKey(uid)
	return c.client.Del(ctx, key).Err()
}

// getQrKey 扫码会话的 Redis key
func (c *RedisSessionCache) getQrKey(sessionID string) string {
	return "qr:login:" + sessionID
}

// SetQrSession 保存扫码会话
func (c *RedisSessionCache) SetQrSession(ctx context.Context, sessionID string, state *QrSessionState, expiration time.Duration) error {
	if sessionID == "" || state == nil {
		return nil
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, c.getQrKey(sessionID), data, expiration).Err()
}

// GetQrSession 取扫码会话，不存在返回 (nil, nil)
func (c *RedisSessionCache) GetQrSession(ctx context.Context, sessionID string) (*QrSessionState, error) {
	data, err := c.client.Get(ctx, c.getQrKey(sessionID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	var state QrSessionState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// DeleteQrSession 删除扫码会话
func (c *RedisSessionCache) DeleteQrSession(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	return c.client.Del(ctx, c.getQrKey(sessionID)).Err()
}

// getPhoneKey 手机号验证码会话的 Redis key
func (c *RedisSessionCache) getPhoneKey(sessionID string) string {
	return "phone:login:" + sessionID
}

// SetPhoneSession 保存手机号验证码会话
func (c *RedisSessionCache) SetPhoneSession(ctx context.Context, sessionID string, state *PhoneSessionState, expiration time.Duration) error {
	if sessionID == "" || state == nil {
		return nil
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, c.getPhoneKey(sessionID), data, expiration).Err()
}

// GetPhoneSession 取手机号验证码会话，不存在返回 (nil, nil)
func (c *RedisSessionCache) GetPhoneSession(ctx context.Context, sessionID string) (*PhoneSessionState, error) {
	data, err := c.client.Get(ctx, c.getPhoneKey(sessionID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	var state PhoneSessionState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// DeletePhoneSession 删除手机号验证码会话
func (c *RedisSessionCache) DeletePhoneSession(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	return c.client.Del(ctx, c.getPhoneKey(sessionID)).Err()
}
