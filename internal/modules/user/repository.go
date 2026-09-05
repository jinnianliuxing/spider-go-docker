package user

import (
	"context"
	"errors"
	"spider-go/internal/common"
	"time"

	"gorm.io/gorm"
)

var (
	ErrUserNotFound       = common.NewAppError(common.CodeUserNotFound, "user not found")
	ErrWeChatBindNotFound = common.NewAppError(common.CodeWeChatBindNotFound, "wechat bind not found")
)

// Repository 用户数据访问接口
type Repository interface {
	Create(ctx context.Context, user *User) error
	FindByID(ctx context.Context, uid int) (*User, error)
	FindByEmail(ctx context.Context, email string) (*User, error)
	Update(ctx context.Context, user *User) error
	UpdatePassword(ctx context.Context, uid int, password string) error
	UpdateJwc(ctx context.Context, uid int, sid, spwd string) error
	Delete(ctx context.Context, uid int) error

	// 微信相关
	FindByWeChatOpenID(ctx context.Context, appID, openID string) (*User, error)
	CreateWeChatBind(ctx context.Context, bind *UserWeChatMiniProgram) error
	FindWeChatBindByUID(ctx context.Context, uid int, appID string) (*UserWeChatMiniProgram, error)
	UpdateWeChatBind(ctx context.Context, bind *UserWeChatMiniProgram) error

	// 统计相关
	CountUsers(ctx context.Context) (int64, error)
	CountNewUsersByDateRange(ctx context.Context, startDate, endDate time.Time) (int64, error)
	CountNewUsersByDate(ctx context.Context, date time.Time) (int64, error)

	// 批量查询相关
	FindAllBoundUsers(ctx context.Context) ([]*User, error)
	ClearJwcBinding(ctx context.Context, uid int) error

	// 注销相关
	FullClearJwcBinding(ctx context.Context, uid int) error
	SoftDeleteAllUserData(ctx context.Context, uid int) error
	GetLastUnbindTime(ctx context.Context, uid int) (*time.Time, error)
}

// repository 用户数据访问实现
type repository struct {
	db *gorm.DB
}

// NewRepository 创建用户数据访问层
func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

// Create 创建用户
func (r *repository) Create(ctx context.Context, user *User) error {
	return r.db.WithContext(ctx).Create(user).Error
}

// FindByID 根据ID查找用户
func (r *repository) FindByID(ctx context.Context, uid int) (*User, error) {
	var user User
	if err := r.db.WithContext(ctx).First(&user, uid).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

// FindByEmail 根据邮箱查找用户
func (r *repository) FindByEmail(ctx context.Context, email string) (*User, error) {
	var user User
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

// Update 更新用户
func (r *repository) Update(ctx context.Context, user *User) error {
	return r.db.WithContext(ctx).Save(user).Error
}

// UpdatePassword 更新密码
func (r *repository) UpdatePassword(ctx context.Context, uid int, password string) error {
	return r.db.WithContext(ctx).Model(&User{}).Where("uid = ?", uid).Update("password", password).Error
}

// UpdateJwc 更新教务系统绑定
func (r *repository) UpdateJwc(ctx context.Context, uid int, sid, spwd string) error {
	return r.db.WithContext(ctx).Model(&User{}).Where("uid = ?", uid).Updates(map[string]interface{}{
		"sid":  sid,
		"spwd": spwd,
	}).Error
}

// Delete 删除用户
func (r *repository) Delete(ctx context.Context, uid int) error {
	return r.db.WithContext(ctx).Delete(&User{}, uid).Error
}

// FindByWeChatOpenID 根据微信OpenID查找用户
func (r *repository) FindByWeChatOpenID(ctx context.Context, appID, openID string) (*User, error) {
	var bind UserWeChatMiniProgram
	if err := r.db.WithContext(ctx).Where("app_id = ? AND open_id = ?", appID, openID).First(&bind).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	// 根据uid查找用户
	return r.FindByID(ctx, bind.Uid)
}

// CreateWeChatBind 创建微信绑定
func (r *repository) CreateWeChatBind(ctx context.Context, bind *UserWeChatMiniProgram) error {
	return r.db.WithContext(ctx).Create(bind).Error
}

// FindWeChatBindByUID 根据用户ID和AppID查找微信绑定
func (r *repository) FindWeChatBindByUID(ctx context.Context, uid int, appID string) (*UserWeChatMiniProgram, error) {
	var bind UserWeChatMiniProgram
	if err := r.db.WithContext(ctx).Where("uid = ? AND app_id = ?", uid, appID).First(&bind).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWeChatBindNotFound
		}
		return nil, err
	}
	return &bind, nil
}

