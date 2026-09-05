package share

// ShareToken 分享令牌（编码到链接参数中）
type ShareToken struct {
	Uid     int    `json:"u"`
	Term    string `json:"t"`
	StartWk int    `json:"s"`
	EndWk   int    `json:"e"`
}

// CreateShareRequest 创建分享链接请求
// 支持单周（只传week）或范围（传start_week+end_week）
type CreateShareRequest struct {
	Term      string `json:"term" binding:"required"`                               // 学期
	Week      *int   `json:"week,omitempty" binding:"omitempty,min=1,max=20"`       // 单周
	StartWeek *int   `json:"start_week,omitempty" binding:"omitempty,min=1,max=20"` // 起始周
	EndWeek   *int   `json:"end_week,omitempty" binding:"omitempty,min=1,max=20"`   // 结束周
}

// CreateShareResponse 创建分享链接响应
type CreateShareResponse struct {
	Token string `json:"token"` // 分享token
}

// SharedCourseResponse 查看分享的课程表响应
type SharedCourseResponse struct {
	UserName  string      `json:"user_name"` // 分享者昵称
	Term      string      `json:"term"`
	StartWeek int         `json:"start_week"`
	EndWeek   int         `json:"end_week"`
	Schedule  interface{} `json:"schedule"` // 单周为对象，多周为数组
}
