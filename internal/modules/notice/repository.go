package notice

import (
	"context"
	"errors"
	"spider-go/internal/common"

	"gorm.io/gorm"
)

var (
	ErrNoticeNotFound       = common.NewAppError(common.CodeNoticeNotFound, "notice not found")
	ErrIntroductionNotFound = common.NewAppError(common.CodeNotFound, "introduction not found")
)

// Repository 通知数据访问接口
type Repository interface {
	Create(ctx context.Context, notice *Notice) error
	Update(ctx context.Context, notice *Notice) error
	Delete(ctx context.Context, nid int) error
	FindByID(ctx context.Context, nid int) (*Notice, error)
	FindAll(ctx context.Context) ([]*Notice, error)
	FindVisible(ctx context.Context) ([]*Notice, error)

	// Introduction 使用须知相关
	CreateIntroduction(ctx context.Context, intro *Introduction) error
	UpdateIntroduction(ctx context.Context, intro *Introduction) error
	DeleteIntroduction(ctx context.Context, id int) error
	FindIntroductionByID(ctx context.Context, id int) (*Introduction, error)
	FindAllIntroductions(ctx context.Context) ([]*Introduction, error)
	FindVisibleIntroductions(ctx context.Context) ([]*Introduction, error)
}

// repository 通知数据访问实现
type repository struct {
	db *gorm.DB
}

// NewRepository 创建通知数据访问层
func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

// Create 创建通知
func (r *repository) Create(ctx context.Context, notice *Notice) error {
	return r.db.WithContext(ctx).Create(notice).Error
}

// Update 更新通知
func (r *repository) Update(ctx context.Context, notice *Notice) error {
	return r.db.WithContext(ctx).Save(notice).Error
}

// Delete 删除通知
func (r *repository) Delete(ctx context.Context, nid int) error {
	return r.db.WithContext(ctx).Delete(&Notice{}, nid).Error
}

// FindByID 根据ID查找通知
func (r *repository) FindByID(ctx context.Context, nid int) (*Notice, error) {
	var notice Notice
	if err := r.db.WithContext(ctx).First(&notice, nid).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNoticeNotFound
		}
		return nil, err
	}
	return &notice, nil
}

// FindAll 获取所有通知（管理员）
func (r *repository) FindAll(ctx context.Context) ([]*Notice, error) {
	var notices []*Notice
	err := r.db.WithContext(ctx).
		Order("is_top DESC, create_time DESC").
		Find(&notices).Error
	return notices, err
}

// FindVisible 获取可见通知（普通用户）
func (r *repository) FindVisible(ctx context.Context) ([]*Notice, error) {
	var notices []*Notice
	err := r.db.WithContext(ctx).
		Where("is_show = ?", true).
		Order("is_top DESC, create_time DESC").
		Find(&notices).Error
	return notices, err
}

// CreateIntroduction 创建使用须知
func (r *repository) CreateIntroduction(ctx context.Context, intro *Introduction) error {
	return r.db.WithContext(ctx).Create(intro).Error
}

// UpdateIntroduction 更新使用须知
func (r *repository) UpdateIntroduction(ctx context.Context, intro *Introduction) error {
	return r.db.WithContext(ctx).Save(intro).Error
}

// DeleteIntroduction 删除使用须知
func (r *repository) DeleteIntroduction(ctx context.Context, id int) error {
	return r.db.WithContext(ctx).Delete(&Introduction{}, id).Error
}

// FindIntroductionByID 根据ID查找使用须知
func (r *repository) FindIntroductionByID(ctx context.Context, id int) (*Introduction, error) {
	var intro Introduction
	if err := r.db.WithContext(ctx).First(&intro, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrIntroductionNotFound
		}
		return nil, err
	}
	return &intro, nil
}

// FindAllIntroductions 获取所有使用须知（管理员）
func (r *repository) FindAllIntroductions(ctx context.Context) ([]*Introduction, error) {
	var intros []*Introduction
	err := r.db.WithContext(ctx).
		Order("create_time DESC").
		Find(&intros).Error
	return intros, err
}

// FindVisibleIntroductions 获取可见使用须知（普通用户）
func (r *repository) FindVisibleIntroductions(ctx context.Context) ([]*Introduction, error) {
	var intros []*Introduction
	err := r.db.WithContext(ctx).
		Where("is_show = ?", true).
		Order("create_time DESC").
		Find(&intros).Error
	return intros, err
}
