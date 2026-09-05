package redis

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
)

// Config Redis 配置
type Config struct {
	Host string
	Port int
	Pass string
	DB   int
}

// NewClient 创建 Redis 客户端
func NewClient(config Config) (*redis.Client, error) {
	// 如果 Host 已经包含端口，直接使用；否则添加端口
	addr := config.Host
	if config.Port != 0 && !strings.Contains(addr, ":") {
		addr = addr + ":" + strconv.Itoa(config.Port)
	}

	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: config.Pass,
		DB:       config.DB,
	})

	// 测试连接
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	return client, nil
}

// MustNewClient 创建 Redis 客户端，失败则 panic
func MustNewClient(config Config) *redis.Client {
	client, err := NewClient(config)
	if err != nil {
		panic(err)
	}
	return client
}
