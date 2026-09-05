package admin

import "time"

// Admin 管理员模型
type Admin struct {
	Uid       int       `gorm:"primaryKey;autoIncrement" json:"uid"`
	Email     string    `gorm:"type:varchar(255);unique;not null" json:"email"`
	Name      string    `gorm:"type:varchar(255)" json:"name"`
	Password  string    `gorm:"type:varchar(255)" json:"-"` // 不序列化
	CreatedAt time.Time `json:"created_at"`
	Avatar    string    `gorm:"type:varchar(500)" json:"avatar"`
}

// TableName 指定表名
func (*Admin) TableName() string {
	return "administrators"
}

// LoginRequest 管理员登录请求
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// ChangePwdRequest 修改密码请求
type ChangePwdRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
}

// BroadcastEmailRequest 群发邮件请求
type BroadcastEmailRequest struct {
	Subject string `json:"subject" binding:"required"`
	Content string `json:"content" binding:"required"`
}

// AdminResponse 管理员响应（不包含敏感信息）
type AdminResponse struct {
	Uid       int       `json:"uid"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Avatar    string    `json:"avatar"`
	CreatedAt time.Time `json:"created_at"`
}

// ToResponse 转换为响应格式
func (a *Admin) ToResponse() *AdminResponse {
	return &AdminResponse{
		Uid:       a.Uid,
		Email:     a.Email,
		Name:      a.Name,
		Avatar:    a.Avatar,
		CreatedAt: a.CreatedAt,
	}
}

// LoginResponse 登录响应
type LoginResponse struct {
	Token string         `json:"token"`
	Admin *AdminResponse `json:"admin"`
}

// UserWithBindStatus 用户及绑定状态（管理员查看用）
type UserWithBindStatus struct {
	Uid        int        `json:"uid"`
	Email      string     `json:"email"`
	Name       string     `json:"name"`
	Sid        string     `json:"sid"`
	Avatar     string     `json:"avatar"`
	CreatedAt  time.Time  `json:"created_at"`
	IsBind     bool       `json:"is_bind"`
	LastBindAt *time.Time `json:"last_bind_at"`
	BindCount  int        `json:"bind_count"`
}

// DeleteUserRequest 删除用户请求
type DeleteUserRequest struct {
	UID int `json:"uid" binding:"required"`
}

// BroadcastEmailResponse 群发邮件响应
type BroadcastEmailResponse struct {
	SuccessCount int `json:"success_count"`
	FailCount    int `json:"fail_count"`
	TotalCount   int `json:"total_count"`
}
