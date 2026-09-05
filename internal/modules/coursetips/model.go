package coursetips

import "strconv"

// GetTeacherStatsRequest 查询请求
type GetTeacherStatsRequest struct {
	CourseName string `form:"course_name" binding:"required"`
}

// TeacherStatsResponse 教师统计响应
type TeacherStatsResponse struct {
	CourseName string         `json:"course_name"`
	Teachers   []TeacherStats `json:"teachers"`
}

// TeacherStats 单个教师的统计数据
type TeacherStats struct {
	TeacherName       string            `json:"teacher_name"`
	StudentCount      int               `json:"student_count"`
	AverageScore      float64           `json:"average_score"`
	MaxScore          float64           `json:"max_score"`
	MinScore          float64           `json:"min_score"`
	FailRate          float64           `json:"fail_rate"`
	ScoreDistribution ScoreDistribution `json:"score_distribution"`
}

// ScoreDistribution 分数段分布
type ScoreDistribution struct {
	Range0To59   int `json:"range_0_59"`
	Range60To69  int `json:"range_60_69"`
	Range70To79  int `json:"range_70_79"`
	Range80To89  int `json:"range_80_89"`
	Range90To100 int `json:"range_90_100"`
}

// GradeWithTeacher 关联查询结果
type GradeWithTeacher struct {
	Score   string
	Teacher string
}

// ValidPECourses 合法体育选修课名称列表
var ValidPECourses = []string{
	"体育选项课Ⅰ",
	"体育选项课Ⅱ",
	"体育选项课Ⅲ",
}

// ScoreMapping 非数值成绩映射表
var ScoreMapping = map[string]float64{
	"优":   95,
	"良":   85,
	"中":   75,
	"及格":  65,
	"不及格": 50,
}

// IsValidPECourse 校验课程名称是否为合法的体育选修课
func IsValidPECourse(name string) bool {
	for _, course := range ValidPECourses {
		if course == name {
			return true
		}
	}
	return false
}

// ParseScore 解析成绩字符串为数值分数
// 返回 (分数, 是否有效)
func ParseScore(score string) (float64, bool) {
	if score == "" {
		return 0, false
	}

	// 尝试数值解析
	if val, err := strconv.ParseFloat(score, 64); err == nil {
		return val, true
	}

	// 尝试映射表查找
	if val, ok := ScoreMapping[score]; ok {
		return val, true
	}

	return 0, false
}
