package admin

import (
	"context"
	"errors"
	"spider-go/internal/common"

	"gorm.io/gorm"
)

var (
	ErrAdminNotFound = common.NewAppError(common.CodeAdminNotFound, "admin not found")
)

// Repository 管理员数据访问接口
type Repository interface {
	Create(ctx context.Context, admin *Admin) error
	FindByID(ctx context.Context, uid int) (*Admin, error)
	FindByEmail(ctx context.Context, email string) (*Admin, error)
	UpdatePassword(ctx context.Context, uid int, password string) error
	UpdateEmail(ctx context.Context, uid int, email string) error
	CheckExists(ctx context.Context) (bool, error)
	DeleteUser(ctx context.Context, uid int) error
}

// adminRepository 管理员数据访问实现
type adminRepository struct {
	db *gorm.DB
}

// NewRepository 创建管理员数据访问层
func NewRepository(db *gorm.DB) Repository {
	return &adminRepository{db: db}
}

// Create 创建管理员
func (r *adminRepository) Create(ctx context.Context, admin *Admin) error {
	return r.db.WithContext(ctx).Create(admin).Error
}

// FindByID 根据ID查找管理员
func (r *adminRepository) FindByID(ctx context.Context, uid int) (*Admin, error) {
	var admin Admin
	if err := r.db.WithContext(ctx).First(&admin, uid).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAdminNotFound
		}
		return nil, err
	}
	return &admin, nil
}

// FindByEmail 根据邮箱查找管理员
func (r *adminRepository) FindByEmail(ctx context.Context, email string) (*Admin, error) {
	var admin Admin
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&admin).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAdminNotFound
		}
		return nil, err
	}
	return &admin, nil
}

// UpdatePassword 更新密码
func (r *adminRepository) UpdatePassword(ctx context.Context, uid int, password string) error {
	return r.db.WithContext(ctx).Model(&Admin{}).Where("uid = ?", uid).Update("password", password).Error
}

// UpdateEmail 更新管理员邮箱
func (r *adminRepository) UpdateEmail(ctx context.Context, uid int, email string) error {
	return r.db.WithContext(ctx).Model(&Admin{}).Where("uid = ?", uid).Update("email", email).Error
}

// CheckExists 检查是否存在管理员
func (r *adminRepository) CheckExists(ctx context.Context) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&Admin{}).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// DeleteUser 删除用户及其所有关联数据
func (r *adminRepository) DeleteUser(ctx context.Context, uid int) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 删除用户绑定的微信信息
		if err := tx.Exec("DELETE FROM user_wechat_mini_program WHERE uid = ?", uid).Error; err != nil {
			return err
		}
		// 删除绑定日志
		if err := tx.Exec("DELETE FROM jwc_bind_logs WHERE uid = ?", uid).Error; err != nil {
			return err
		}
		// 删除成绩数据
		if err := tx.Exec("DELETE FROM grades WHERE uid = ?", uid).Error; err != nil {
			return err
		}
		// 删除平时分
		if err := tx.Exec("DELETE FROM regular_grades WHERE uid = ?", uid).Error; err != nil {
			return err
		}
		// 删除考试安排
		if err := tx.Exec("DELETE FROM exams WHERE uid = ?", uid).Error; err != nil {
			return err
		}
		// 删除等级考试
		if err := tx.Exec("DELETE FROM level_exams WHERE uid = ?", uid).Error; err != nil {
			return err
		}
		// 删除课表
		if err := tx.Exec("DELETE FROM courses WHERE uid = ?", uid).Error; err != nil {
			return err
		}
		// 删除同步日志
		if err := tx.Exec("DELETE FROM sync_logs WHERE uid = ?", uid).Error; err != nil {
			return err
		}
		// 删除同步状态
		if err := tx.Exec("DELETE FROM user_sync_status WHERE uid = ?", uid).Error; err != nil {
			return err
		}
		// 删除GPA排名数据
		if err := tx.Exec("DELETE FROM student_gpas WHERE uid = ?", uid).Error; err != nil {
			return err
		}
		// 删除用户
		if err := tx.Exec("DELETE FROM users WHERE uid = ?", uid).Error; err != nil {
			return err
		}
		return nil
	})
}
