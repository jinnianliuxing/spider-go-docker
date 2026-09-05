package middleware

import (
	"spider-go/internal/common"
	"spider-go/internal/service"
	"spider-go/internal/shared"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// AuthMiddleWare JWT 认证中间件（带日活统计）
func AuthMiddleWare(secret []byte, dauService service.DAUService) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.Request.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			common.Error(c, common.CodeUnauthorized, "请提供有效的令牌")
			c.Abort()
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		token, err := jwt.ParseWithClaims(tokenString, &shared.UserClaims{}, func(token *jwt.Token) (any, error) {
			return secret, nil
		})

		if err != nil {
			common.Error(c, common.CodeInvalidToken, "令牌无效或已过期")
			c.Abort()
			return
		}

		claims, ok := token.Claims.(*shared.UserClaims)
		if !ok || !token.Valid {
			common.Error(c, common.CodeInvalidToken, "令牌无效")
			c.Abort()
			return
		}

		// 将用户信息存入上下文
		c.Set("uid", claims.Uid)
		c.Set("username", claims.Name)

		// 记录用户活跃（日活统计）
		// 使用 goroutine 异步记录，不影响请求性能
		go func() {
			_ = dauService.RecordUserActivity(c.Request.Context(), claims.Uid)
		}()

		c.Next()
	}
}
