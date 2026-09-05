package middleware

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// NewCORSMiddleware 创建 CORS 中间件
func NewCORSMiddleware(
	allowOrigins []string,
	allowMethods []string,
	allowHeaders []string,
	exposeHeaders []string,
	allowCredentials bool,
	maxAge int,
) gin.HandlerFunc {
	config := &corsConfig{
		AllowOrigins:     allowOrigins,
		AllowMethods:     allowMethods,
		AllowHeaders:     allowHeaders,
		ExposeHeaders:    exposeHeaders,
		AllowCredentials: allowCredentials,
		MaxAge:           maxAge,
	}

	return createCORSHandler(config)
}

// corsConfig CORS 配置
type corsConfig struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	ExposeHeaders    []string
	AllowCredentials bool
	MaxAge           int
}

// createCORSHandler 创建 CORS 处理函数
func createCORSHandler(config *corsConfig) gin.HandlerFunc {
	// 预处理配置数据
	allowMethodsStr := strings.Join(config.AllowMethods, ", ")
	allowHeadersStr := strings.Join(config.AllowHeaders, ", ")
	exposeHeadersStr := strings.Join(config.ExposeHeaders, ", ")
	maxAgeStr := strconv.Itoa(config.MaxAge)
	allowCredentials := "false"
	if config.AllowCredentials {
		allowCredentials = "true"
	}

	return func(ctx *gin.Context) {
		// 获取请求来源
		origin := ctx.Request.Header.Get("Origin")

		// 检查来源是否允许
		allowed := false
		for _, allowedOrigin := range config.AllowOrigins {
			if origin == allowedOrigin {
				allowed = true
				break
			}
		}

		// 设置 CORS 响应头
		if allowed {
			ctx.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			ctx.Writer.Header().Set("Access-Control-Allow-Credentials", allowCredentials)

			// 如果不是预检请求，也设置 Expose-Headers
			if ctx.Request.Method != "OPTIONS" && exposeHeadersStr != "" {
				ctx.Writer.Header().Set("Access-Control-Expose-Headers", exposeHeadersStr)
			}
		}

		// 处理预检请求 (OPTIONS)
		if ctx.Request.Method == "OPTIONS" {
			if allowed {
				ctx.Writer.Header().Set("Access-Control-Allow-Methods", allowMethodsStr)
				ctx.Writer.Header().Set("Access-Control-Allow-Headers", allowHeadersStr)
				ctx.Writer.Header().Set("Access-Control-Max-Age", maxAgeStr)
			}
			ctx.AbortWithStatus(204)
			return
		}

		// 继续处理请求
		ctx.Next()
	}
}
