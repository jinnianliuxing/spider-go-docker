package shared

import "github.com/golang-jwt/jwt/v5"

// UserClaims 用户JWT Claims
type UserClaims struct {
	Uid  int    `json:"user_id"`
	Name string `json:"name"`
	jwt.RegisteredClaims
}

// AdminClaims 管理员JWT Claims
type AdminClaims struct {
	Uid     int    `json:"uid"`
	Name    string `json:"name"`
	IsAdmin bool   `json:"is_admin"`
	jwt.RegisteredClaims
}
