package user

import "time"

// User 用户模型
type User struct {
	Uid                    int                   `gorm:"primary_key;AUTO_INCREMENT" json:"uid"`
	Email                  string                `gorm:"type:varchar(255);unique;index" json:"email"`
	Name                   string                `gorm:"type:varchar(255)" json:"name"`
	Password               string                `gorm:"type:varchar(255)" json:"-"`  // 不序列化
	Sid                    string                `gorm:"type:varchar(50)" json:"sid"` // 学号
	Spwd                   string                `gorm:"type:varchar(255)" json:"-"`  // 教务系统密码（不序列化）
	CreatedAt              time.Time             `json:"created_at"`
	Avatar                 string                `gorm:"type:varchar(500)" json:"avatar"`
	WeChatMiniProgramBinds UserWeChatMiniProgram `gorm:"foreignKey:Uid" json:"-"` // HasMany 关系

	// 绑定频率控制
	BindCountCurrentMonth int        `gorm:"type:tinyint;default:0;comment:本月绑定次数" json:"-"`
	BindMonth             string     `gorm:"type:varchar(7);comment:绑定计数月份(YYYY-MM);index" json:"-"`
	LastBindAt            *time.Time `gorm:"comment:最后一次绑定时间" json:"-"`
	TotalBindCount        int        `gorm:"type:int;default:0;comment:累计绑定次数" json:"-"`
}

// TableName 指定表名
func (*User) TableName() string {
	return "users"
}

// UserWeChatMiniProgram 微信登录表模型
type UserWeChatMiniProgram struct {
	Id          int       `gorm:"primary_key;AUTO_INCREMENT" json:"id"`
	Uid         int       `gorm:"type:bigint;not null;uniqueIndex" json:"uid"`
	PhoneNumber string    `gorm:"size:20" json:"phone_number"`
	AppID       string    `gorm:"size:32;not null" json:"appid"`
	OpenID      string    `gorm:"size:64;not null" json:"openid"`
	UnionID     string    `gorm:"size:64" json:"unionid,omitempty"`
	LastLogin   time.Time `json:"last_login"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (*UserWeChatMiniProgram) TableName() string {
	return "user_wechat_mini_program"
}

// RegisterRequest 用户注册请求
type RegisterRequest struct {
	Name     string `json:"name" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Captcha  string `json:"captcha" binding:"required"`
}

// LoginRequest 用户登录请求
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// BindJwcRequest 绑定教务系统请求
type BindJwcRequest struct {
	Sid  string `json:"sid"`                     // 学号（不传则自动填充已绑定的学号）
	Spwd string `json:"spwd" binding:"required"` // 教务系统密码
}

// ResetPasswordRequest 重置密码请求
type ResetPasswordRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Captcha  string `json:"captcha" binding:"required"`
}

// SendEmailCaptchaRequest 发送邮箱验证码请求
type SendEmailCaptchaRequest struct {
	Email string `json:"email" binding:"required,email"`
}

// WeChatLoginRequest 微信登录请求(自动注册账号)
type WeChatLoginRequest struct {
	Code string `json:"code" binding:"required"` // 微信授权code
}

// UpdateNameRequest 更新用户名请求
type UpdateNameRequest struct {
	Name string `json:"name" binding:"required"`
}

// UpdateEmailRequest 更新邮箱请求
type UpdateEmailRequest struct {
	Email   string `json:"email" binding:"required,email"`
	Captcha string `json:"captcha" binding:"required"`
}

// UserResponse 用户响应（不包含敏感信息）
type UserResponse struct {
	Uid       int       `json:"uid"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Sid       string    `json:"sid"`
	Avatar    string    `json:"avatar"`
	CreatedAt time.Time `json:"created_at"`
	IsBind    bool      `json:"is_bind"` // 是否绑定教务系统
}

// ToResponse 转换为响应格式
func (u *User) ToResponse() *UserResponse {
	return &UserResponse{
		Uid:       u.Uid,
		Email:     u.Email,
		Name:      u.Name,
		Sid:       u.Sid,
		Avatar:    u.Avatar,
		CreatedAt: u.CreatedAt,
		IsBind:    u.Sid != "" && u.Spwd != "",
	}
}

// LoginResponse 登录响应
type LoginResponse struct {
	Token string        `json:"token"`
	User  *UserResponse `json:"user"`
}

// BindStatusResponse 绑定状态响应
type BindStatusResponse struct {
	IsBound        bool       `json:"is_bound"`         // 是否已绑定
	CurrentSid     string     `json:"current_sid"`      // 当前学号
	TotalBindCount int        `json:"total_bind_count"` // 累计绑定次数
	LastBindAt     *time.Time `json:"last_bind_at"`     // 最后绑定时间
	CanChangeSid   bool       `json:"can_change_sid"`   // 是否可以更换学号（只有未绑定时为true）
}

// JwcBindLog 教务系统绑定日志
type JwcBindLog struct {
	ID         int64     `gorm:"primary_key;AUTO_INCREMENT" json:"id"`
	Uid        int       `gorm:"type:int;not null;index:idx_uid_created" json:"uid"`
	OldSid     string    `gorm:"type:varchar(50);comment:原学号" json:"old_sid"`
	NewSid     string    `gorm:"type:varchar(50);not null;comment:新学号" json:"new_sid"`
	BindStatus int       `gorm:"type:tinyint;not null;index;comment:绑定状态(1成功 2失败-账号错误 3失败-超过限制 4失败-其他)" json:"bind_status"`
	ErrorMsg   string    `gorm:"type:varchar(500);comment:错误信息" json:"error_msg"`
	IpAddress  string    `gorm:"type:varchar(45);comment:IP地址" json:"ip_address"`
	UserAgent  string    `gorm:"type:varchar(500);comment:User-Agent" json:"user_agent"`
	CreatedAt  time.Time `gorm:"index:idx_uid_created;index:idx_created_at" json:"created_at"`
}

// TableName 指定表名
func (*JwcBindLog) TableName() string {
	return "jwc_bind_logs"
}

// 绑定状态常量
const (
	BindStatusSuccess     = 1 // 绑定成功
	BindStatusFailedAuth  = 2 // 失败-账号错误
	BindStatusFailedLimit = 3 // 失败-超过限制
	BindStatusFailedOther = 4 // 失败-其他原因
	BindStatusUnbind      = 5 // 用户主动注销
)
