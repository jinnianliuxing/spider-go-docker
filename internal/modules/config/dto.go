package config

// SetCurrentTermRequest 设置当前学期请求
type SetCurrentTermRequest struct {
	Term string `json:"term" binding:"required"` // 格式：2024-2025-1
}

// CurrentTermResponse 当前学期响应
type CurrentTermResponse struct {
	Term string `json:"term"`
}

// SetSemesterDatesRequest 设置学期日期请求
type SetSemesterDatesRequest struct {
	Term      string `json:"term" binding:"required"`       // 格式：2024-2025-1
	StartDate string `json:"start_date" binding:"required"` // 格式：2024-09-01
	EndDate   string `json:"end_date" binding:"required"`   // 格式：2025-01-15
}

// SemesterDatesResponse 学期日期响应
type SemesterDatesResponse struct {
	Term      string `json:"term"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}
