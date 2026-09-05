package grade

import "strings"

// Grade 成绩信息
type Grade struct {
	SerialNo string  `json:"serialNo"` // 序号
	Term     string  `json:"term"`     // 学期
	Code     string  `json:"code"`     // 课程代码
	Subject  string  `json:"subject"`  // 课程名称
	Score    string  `json:"score"`    // 分数
	Credit   float64 `json:"credit"`   // 学分
	Gpa      float64 `json:"gpa"`      // 绩点
	Status   int     `json:"Status"`   // 状态：0=正常考试，1=补考/重修
	Property string  `json:"property"` // 课程性质名称：必修/选修/任选/限选（来自教务 kcxzmc）
	// CourseProperty 课程属性（来自教务 kcsx，如公选/任选/通识等；可用于识别"评奖评优不计入的公选课程"）
	CourseProperty string `json:"course_property,omitempty"`
	Flag     string  `json:"flag"`     //课程标志 修复缓考还计算成绩的bug
}

// 评奖评优是否计入，判断依据是教务"课程属性"字段 kcsx（映射到 Grade.CourseProperty）。
// 官方口径：仅 kcsx 为"必修"的课程计入评奖评优；kcsx 为"公选/任选/通识/选修"等均不参与。
// 注意：教务 kcxzmc(课程性质名称，映射到 Grade.Property) 例如"公共课/通识教育课程/学科基础课"
// 是课程性质分类，不代表课程属性，因此**不做**评奖评优的剔除依据（真实抓取中 kcxzmc 从不出现"必修"字样）。

// awardMustKeywords 在课程属性字段中代表"必修课"的关键词（按需补充）
var awardMustKeywords = []string{"必修"}

// courseAttrIsRequired 判断教务课程属性(kcsx)是否标识该课为必修。
func courseAttrIsRequired(attr string) bool {
	for _, kw := range awardMustKeywords {
		if strings.Contains(attr, kw) {
			return true
		}
	}
	return false
}

// IsAwardEligible 判断课程是否计入"评奖评优GPA"。
// 口径：以教务课程属性 kcsx(CourseProperty) 为准，仅当其为"必修"时计入。
// 为兼容尚未写入 course_property 的历史数据（仅当 kcsx 源字段缺失、course_property 为空时），
// 兜底回看课程性质名称 property(kcxzmc) 中是否字面含"必修"；若 property 仅是"公共课/通识课"等则不计。
func IsAwardEligible(g Grade) bool {
	// 第一优先级：课程属性 kcsx。只要有值，就完全以它为准（只有"必修"计入）。
	if g.CourseProperty != "" {
		return courseAttrIsRequired(g.CourseProperty)
	}
	// 兜底：course_property 缺失的历史数据，仅接受 property 字面含"必修"；"公共课/通识课"等不会命中。
	return courseAttrIsRequired(g.Property)
}

type UserDetailedInfo struct {
	Grade   string `json:"grade"`
	Class   string `json:"class"`
	Major   string `json:"major"`
	Collage string `json:"collage"`
	Name    string `json:"name"`
}

// GPA 绩点信息
type GPA struct {
	AverageGPA   float64 `json:"averageGPA"`   // 平均绩点
	AverageScore float64 `json:"averageScore"` // 平均分
	BasicScore   float64 `json:"basicScore"`   // 基本分
}

// LevelGrade 等级考试成绩
type LevelGrade struct {
	No         string `json:"no"`         // 序号
	CourseName string `json:"CourseName"` // 考试名称
	LevGrade   string `json:"LevelGrade"` // 成绩/等级
	Time       string `json:"Time"`       // 考试时间
}

// RegularGrade 平时分信息
type RegularGrade struct {
	FinalExamScore string `json:"finalExamScore"` //期末考试分数
	FinalExamRatio string `json:"finalExamRatio"` //期末成绩占总成绩比例
	RegularScore   string `json:"regularScore"`   //平时成绩分数
	RegularRatio   string `json:"regularRatio"`   //平时成绩占总成绩比例
	FinalScore     string `json:"finalScore"`     //总成绩
}

// GetGradesRequest 获取成绩请求
type GetGradesRequest struct {
	Term string `form:"term"` // 学期（可选），格式：2024-2025-1
}

// GradesResponse 成绩响应
type GradesResponse struct {
	Grades []Grade `json:"grades" binding:"required"`
	GPA    *GPA    `json:"gpa" binding:"required"`
}

type GetRegularGradesRequest struct {
	Term string `json:"term" binding:"required"` //学期
	Code string `json:"code" binding:"required"` //课程编号
}

// TermGradesData 单个学期的成绩数据（扁平结构，对齐前端成绩分析 tab）
type TermGradesData struct {
	Term         string  `json:"term"`          // 学期
	AverageGPA   float64 `json:"average_gpa"`   // 平均绩点
	AverageScore float64 `json:"average_score"` // 平均分
	TotalCredit  float64 `json:"total_credit"`  // 总学分
	CourseCount  int     `json:"course_count"`  // 课程数
}

// TermsGradesAnalysis 多学期成绩分析
type TermsGradesAnalysis struct {
	CurrentTerm   string           `json:"current_term"`   // 当前学期
	Semesters     []TermGradesData `json:"semesters"`      // 各学期数据
	OverallGPA    *GPA             `json:"overall_gpa"`    // 总体GPA
	TrendAnalysis *TrendAnalysis   `json:"trend_analysis"` // 趋势分析
}

// TrendAnalysis 趋势分析
type TrendAnalysis struct {
	GPATrend     string  `json:"gpa_trend"`      // GPA趋势：上升/下降/稳定
	ScoreTrend   string  `json:"score_trend"`    // 成绩趋势
	BestTerm     string  `json:"best_term"`      // 最好的学期
	BestTermGPA  float64 `json:"best_term_gpa"`  // 最好学期的GPA
	WorstTerm    string  `json:"worst_term"`     // 最差的学期
	WorstTermGPA float64 `json:"worst_term_gpa"` // 最差学期的GPA
}
