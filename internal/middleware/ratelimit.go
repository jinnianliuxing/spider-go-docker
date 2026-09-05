package middleware

import (
	"context"
	"fmt"
	"spider-go/internal/common"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// RateLimitConfig 频率限制配置
type RateLimitConfig struct {
	Window      time.Duration // 窗口大小
	MaxRequests int           // 窗口内最大请求次数
	IsLogin     bool          // 是否限制登录接口
}

// RateLimitMiddleware 基于 Redis 的滑动窗口频率限制中间件
func RateLimitMiddleware(redisClient *redis.Client, config RateLimitConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取客户端标识：优先用邮箱，其次用 IP
		key := "ratelimit:ip:" + c.ClientIP() + ":" + c.FullPath()
		if config.IsLogin {
			var body struct {
				Email string `json:"email"`
			}
			if err := c.ShouldBindJSON(&body); err == nil && body.Email != "" {
				key = "ratelimit:email:" + body.Email + ":login"
			}
		}

		ctx := context.Background()
		now := time.Now().UnixMilli()
		windowStart := now - config.Window.Milliseconds()

		pipe := redisClient.Pipeline()
		pipe.ZRemRangeByScore(ctx, key, "0", strconv.FormatInt(windowStart, 10))
		countCmd := pipe.ZCard(ctx, key)
		pipe.ZAdd(ctx, key, redis.Z{Score: float64(now), Member: now})
		pipe.Expire(ctx, key, config.Window)

		if _, err := pipe.Exec(ctx); err != nil {
			c.Next()
			return
		}

		count, _ := countCmd.Result()
		if count > int64(config.MaxRequests) {
			seconds := int(config.Window.Seconds())
			if config.IsLogin {
				common.Error(c, common.CodeInvalidParams, fmt.Sprintf("登录尝试过于频繁，请%d秒后再试", seconds))
			} else {
				common.Error(c, common.CodeInvalidParams, "请求过于频繁，请稍后再试")
			}
			c.Abort()
			return
		}

		c.Next()
	}
}