// UpdateWeChatBind 更新微信绑定
func (r *repository) UpdateWeChatBind(ctx context.Context, bind *UserWeChatMiniProgram) error {
	return r.db.WithContext(ctx).Save(bind).Error
}

// CountUsers 统计用户总数
func (r *repository) CountUsers(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&User{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// CountNewUsersByDateRange 统计指定日期范围内的新增用户数
func (r *repository) CountNewUsersByDateRange(ctx context.Context, startDate, endDate time.Time) (int64, error) {
	var count int64
	// 设置时间范围：startDate 00:00:00 到 endDate 23:59:59
	startOfDay := time.Date(startDate.Year(), startDate.Month(), startDate.Day(), 0, 0, 0, 0, startDate.Location())
	endOfDay := time.Date(endDate.Year(), endDate.Month(), endDate.Day(), 23, 59, 59, 999999999, endDate.Location())

	if err := r.db.WithContext(ctx).Model(&User{}).
		Where("created_at >= ? AND created_at <= ?", startOfDay, endOfDay).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// CountNewUsersByDate 统计指定日期的新增用户数
func (r *repository) CountNewUsersByDate(ctx context.Context, date time.Time) (int64, error) {
	var count int64
	// 设置时间范围：当天 00:00:00 到 23:59:59
	startOfDay := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	endOfDay := time.Date(date.Year(), date.Month(), date.Day(), 23, 59, 59, 999999999, date.Location())

	if err := r.db.WithContext(ctx).Model(&User{}).
		Where("created_at >= ? AND created_at <= ?", startOfDay, endOfDay).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// FindAllBoundUsers 查找所有已绑定教务系统的用户
func (r *repository) FindAllBoundUsers(ctx context.Context) ([]*User, error) {
	var users []*User
	if err := r.db.WithContext(ctx).
		Where("sid != '' AND sid IS NOT NULL AND spwd != '' AND spwd IS NOT NULL").
		Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

// ClearJwcBinding 清除用户教务系统密码（保留学号，防止数据污染）
func (r *repository) ClearJwcBinding(ctx context.Context, uid int) error {
	return r.db.WithContext(ctx).Model(&User{}).Where("uid = ?", uid).Updates(map[string]interface{}{
		"spwd": "",
	}).Error
}

// FullClearJwcBinding 完全清除教务系统绑定信息（sid + spwd + 绑定计数），用于用户主动注销
func (r *repository) FullClearJwcBinding(ctx context.Context, uid int) error {
	return r.db.WithContext(ctx).Model(&User{}).Where("uid = ?", uid).Updates(map[string]interface{}{
		"sid":                      "",
		"spwd":                     "",
		"bind_count_current_month": 0,
		"bind_month":               "",
		"total_bind_count":         0,
	}).Error
}

// SoftDeleteAllUserData 软删除用户所有同步数据（成绩、平时分、考试、等级考试、课表）
func (r *repository) SoftDeleteAllUserData(ctx context.Context, uid int) error {
	tables := []string{"grades", "regular_grades", "exams", "level_exams", "courses"}
	for _, table := range tables {
		if err := r.db.WithContext(ctx).
			Table(table).
			Where("uid = ?", uid).
			Update("is_deleted", true).Error; err != nil {
			return err
		}
	}
	// 重置用户同步状态
	return r.db.WithContext(ctx).
		Table("user_sync_status").
		Where("uid = ?", uid).
		Updates(map[string]interface{}{
			"grade_last_sync_at":         nil,
			"grade_sync_version":         0,
			"regular_grade_last_sync_at": nil,
			"regular_grade_sync_version": 0,
			"exam_last_sync_at":          nil,
			"exam_sync_version":          0,
			"level_exam_last_sync_at":    nil,
			"level_exam_sync_version":    0,
			"course_last_sync_at":        nil,
			"course_sync_version":        0,
		}).Error
}

// GetLastUnbindTime 获取用户最近一次注销操作的时间
func (r *repository) GetLastUnbindTime(ctx context.Context, uid int) (*time.Time, error) {
	var log JwcBindLog
	err := r.db.WithContext(ctx).
		Where("uid = ? AND bind_status = ?", uid, BindStatusUnbind).
		Order("created_at DESC").
		First(&log).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &log.CreatedAt, nil
}
