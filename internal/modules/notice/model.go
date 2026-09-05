package notice

import "time"

// Notice 通知公告
type Notice struct {
	Nid        int       `gorm:"primary_key;AUTO_INCREMENT" json:"nid"`
	Content    string    `gorm:"type:text" json:"content"`            // 通知内容
	NoticeType string    `gorm:"type:varchar(50)" json:"notice_type"` // 通知类型
	IsShow     bool      `json:"is_show"`                             // 是否显示
	CreateTime time.Time `gorm:"autoCreateTime" json:"create_time"`   // 创建时间
	UpdateTime time.Time `gorm:"autoUpdateTime" json:"update_time"`   // 更新时间
	IsTop      bool      `json:"is_top"`                              // 是否置顶
	IsHtml     bool      `json:"is_html"`                             // 是否HTML格式
}

type Introduction struct {
	Id         int       `gorm:"primary_key;AUTO_INCREMENT" json:"id"`
	Content    string    `gorm:"type:text" json:"content"`
	IsShow     bool      `json:"is_show"`
	CreateTime time.Time `gorm:"autoCreateTime" json:"create_time"`
	UpdateTime time.Time `gorm:"autoUpdateTime" json:"update_time"`
	IsRedirect bool      `json:"is_redirect"`
	Link       string    `json:"link"`
}

// TableName 指定表名
func (Introduction) TableName() string {
	return "introduction"
}

// TableName 指定表名
func (Notice) TableName() string {
	return "notices"
}

// CreateNoticeRequest 创建通知请求
type CreateNoticeRequest struct {
	Content    string `json:"content" binding:"required"` // 通知内容
	NoticeType string `json:"notice_type"`                // 通知类型
	IsShow     bool   `json:"is_show"`                    // 是否显示
	IsTop      bool   `json:"is_top"`                     // 是否置顶
	IsHtml     bool   `json:"is_html"`                    // 是否HTML格式
}

// UpdateNoticeRequest 更新通知请求
type UpdateNoticeRequest struct {
	Content    string `json:"content" binding:"required"` // 通知内容
	NoticeType string `json:"notice_type"`                // 通知类型
	IsShow     bool   `json:"is_show"`                    // 是否显示
	IsTop      bool   `json:"is_top"`                     // 是否置顶
	IsHtml     bool   `json:"is_html"`                    // 是否HTML格式
}

// CreateIntroductionRequest 创建使用须知请求
type CreateIntroductionRequest struct {
	Content    string `json:"content" binding:"required"` // 使用须知内容
	IsShow     bool   `json:"is_show"`                    // 是否显示
	IsRedirect bool   `json:"is_redirect"`                // 是否重定向
	Link       string `json:"link"`                       // 重定向链接
}

// UpdateIntroductionRequest 更新使用须知请求
type UpdateIntroductionRequest struct {
	Content    string `json:"content" binding:"required"` // 使用须知内容
	IsShow     bool   `json:"is_show"`                    // 是否显示
	IsRedirect bool   `json:"is_redirect"`                // 是否重定向
	Link       string `json:"link"`                       // 重定向链接
}
