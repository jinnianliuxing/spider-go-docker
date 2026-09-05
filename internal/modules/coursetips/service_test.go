package coursetips

import (
	"math"
	"testing"
)

// makeGrades 生成指定数量的 GradeWithTeacher 测试数据
func makeGrades(teacher string, scores []string) []GradeWithTeacher {
	var grades []GradeWithTeacher
	for _, s := range scores {
		grades = append(grades, GradeWithTeacher{Score: s, Teacher: teacher})
	}
	return grades
}

// repeatScore 重复生成 n 个相同分数
func repeatScore(score string, n int) []string {
	s := make([]string, n)
	for i := range s {
		s[i] = score
	}
	return s
}

func TestAggregateByTeacher_Empty(t *testing.T) {
	result := aggregateByTeacher(nil)
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d teachers", len(result))
	}
}

func TestAggregateByTeacher_SingleTeacher(t *testing.T) {
	// 30 个学生：6 个 90, 6 个 80, 6 个 70, 6 个 60, 6 个 50
	var grades []GradeWithTeacher
	grades = append(grades, makeGrades("张老师", repeatScore("90", 6))...)
	grades = append(grades, makeGrades("张老师", repeatScore("80", 6))...)
	grades = append(grades, makeGrades("张老师", repeatScore("70", 6))...)
	grades = append(grades, makeGrades("张老师", repeatScore("60", 6))...)
	grades = append(grades, makeGrades("张老师", repeatScore("50", 6))...)

	result := aggregateByTeacher(grades)
	if len(result) != 1 {
		t.Fatalf("expected 1 teacher, got %d", len(result))
	}

	stats := result[0]
	if stats.TeacherName != "张老师" {
		t.Errorf("expected teacher name 张老师, got %s", stats.TeacherName)
	}
	if stats.StudentCount != 30 {
		t.Errorf("expected 30 students, got %d", stats.StudentCount)
	}
	if stats.AverageScore != 70 {
		t.Errorf("expected average 70, got %f", stats.AverageScore)
	}
	if stats.MaxScore != 90 {
		t.Errorf("expected max 90, got %f", stats.MaxScore)
	}
	if stats.MinScore != 50 {
		t.Errorf("expected min 50, got %f", stats.MinScore)
	}
	// 6 students < 60 out of 30
	if stats.FailRate != 0.2 {
		t.Errorf("expected fail rate 0.2, got %f", stats.FailRate)
	}
}

func TestAggregateByTeacher_ScoreDistribution(t *testing.T) {
	// 30 个学生，每个分数段 6 个
	var grades []GradeWithTeacher
	grades = append(grades, makeGrades("李老师", repeatScore("55", 6))...) // 0-59
	grades = append(grades, makeGrades("李老师", repeatScore("65", 6))...) // 60-69
	grades = append(grades, makeGrades("李老师", repeatScore("75", 6))...) // 70-79
	grades = append(grades, makeGrades("李老师", repeatScore("85", 6))...) // 80-89
	grades = append(grades, makeGrades("李老师", repeatScore("95", 6))...) // 90-100

	result := aggregateByTeacher(grades)
	if len(result) != 1 {
		t.Fatalf("expected 1 teacher, got %d", len(result))
	}

	dist := result[0].ScoreDistribution
	if dist.Range0To59 != 6 {
		t.Errorf("expected 6 in 0-59, got %d", dist.Range0To59)
	}
	if dist.Range60To69 != 6 {
		t.Errorf("expected 6 in 60-69, got %d", dist.Range60To69)
	}
	if dist.Range70To79 != 6 {
		t.Errorf("expected 6 in 70-79, got %d", dist.Range70To79)
	}
	if dist.Range80To89 != 6 {
		t.Errorf("expected 6 in 80-89, got %d", dist.Range80To89)
	}
	if dist.Range90To100 != 6 {
		t.Errorf("expected 6 in 90-100, got %d", dist.Range90To100)
	}
}

func TestAggregateByTeacher_NonNumericScores(t *testing.T) {
	// 30 个学生用非数值成绩
	var grades []GradeWithTeacher
	grades = append(grades, makeGrades("王老师", repeatScore("优", 8))...)   // 95
	grades = append(grades, makeGrades("王老师", repeatScore("良", 8))...)   // 85
	grades = append(grades, makeGrades("王老师", repeatScore("及格", 8))...)  // 65
	grades = append(grades, makeGrades("王老师", repeatScore("不及格", 6))...) // 50

	result := aggregateByTeacher(grades)
	if len(result) != 1 {
		t.Fatalf("expected 1 teacher, got %d", len(result))
	}

	stats := result[0]
	if stats.StudentCount != 30 {
		t.Errorf("expected 30 students, got %d", stats.StudentCount)
	}
	if stats.MaxScore != 95 {
		t.Errorf("expected max 95, got %f", stats.MaxScore)
	}
	if stats.MinScore != 50 {
		t.Errorf("expected min 50, got %f", stats.MinScore)
	}
	// 6 students < 60 (不及格=50) out of 30
	if stats.FailRate != 0.2 {
		t.Errorf("expected fail rate 0.2, got %f", stats.FailRate)
	}
}

