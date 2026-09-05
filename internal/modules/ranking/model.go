package ranking

import "time"

// StudentGPA 学生GPA数据（只存储GPA，不存储排名）
type StudentGPA struct {
	ID      int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	Uid     int    `gorm:"type:int;not null;uniqueIndex:uk_uid_type_term;index" json:"uid"`
	Sid     string `gorm:"type:varchar(50);not null;index" json:"sid"`                  // 学号
	Name    string `gorm:"type:varchar(255);not null" json:"name"`                      // 姓名
	College string `gorm:"type:varchar(100);not null;index:idx_college" json:"college"` // 学院
	Major   string `gorm:"type:varchar(100);not null;index:idx_major" json:"major"`     // 专业
	Grade   string `gorm:"type:varchar(20);not null;index:idx_grade" json:"grade"`      // 年级
	Class   string `gorm:"type:varchar(50);not null;index:idx_class" json:"class"`      // 班级

	// GPA数据
	GPA              float64 `gorm:"type:decimal(5,3);not null;index:idx_gpa" json:"gpa"` // 平均绩点
	AvgScore         float64 `gorm:"type:decimal(6,2);not null" json:"avg_score"`         // 平均分
	TotalCredit      float64 `gorm:"type:decimal(6,2);not null" json:"total_credit"`      // 总学分
	CompletedCourses int     `gorm:"type:int;not null" json:"completed_courses"`          // 完成课程数

	// 统计周期
	StatisticsTerm string `gorm:"type:varchar(20);uniqueIndex:uk_uid_type_term;index;comment:统计学期(如2024-2025-1,all表示累计)" json:"statistics_term"`
	StatisticsType string `gorm:"type:varchar(20);not null;default:'cumulative';uniqueIndex:uk_uid_type_term;index;comment:统计类型(cumulative累计/semester学期)" json:"statistics_type"`

	// 时间戳
	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

func (*StudentGPA) TableName() string {
	return "student_gpas"
}

// StudentRankingView 学生排名视图
type StudentRankingView struct {
	StudentGPA
	CollegeRank  int `json:"college_rank"`  // 学院排名（实时计算）
	MajorRank    int `json:"major_rank"`    // 专业排名（实时计算）
	GradeRank    int `json:"grade_rank"`    // 年级排名（实时计算）
	ClassRank    int `json:"class_rank"`    // 班级排名（实时计算）
	CollegeTotal int `json:"college_total"` // 学院总人数
	MajorTotal   int `json:"major_total"`   // 专业总人数
	GradeTotal   int `json:"grade_total"`   // 年级总人数
	ClassTotal   int `json:"class_total"`   // 班级总人数
}

// CollegeRankingStats 学院排名统计
type CollegeRankingStats struct {
	College       string  `json:"college"`
	TotalStudents int     `json:"total_students"`
	AvgGPA        float64 `json:"avg_gpa"`
	MaxGPA        float64 `json:"max_gpa"`
	MinGPA        float64 `json:"min_gpa"`
	Top10AvgGPA   float64 `json:"top10_avg_gpa"` // 前10%平均GPA
}

// MajorRankingStats 专业排名统计
type MajorRankingStats struct {
	Major         string  `json:"major"`
	TotalStudents int     `json:"total_students"`
	AvgGPA        float64 `json:"avg_gpa"`
	MaxGPA        float64 `json:"max_gpa"`
	MinGPA        float64 `json:"min_gpa"`
}

// GetRankingRequest 获取排名请求
type GetRankingRequest struct {
	StatisticsType string `form:"statistics_type"` // cumulative(累计) 或 semester(学期) 或 year(学年)
	StatisticsTerm string `form:"statistics_term"` // 学期/学年（如 2024-2025-1 或 2024-2025）
}

// MyRankingResponse 我的排名响应（只显示自己的排名，不显示他人信息）
type MyRankingResponse struct {
	// 基本信息
	Name    string `json:"name"`    // 姓名
	College string `json:"college"` // 学院
	Major   string `json:"major"`   // 专业
	Grade   string `json:"grade"`   // 年级
	Class   string `json:"class"`   // 班级

	// GPA信息
	GPA      float64 `json:"gpa"`       // 平均绩点
	AvgScore float64 `json:"avg_score"` // 平均分

	// 排名信息（只显示排名，不显示他人）
	CollegeRank  int `json:"college_rank"`  // 学院排名
	CollegeTotal int `json:"college_total"` // 学院总人数
	MajorRank    int `json:"major_rank"`    // 专业排名
	MajorTotal   int `json:"major_total"`   // 专业总人数

	// 统计信息
	StatisticsType string `json:"statistics_type"` // 统计类型
	StatisticsTerm string `json:"statistics_term"` // 统计学期/学年
}

// RankingResponse 排名响应（保留兼容，但简化内容）
type RankingResponse struct {
	Student      *StudentRankingView  `json:"student"`
	CollegeStats *CollegeRankingStats `json:"college_stats"`
	MajorStats   *MajorRankingStats   `json:"major_stats"`
}

// RankingListRequest 排名列表请求
type RankingListRequest struct {
	College        string `form:"college"`                                    // 学院筛选
	Major          string `form:"major"`                                      // 专业筛选
	Grade          string `form:"grade"`                                      // 年级筛选
	Class          string `form:"class"`                                      // 班级筛选
	StatisticsType string `form:"statistics_type" binding:"required"`         // cumulative 或 semester
	StatisticsTerm string `form:"statistics_term"`                            // 学期
	Page           int    `form:"page" binding:"required,min=1"`              // 页码
	PageSize       int    `form:"page_size" binding:"required,min=1,max=100"` // 每页数量
}

// RankingListResponse 排名列表响应
type RankingListResponse struct {
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
	List     []*StudentRankingView `json:"list"`
}

// StatisticsType 统计类型常量
const (
	StatisticsTypeCumulative = "cumulative" // 累计统计（所有学期）
	StatisticsTypeSemester   = "semester"   // 学期统计（单学期）
)

// StudentGPAData 用于GPA计算的数据结构（从对账模块传入）
type StudentGPAData struct {
	Uid              int
	Sid              string
	Name             string
	College          string
	Major            string
	Grade            string
	Class            string
	GPA              float64
	AvgScore         float64
	TotalCredit      float64
	CompletedCourses int
}
