package grade

import "testing"

// 回归测试：《中南林业科技大学本科学生成绩记载说明》的换算口径
//  1. 五级制/两级制 -> 绩点：优/优秀=4、良/良好=3、中/中等=2、及格/合格=1、不及格/不合格=0
//  2. 百分制 -> 绩点：(分数-60)/10+1，且 <60 一律 0
//
// 2026-09-18 修复：原先 52~59 分会按 (分数-50)/10 拿到 0.2~0.9 的非零绩点。
func TestHandelGpPerTranscriptRules(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		// 五级制（缩写 / 全称）
		{"优", 4}, {"优秀", 4},
		{"良", 3}, {"良好", 3},
		{"中", 2}, {"中等", 2},
		{"及格", 1}, {"合格", 1},
		{"不及格", 0}, {"不合格", 0},
		// 百分制 —— (分数-60)/10+1
		{"100", 5}, {"90", 4}, {"85", 3.5}, {"60", 1},
		// 百分制 <60 一律 0（重点回归项）
		{"59.9", 0}, {"59", 0}, {"55", 0}, {"52", 0}, {"51", 0}, {"50", 0}, {"0", 0},
	}
	for _, c := range cases {
		if got := handelGp(c.in); got != c.want {
			t.Errorf("handelGp(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestMapGradeToScoreForBasicPerTranscriptRules(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"优", 90}, {"优秀", 90},
		{"良", 80}, {"良好", 80},
		{"中", 70}, {"中等", 70},
		{"及格", 60}, {"合格", 60},
		{"不及格", 50}, {"不合格", 50},
		{"78.69", 78.69},
	}
	for _, c := range cases {
		if got := mapGradeToScoreForBasic(c.in); got != c.want {
			t.Errorf("mapGradeToScoreForBasic(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
