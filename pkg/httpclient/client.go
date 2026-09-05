package httpclient

import (
	"context"
	"net/http"
	"time"
)

// Client HTTP客户端接口
type Client interface {
	Do(ctx context.Context, req *http.Request) (*http.Response, error)
	Get(ctx context.Context, url string) (*http.Response, error)
	Post(ctx context.Context, url string, body interface{}) (*http.Response, error)
}

// Config HTTP客户端配置
type Config struct {
	Timeout         time.Duration
	MaxIdleConns    int
	IdleConnTimeout time.Duration
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Timeout:         30 * time.Second,
		MaxIdleConns:    100,
		IdleConnTimeout: 90 * time.Second,
	}
}

// client HTTP客户端实现
type client struct {
	httpClient *http.Client
}

// New 创建新的HTTP客户端
func New(cfg *Config) Client {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	return &client{
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:    cfg.MaxIdleConns,
				IdleConnTimeout: cfg.IdleConnTimeout,
			},
		},
	}
}

// Do 执行HTTP请求
func (c *client) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	return c.httpClient.Do(req.WithContext(ctx))
}

// Get 执行GET请求
func (c *client) Get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return c.httpClient.Do(req)
}

// Post 执行POST请求
func (c *client) Post(ctx context.Context, url string, body interface{}) (*http.Response, error) {
	// TODO: 实现POST请求
	return nil, nil
}
