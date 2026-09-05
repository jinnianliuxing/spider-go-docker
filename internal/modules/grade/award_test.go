package grade

import "testing"

// 评奖评优口径以教务"课程属性"kcsx(CourseProperty)为准：
// 仅 kcsx=="必修"计入；kcsx=="公选/任选/通识/选修"等均不计入。
// 教务"课程性质"kcxzmc(Property，如 公共课/通识教育课程/学科基础课) 不做剔除依据。
//
// 真实抓取样例（HAR 2026-09-03）：
//
//	(kcxzmc,kcsx): (公共课,必修) (学科基础课,必修) (通识教育课程,必修)
//	              (公共选修课,公选)
func TestIsAwardEligible(t *testing.T) {
	cases := []struct {
		name string
		g    Grade
		want bool
	}{
		// —— 课程属性 kcsx 有值：完全以 kcsx 为准 ——
		{"真实:kcxzmc=通识教育课+课程属性=必修 → 计入", Grade{Property: "通识教育课程", CourseProperty: "必修"}, true},
		{"真实:kcxzmc=公共课+课程属性=必修 → 计入", Grade{Property: "公共课", CourseProperty: "必修"}, true},
		{"真实:kcxzmc=学科基础课+课程属性=必修 → 计入", Grade{Property: "学科基础课", CourseProperty: "必修"}, true},
		{"真实:kcxzmc=公共选修课+课程属性=公选 → 不计入", Grade{Property: "公共选修课", CourseProperty: "公选"}, false},
		{"课程属性=公选 → 不计入", Grade{Property: "必修", CourseProperty: "公选"}, false},
		{"课程属性=任选 → 不计入", Grade{Property: "必修", CourseProperty: "任选"}, false},
		{"课程属性=通识 → 不计入", Grade{Property: "必修", CourseProperty: "通识"}, false},
		{"课程属性=选修 → 不计入", Grade{Property: "必修", CourseProperty: "选修"}, false},
		{"课程属性空、property=必修 → 计入(兼容旧数据)", Grade{Property: "必修", CourseProperty: ""}, true},
		{"课程属性空、property=公共课 → 不计入", Grade{Property: "公共课", CourseProperty: ""}, false},
		{"课程属性空、property=通识教育课程 → 不计入", Grade{Property: "通识教育课程", CourseProperty: ""}, false},
	}
	for _, c := range cases {
		if got := IsAwardEligible(c.g); got != c.want {
			t.Errorf("%s: IsAwardEligible(%+v)=%v, want %v", c.name, c.g, got, c.want)
		}
	}
}

func TestCourseAttrIsRequired(t *testing.T) {
	if !courseAttrIsRequired("必修") {
		t.Error("必修 应识别为必修")
	}
	if courseAttrIsRequired("公选") || courseAttrIsRequired("任选") || courseAttrIsRequired("通识教育") {
		t.Error("公选/任选/通识 不应识别为必修")
	}
	if courseAttrIsRequired("") {
		t.Error("空串不应识别为必修")
	}
}
