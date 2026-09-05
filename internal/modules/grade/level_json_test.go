package grade

import (
	"testing"
)

func TestParseLevelGradesJSONBytes(t *testing.T) {
	// 百分制 / 等级制两种行
	raw := []byte(`{
		"code": 0, "count": 2,
		"data": [
			{"skkcdjmc":"大学英语四级","kssj":"2025-12-14","jssj":"2025-12-14","shzt":"审核通过","fslbscj":"120","fslsjcj":"85","fslcj":"205","skcjdj":"","djlbscj":"","djlzcj":""},
			{"skkcdjmc":"全国计算机等级考试二级","kssj":"2025-09-20","jssj":"2025-09-21","shzt":"审核通过","fslbscj":"","fslcj":"","skcjdj":"优秀","djlzcj":"优秀"}
		]
	}`)
	grades, total, err := parseLevelGradesJSONBytes(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if total != 2 || len(grades) != 2 {
		t.Fatalf("total=%d len=%d, want 2/2", total, len(grades))
	}
	if grades[0].CourseName != "大学英语四级" || grades[0].LevGrade != "205" || grades[0].Time != "2025-12-14" {
		t.Errorf("百分制行解析错误: %+v", grades[0])
	}
	if grades[1].LevGrade != "优秀" {
		t.Errorf("等级制行解析错误: %+v", grades[1])
	}
}

func TestParseLevelGradesJSONNumericType(t *testing.T) {
	// 数值类型容错：成绩为数字
	raw := []byte(`{"data":[{"skkcdjmc":"测试课程","fslcj":88,"kssj":"2025-01-01"}]}`)
	grades, _, err := parseLevelGradesJSONBytes(raw)
	if err != nil || len(grades) != 1 {
		t.Fatalf("err=%v len=%d", err, len(grades))
	}
	if grades[0].LevGrade != "88" {
		t.Errorf("数字成绩应为 \"88\", got %q", grades[0].LevGrade)
	}
}
