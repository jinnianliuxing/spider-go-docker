package notice

import (
	"context"
	"spider-go/internal/common"
	"time"
)

var (
	ErrEmptyContent = common.NewAppError(common.CodeInvalidParams, "通知内容不能为空")
)

// Service 通知服务接口
type Service interface {
	Create(ctx context.Context, content, noticeType string, isShow, isTop, isHtml bool) (*Notice, error)
	Update(ctx context.Context, nid int, content, noticeType string, isShow, isTop, isHtml bool) (*Notice, error)
	Delete(ctx context.Context, nid int) error
	GetByID(ctx context.Context, nid int) (*Notice, error)
	GetAll(ctx context.Context) ([]*Notice, error)
	GetVisible(ctx context.Context) ([]*Notice, error)

	// Introduction 使用须知相关
	CreateIntroduction(ctx context.Context, content string, isShow, isRedirect bool, link string) (*Introduction, error)
	UpdateIntroduction(ctx context.Context, id int, content string, isShow, isRedirect bool, link string) (*Introduction, error)
	DeleteIntroduction(ctx context.Context, id int) error
	GetIntroductionByID(ctx context.Context, id int) (*Introduction, error)
	GetAllIntroductions(ctx context.Context) ([]*Introduction, error)
	GetVisibleIntroductions(ctx context.Context) ([]*Introduction, error)
}

// service 通知服务实现
type service struct {
	repo Repository
}

// NewService 创建通知服务
func NewService(repo Repository) Service {
	return &service{
		repo: repo,
	}
}

// Create 创建通知
func (s *service) Create(ctx context.Context, content, noticeType string, isShow, isTop, isHtml bool) (*Notice, error) {
	if content == "" {
		return nil, ErrEmptyContent
	}

	notice := &Notice{
		Content:    content,
		NoticeType: noticeType,
		IsShow:     isShow,
		IsTop:      isTop,
		IsHtml:     isHtml,
		CreateTime: time.Now(),
		UpdateTime: time.Now(),
	}

	if err := s.repo.Create(ctx, notice); err != nil {
		return nil, err
	}

	return notice, nil
}

// Update 更新通知
func (s *service) Update(ctx context.Context, nid int, content, noticeType string, isShow, isTop, isHtml bool) (*Notice, error) {
	if content == "" {
		return nil, ErrEmptyContent
	}

	// 获取现有通知
	notice, err := s.repo.FindByID(ctx, nid)
	if err != nil {
		return nil, err
	}

	// 更新字段
	notice.Content = content
	notice.NoticeType = noticeType
	notice.IsShow = isShow
	notice.IsTop = isTop
	notice.IsHtml = isHtml
	notice.UpdateTime = time.Now()

	if err := s.repo.Update(ctx, notice); err != nil {
		return nil, err
	}

	return notice, nil
}

// Delete 删除通知
func (s *service) Delete(ctx context.Context, nid int) error {
	return s.repo.Delete(ctx, nid)
}

// GetByID 根据ID获取通知
func (s *service) GetByID(ctx context.Context, nid int) (*Notice, error) {
	return s.repo.FindByID(ctx, nid)
}

// GetAll 获取所有通知（管理员）
func (s *service) GetAll(ctx context.Context) ([]*Notice, error) {
	return s.repo.FindAll(ctx)
}

// GetVisible 获取可见通知（普通用户）
func (s *service) GetVisible(ctx context.Context) ([]*Notice, error) {
	return s.repo.FindVisible(ctx)
}

// CreateIntroduction 创建使用须知
func (s *service) CreateIntroduction(ctx context.Context, content string, isShow, isRedirect bool, link string) (*Introduction, error) {
	if content == "" {
		return nil, ErrEmptyContent
	}

	intro := &Introduction{
		Content:    content,
		IsShow:     isShow,
		IsRedirect: isRedirect,
		Link:       link,
		CreateTime: time.Now(),
		UpdateTime: time.Now(),
	}

	if err := s.repo.CreateIntroduction(ctx, intro); err != nil {
		return nil, err
	}

	return intro, nil
}

// UpdateIntroduction 更新使用须知
func (s *service) UpdateIntroduction(ctx context.Context, id int, content string, isShow, isRedirect bool, link string) (*Introduction, error) {
	if content == "" {
		return nil, ErrEmptyContent
	}

	// 获取现有使用须知
	intro, err := s.repo.FindIntroductionByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// 更新字段
	intro.Content = content
	intro.IsShow = isShow
	intro.IsRedirect = isRedirect
	intro.Link = link
	intro.UpdateTime = time.Now()

	if err := s.repo.UpdateIntroduction(ctx, intro); err != nil {
		return nil, err
	}

	return intro, nil
}

// DeleteIntroduction 删除使用须知
func (s *service) DeleteIntroduction(ctx context.Context, id int) error {
	return s.repo.DeleteIntroduction(ctx, id)
}

// GetIntroductionByID 根据ID获取使用须知
func (s *service) GetIntroductionByID(ctx context.Context, id int) (*Introduction, error) {
	return s.repo.FindIntroductionByID(ctx, id)
}

// GetAllIntroductions 获取所有使用须知（管理员）
func (s *service) GetAllIntroductions(ctx context.Context) ([]*Introduction, error) {
	return s.repo.FindAllIntroductions(ctx)
}

// GetVisibleIntroductions 获取可见使用须知（普通用户）
func (s *service) GetVisibleIntroductions(ctx context.Context) ([]*Introduction, error) {
	return s.repo.FindVisibleIntroductions(ctx)
}
