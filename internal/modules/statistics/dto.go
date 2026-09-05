package statistics

// DAUResponse DAU响应
type DAUResponse struct {
	Date  string `json:"date"`  // 日期：2024-01-01
	Count int64  `json:"count"` // 日活数量
}

// DAURangeResponse DAU范围响应
type DAURangeResponse struct {
	StartDate string           `json:"start_date"` // 开始日期
	EndDate   string           `json:"end_date"`   // 结束日期
	Data      []DAUDayResponse `json:"data"`       // 每日数据
}

// DAUDayResponse 每日DAU数据
type DAUDayResponse struct {
	Date  string `json:"date"`  // 日期：2024-01-01
	Count int64  `json:"count"` // 日活数量
}

// UserCountResponse 用户数量响应
type UserCountResponse struct {
	TotalCount int64 `json:"total_count"` // 用户总数
}

// NewUserCountResponse 新增用户数量响应
type NewUserCountResponse struct {
	StartDate  string `json:"start_date,omitempty"` // 开始日期（可选）
	EndDate    string `json:"end_date,omitempty"`   // 结束日期（可选）
	Date       string `json:"date,omitempty"`       // 单日日期（可选）
	TotalCount int64  `json:"total_count"`          // 新增用户总数
}