func TestAggregateByTeacher_InvalidScoresSkipped(t *testing.T) {
	// 30 个有效 + 2 个无效
	var grades []GradeWithTeacher
	grades = append(grades, makeGrades("赵老师", repeatScore("80", 15))...)
	grades = append(grades, makeGrades("赵老师", repeatScore("90", 15))...)
	grades = append(grades, GradeWithTeacher{Score: "", Teacher: "赵老师"})
	grades = append(grades, GradeWithTeacher{Score: "invalid", Teacher: "赵老师"})

	result := aggregateByTeacher(grades)
	if len(result) != 1 {
		t.Fatalf("expected 1 teacher, got %d", len(result))
	}

	stats := result[0]
	if stats.StudentCount != 30 {
		t.Errorf("expected 30 valid students, got %d", stats.StudentCount)
	}
	if stats.AverageScore != 85 {
		t.Errorf("expected average 85, got %f", stats.AverageScore)
	}
}

func TestAggregateByTeacher_MultipleTeachers(t *testing.T) {
	var grades []GradeWithTeacher
	grades = append(grades, makeGrades("A老师", repeatScore("90", 30))...)
	grades = append(grades, makeGrades("B老师", repeatScore("80", 30))...)

	result := aggregateByTeacher(grades)
	if len(result) != 2 {
		t.Fatalf("expected 2 teachers, got %d", len(result))
	}

	if result[0].TeacherName != "A老师" {
		t.Errorf("expected first teacher A老师, got %s", result[0].TeacherName)
	}
	if result[0].StudentCount != 30 {
		t.Errorf("expected 30 students for A老师, got %d", result[0].StudentCount)
	}
	if result[1].TeacherName != "B老师" {
		t.Errorf("expected second teacher B老师, got %s", result[1].TeacherName)
	}
	if result[1].StudentCount != 30 {
		t.Errorf("expected 30 students for B老师, got %d", result[1].StudentCount)
	}
}

func TestAggregateByTeacher_FiltersBelowThreshold(t *testing.T) {
	// A老师有 30 个学生（保留），B老师只有 5 个（过滤掉）
	var grades []GradeWithTeacher
	grades = append(grades, makeGrades("A老师", repeatScore("85", 30))...)
	grades = append(grades, makeGrades("B老师", repeatScore("70", 5))...)

	result := aggregateByTeacher(grades)
	if len(result) != 1 {
		t.Fatalf("expected 1 teacher after filtering, got %d", len(result))
	}
	if result[0].TeacherName != "A老师" {
		t.Errorf("expected A老师, got %s", result[0].TeacherName)
	}
}

func TestAggregateByTeacher_AllFailing(t *testing.T) {
	var grades []GradeWithTeacher
	grades = append(grades, makeGrades("刘老师", repeatScore("30", 10))...)
	grades = append(grades, makeGrades("刘老师", repeatScore("45", 10))...)
	grades = append(grades, makeGrades("刘老师", repeatScore("50", 10))...)

	result := aggregateByTeacher(grades)
	if len(result) != 1 {
		t.Fatalf("expected 1 teacher, got %d", len(result))
	}
	stats := result[0]

	if stats.FailRate != 1.0 {
		t.Errorf("expected fail rate 1.0, got %f", stats.FailRate)
	}
	if stats.ScoreDistribution.Range0To59 != 30 {
		t.Errorf("expected 30 in 0-59, got %d", stats.ScoreDistribution.Range0To59)
	}
}

func TestAggregateByTeacher_AllPassing(t *testing.T) {
	var grades []GradeWithTeacher
	grades = append(grades, makeGrades("陈老师", repeatScore("100", 10))...)
	grades = append(grades, makeGrades("陈老师", repeatScore("90", 10))...)
	grades = append(grades, makeGrades("陈老师", repeatScore("95", 10))...)

	result := aggregateByTeacher(grades)
	if len(result) != 1 {
		t.Fatalf("expected 1 teacher, got %d", len(result))
	}
	stats := result[0]

	if stats.FailRate != 0.0 {
		t.Errorf("expected fail rate 0.0, got %f", stats.FailRate)
	}
	if stats.ScoreDistribution.Range90To100 != 30 {
		t.Errorf("expected 30 in 90-100, got %d", stats.ScoreDistribution.Range90To100)
	}
}

func TestComputeTeacherStats_EmptyScores(t *testing.T) {
	stats := computeTeacherStats("空老师", nil)
	if stats.StudentCount != 0 {
		t.Errorf("expected 0 students, got %d", stats.StudentCount)
	}
	if stats.AverageScore != 0 {
		t.Errorf("expected 0 average, got %f", stats.AverageScore)
	}
}

func TestComputeTeacherStats_Precision(t *testing.T) {
	stats := computeTeacherStats("精度老师", []float64{91, 82, 73})
	expectedAvg := 82.0
	if math.Abs(stats.AverageScore-expectedAvg) > 0.01 {
		t.Errorf("expected average %f, got %f", expectedAvg, stats.AverageScore)
	}
}
