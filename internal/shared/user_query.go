package shared

import (
	"context"
	"errors"
	"spider-go/internal/common"

	"gorm.io/gorm"
)

var (
	ErrUserNotFound = common.NewAppError(common.CodeUserNotFound, "user not found")
)

// UserInfo 用户基本信息（用于跨模块查询）
type UserInfo struct {
	Uid       int    `gorm:"column:uid"`
	Email     string `gorm:"column:email"`
	Name      string `gorm:"column:name"`       // 姓名
	Sid       string `gorm:"column:sid"`        // 学号
	Spwd      string `gorm:"column:spwd"`       // 教务系统密码（扫码/手机号绑定时为空）
	BindMode  string `gorm:"column:bind_mode"`  // 绑定方式 password/qr/phone
	BindPhone string `gorm:"column:bind_phone"` // 手机号绑定的手机号
}

// 绑定方式常量（与 user 模块 model.go 保持一致，此处避免跨包循环依赖故重复定义）
const (
	BindModePassword = "password"
	BindModeQR       = "qr"
	BindModePhone    = "phone"
)

// HasJwcPassword 判断用户是否保存了教务密码（会话过期时能否自动重登）。
//
// 只有密码绑定能自动重登；扫码与手机号绑定都没有密码（spwd 为空 + bind_mode 有值），
// 会话过期必须由用户重新提供凭证。所有"认证失败要不要清绑定"的判断都基于本方法。
func (u *UserInfo) HasJwcPassword() bool {
	if u.Spwd != "" {
		return true
	}
	return u.BindMode == "" // 老数据：bind_mode 未写入，按有密码处理
}

// IsBound 判断是否已绑定教务系统。
//
// 兼容三种绑定方式：
//   - 密码绑定：有学号 + 有密码
//   - 扫码绑定：有学号 + bind_mode=qr（此时 spwd 必然为空）
//   - 手机号绑定：有学号 + bind_mode=phone（此时 spwd 必然为空）
//
// 历史数据 bind_mode 为空但 sid/spwd 都有 → 按密码绑定，行为不变。
//
// ⚠️ 本方法与 user/model.go 的 User.IsBound 是**同一口径的两份实现**
// （避免跨包循环依赖）→ **改这里必须同改那里**。
func (u *UserInfo) IsBound() bool {
	if u.Sid == "" {
		return false
	}
	if u.BindMode == BindModeQR || u.BindMode == BindModePhone {
		return true
	}
	return u.Spwd != ""
}

// CanQuery 判断能否调用教务查询接口（成绩/课表/考试等）。
//
// ⚠️ 业务模块原先统一写 Sid=="" || Spwd==""，扫码绑定（spwd 为空）会被误判成未绑定。
// 统一改用本方法后，只需"已绑定"即可查询。
// 会话是否真的还有效，由查询链路里的 EnsureSessionAlive 判定。
func (u *UserInfo) CanQuery() bool {
	return u.IsBound()
}

// IsNoPasswordUser 判断用户是否属于"没有教务密码"的绑定方式（扫码 / 手机号）。
//
// 这类用户会话过期后**无法自动重登**，因此：
//   - 后台定时同步无法覆盖他们（拿不到密码）；
//   - 任何"认证失败就清绑定"的逻辑都必须跳过他们，否则一次会话过期就会把绑定清掉。
func (u *UserInfo) IsNoPasswordUser() bool {
	return !u.HasJwcPassword()
}

// TableName 指定表名
func (UserInfo) TableName() string {
	return "users"
}

// UserQuery 用户查询接口（用于跨模块访问用户数据）
type UserQuery interface {
	// GetUserByUid 根据UID获取用户信息
	GetUserByUid(ctx context.Context, uid int) (*UserInfo, error)
	// GetAllUserEmails 获取所有用户的邮箱
	GetAllUserEmails(ctx context.Context) ([]string, error)
	// GetAllBoundUsers 获取所有已绑定教务系统的用户
	GetAllBoundUsers(ctx context.Context) ([]UserInfo, error)
	// ClearJwcBinding 清除用户教务系统密码（保留学号）
	ClearJwcBinding(ctx context.Context, uid int) error
	// GetAllUsers 获取所有用户（管理员用）
	GetAllUsers(ctx context.Context) ([]UserInfo, error)
}

// userQuery 用户查询实现
type userQuery struct {
	db *gorm.DB
}

// NewUserQuery 创建用户查询服务
func NewUserQuery(db *gorm.DB) UserQuery {
	return &userQuery{db: db}
}

// GetUserByUid 根据UID获取用户信息
func (q *userQuery) GetUserByUid(ctx context.Context, uid int) (*UserInfo, error) {
	var user UserInfo
	if err := q.db.WithContext(ctx).First(&user, uid).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

// GetAllUserEmails 获取所有用户的邮箱
func (q *userQuery) GetAllUserEmails(ctx context.Context) ([]string, error) {
	var emails []string
	err := q.db.WithContext(ctx).Model(&UserInfo{}).Pluck("email", &emails).Error
	if err != nil {
		return nil, err
	}
	return emails, nil
}

// GetAllBoundUsers 获取所有已绑定教务系统的用户。
//
// 兼容扫码/手机号绑定用户（无 spwd）—— 但注意：后台定时同步需要密码才能自动重登，
// 无密码用户无法参与自动同步（会话过期后需用户手动重新扫码/补验证码），
// 所以调用方若依赖密码，还应自行过滤（见 UserInfo.IsNoPasswordUser）。
func (q *userQuery) GetAllBoundUsers(ctx context.Context) ([]UserInfo, error) {
	var users []UserInfo
	err := q.db.WithContext(ctx).
		Where("sid != ? AND sid IS NOT NULL", "").
		Where("(spwd != ? AND spwd IS NOT NULL) OR bind_mode IN ?", "", []string{BindModeQR, BindModePhone}).
		Find(&users).Error
	if err != nil {
		return nil, err
	}
	return users, nil
}

// ClearJwcBinding 清除用户教务系统密码（保留学号，防止数据污染）
// 学号一旦绑定不可更换，认证失败时只清除密码，用户重新输入密码即可恢复
func (q *userQuery) ClearJwcBinding(ctx context.Context, uid int) error {
	return q.db.WithContext(ctx).Model(&UserInfo{}).
		Where("uid = ?", uid).
		Updates(map[string]interface{}{
			"spwd": "",
		}).Error
}

// GetAllUsers 获取所有用户
func (q *userQuery) GetAllUsers(ctx context.Context) ([]UserInfo, error) {
	var users []UserInfo
	err := q.db.WithContext(ctx).Order("uid ASC").Find(&users).Error
	if err != nil {
		return nil, err
	}
	return users, nil
}
