package course

// Course 课程信息
type Course struct {
	Name        string `json:"name"`         // 课程名称
	Teacher     string `json:"teacher"`      // 任课老师
	Classroom   string `json:"classroom"`    // 教室
	Weekday     int    `json:"weekday"`      // 周几：1~7
	StartPeriod int    `json:"start_period"` // 开始节次
	EndPeriod   int    `json:"end_period"`   // 结束节次
}

// DaySchedule 一天的课程安排
type DaySchedule struct {
	Weekday int      `json:"weekday"` // 值为1-7，表示周一到周日
	Courses []Course `json:"courses"` // 当天课程，没有课则为nil
}

// WeekSchedule 一周的课程安排
type WeekSchedule struct {
	WeekNo    int           `json:"weekno"`    // 周次
	Starttime string        `json:"starttime"` // 开始日期
	Endtime   string        `json:"endtime"`   // 结束日期
	Days      []DaySchedule `json:"days"`      // 7天的课程安排
}

// GetCourseTableRequest 获取课程表请求
type GetCourseTableRequest struct {
	Week int    `form:"week" binding:"required,min=1,max=20"` // 周次：1-20
	Term string `form:"term" binding:"required"`              // 学期：2024-2025-1
}
