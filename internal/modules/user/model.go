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

	// BindMode 绑定方式："" 或 "password"（密码绑定） / "qr"（扫码绑定） / "phone"（手机号验证码绑定）。
	// 扫码与手机号绑定都拿不到教务密码，spwd 为空，因此不能再靠 spwd 是否为空判断"是否已绑定"。
	// 历史数据此列为空，按密码绑定处理（见 IsBound）。
	BindMode string `gorm:"type:varchar(16);default:'';comment:绑定方式 password/qr/phone" json:"bind_mode"`

	// BindPhone 手机号绑定 / 手机号验证码登录所用的手机号（脱敏后展示给前端，用于回填）。
	// 仅手机号绑定方式下有值；密码/扫码绑定为空。
	BindPhone string `gorm:"type:varchar(20);comment:手机号绑定的手机号" json:"bind_phone"`

	// 绑定频率控制
	BindCountCurrentMonth int        `gorm:"type:tinyint;default:0;comment:本月绑定次数" json:"-"`
	BindMonth             string     `gorm:"type:varchar(7);comment:绑定计数月份(YYYY-MM);index" json:"-"`
	LastBindAt            *time.Time `gorm:"comment:最后一次绑定时间" json:"-"`
	TotalBindCount        int        `gorm:"type:int;default:0;comment:累计绑定次数" json:"-"`
}

// 绑定方式常量
const (
	// BindModePassword 密码绑定（有 spwd，会话过期可自动重登）
	BindModePassword = "password"
	// BindModeQR 扫码绑定（无 spwd，会话过期需重新扫码）
	BindModeQR = "qr"
	// BindModePhone 手机号验证码绑定（无 spwd，会话过期需补一次短信验证码）
	BindModePhone = "phone"
)

// HasJwcPassword 判断用户是否保存了教务密码（即会话过期时能否自动重登）。
//
// 只有密码绑定能自动重登；扫码与手机号绑定都没有密码，
// 会话过期后必须由用户重新提供凭证（扫码 / 补验证码）。
// ⚠️ 所有"认证失败要不要清绑定"的判断都应基于本方法，而不是直接看 Spwd ——
// 历史数据 bind_mode 为空但有 spwd，同样算有密码。
func (u *User) HasJwcPassword() bool {
	if u.Spwd != "" {
		return true
	}
	return u.BindMode == "" // 老数据：bind_mode 未写入，按有密码处理
}

// IsNoPasswordUser 判断用户是否属于"没有教务密码"的绑定方式（扫码 / 手机号）。
//
// 这类用户会话过期后**无法自动重登**，因此后台定时同步、认证失败清绑定等
// 逻辑都必须跳过他们（见 shared/user_query.go 的同名方法）。
func (u *User) IsNoPasswordUser() bool {
	return !u.HasJwcPassword()
}

// IsBound 判断用户是否已绑定教务系统。
//
// 兼容三种绑定方式：
//   - 密码绑定：有学号 + 有密码
//   - 扫码绑定：有学号 + BindMode=qr（此时 spwd 必然为空）
//   - 手机号绑定：有学号 + BindMode=phone（此时 spwd 必然为空）
//
// 历史数据 BindMode 为空但 sid/spwd 都有 → 按密码绑定，行为不变。
//
// ⚠️ 本方法与 shared/user_query.go 的 UserInfo.IsBound 是**同一口径的两份实现**
// （避免跨包循环依赖）→ **改这里必须同改那里**。
func (u *User) IsBound() bool {
	if u.Sid == "" {
		return false
	}
	if u.BindMode == BindModeQR || u.BindMode == BindModePhone {
		return true
	}
	return u.Spwd != ""
}

// CanQuery 判断用户能否调用教务查询接口（成绩/课表/考试等）。
//
// 业务模块原先统一写 Sid=="" || Spwd==""，这在扫码绑定下会误判成"未绑定"
// （扫码拿不到密码，spwd 必然为空）。统一改用本方法，只要求"已绑定"。
//
// ⚠️ 注意：CanQuery 只说明"有资格查"，不代表"现在一定能查出"。
// 会话可能已过期，此时查询链路会经 EnsureSessionAlive 判定后
// 返回 CodeJwcBindExpired，前端据此引导用户（扫码用户重新扫码 / 密码用户重输密码）。
func (u *User) CanQuery() bool {
	return u.IsBound()
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

// BindQrSessionRequest 扫码绑定-轮询/完成请求
type BindQrSessionRequest struct {
	SessionID string `json:"session_id" binding:"required"`
}

// BindPhoneStartRequest 手机号验证码绑定-发送验证码请求
// Phone 不传时后端自动使用库里已保存的绑定手机号（会话过期后"零输入"重发验证码）
type BindPhoneStartRequest struct {
	Phone string `json:"phone"`
}

// BindPhoneCompleteRequest 手机号验证码绑定-提交验证码请求
type BindPhoneCompleteRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Code      string `json:"code" binding:"required"`
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
	IsBind    bool      `json:"is_bind"`   // 是否绑定教务系统
	BindMode  string    `json:"bind_mode"` // 绑定方式：password / qr / phone（前端据此决定"更新密码"、"重新扫码"还是"补验证码"）
	// BindPhone 手机号绑定的手机号（仅绑定时有值）。前端据此回填输入框，
	// 会话过期时用户只要点「发送验证码」再补一次码即可恢复，不必重新输手机号。
	BindPhone string `json:"bind_phone"`
	// HasPassword 是否保存了教务密码（即会话过期能否自动重登）。
	// 扫码/手机号绑定为 false，前端据此把"重新输入密码"引导改为对应方式。
	HasPassword bool `json:"has_password"`
}

// ToResponse 转换为响应格式
func (u *User) ToResponse() *UserResponse {
	mode := u.BindMode
	if mode == "" && u.Spwd != "" {
		mode = BindModePassword
	}
	return &UserResponse{
		Uid:         u.Uid,
		Email:       u.Email,
		Name:        u.Name,
		Sid:         u.Sid,
		Avatar:      u.Avatar,
		CreatedAt:   u.CreatedAt,
		IsBind:      u.IsBound(),
		BindMode:    mode,
		BindPhone:   u.BindPhone,
		HasPassword: u.Spwd != "",
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
