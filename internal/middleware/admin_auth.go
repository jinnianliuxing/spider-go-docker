package middleware

import (
	"fmt"
	"spider-go/internal/common"
	"spider-go/internal/shared"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// AdminAuthMiddleware 管理员认证中间件
func AdminAuthMiddleware(secret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.Request.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			common.Error(c, common.CodeUnauthorized, "请提供有效的令牌")
			c.Abort()
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		token, err := jwt.ParseWithClaims(tokenString, &shared.AdminClaims{}, func(token *jwt.Token) (any, error) {
			return secret, nil
		})

		if err != nil {
			common.Error(c, common.CodeInvalidToken, "令牌无效或已过期")
			c.Abort()
			return
		}

		claims, ok := token.Claims.(*shared.AdminClaims)
		if !ok || !token.Valid {
			common.Error(c, common.CodeInvalidToken, "令牌无效")
			c.Abort()
			return
		}

		// 验证是否为管理员
		if !claims.IsAdmin {
			// 添加调试日志
			c.Error(fmt.Errorf("[AdminAuth] IsAdmin=false, uid=%d, name=%s", claims.Uid, claims.Name))
			common.Error(c, common.CodeForbidden, "需要管理员权限")
			c.Abort()
			return
		}

		// 将管理员信息存入上下文
		c.Set("aid", claims.Uid)
		c.Set("admin_name", claims.Name)
		c.Next()
	}
}
