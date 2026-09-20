package course

import (
	"os"
	"path/filepath"
	"testing"
)

// 夹具来自真实浏览器 HAR（2026-09-18 22:51 抓取），是浏览器实测的响应原文。
//
// HAR 已证三件事：
//  1. 请求 xskb_list.do?viweType=0&...&zc=（zc 留空）时，教务返回**全学期所有课程**；
//  2. 返回页 select#zc 的 31 个 option（(全部)+1..30）**一个 selected 都没有**；
//  3. 不带 viweType 的 xskb_list.do 返回的是「tab 壳」（只有 iframe，无课表）。
//
// 因此 select#zc option[selected] 这个覆盖分支在真实响应里**永不命中**，weekNo 保持 requestWeek。
const fixtureCourseDir = "../../../.workbuddy/fixtures"

func openFixture(t *testing.T, name string) *os.File {
	t.Helper()
	p := filepath.Join(fixtureCourseDir, name)
	f, err := os.Open(p)
	if err != nil {
		t.Skipf("夹具不存在，跳过（%s）", p)
	}
	return f
}

// TestCourseTableByWeekFromHAR 逐周跑真实解析器，用金标准计数锁住行为。
// 计数值由 HAR 中每个条目的 `时间:X-Y周[...]` 区间人工推算并逐周核对而来。
func TestCourseTableByWeekFromHAR(t *testing.T) {
	cases := []struct {
		file   string
		term   string
		golden map[int]int // 周次 -> 课程数（0 表示该周无课）
	}{
		{
			file: "course-2026-2027-1.html",
			term: "2026-2027-1",
			// 1-8周[3-4节]x2, 1-8周[5-6节]x2                     → 1..8  = 4
			// 10-17周[1-2节]x2, 10-17周[3-4节], 10-17周[9-10节]  → 10..17 = 4
			// 12-13周[1-8节]                                     → 12,13 = 5
			golden: map[int]int{
				1: 4, 2: 4, 3: 4, 4: 4, 5: 4, 6: 4, 7: 4, 8: 4,
				9: 0,
				10: 4, 11: 4, 12: 5, 13: 5, 14: 4, 15: 4, 16: 4, 17: 4,
				18: 0, 19: 0, 20: 0,
			},
		},
		{
			file: "course-2025-2026-2.html",
			term: "2025-2026-2",
			golden: map[int]int{
				1: 6, 2: 7, 3: 6, 4: 8, 5: 9, 6: 10, 7: 9, 8: 9,
				9: 0,
				10: 4, 11: 5, 12: 5, 13: 4, 14: 8, 15: 8, 16: 2,
				17: 0, 18: 0, 19: 0, 20: 0,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.term, func(t *testing.T) {
			for week := 1; week <= 20; week++ {
				f := openFixture(t, tc.file)
				ws, err := (&courseService{}).parseCourseTableFromHTML(f, week)
				f.Close()
				if err != nil {
					t.Fatalf("第%d周 解析失败: %v", week, err)
				}
				if ws.WeekNo != week {
					t.Errorf("第%d周: WeekNo 被改成 %d（不应被响应页覆盖）", week, ws.WeekNo)
				}
				n := 0
				for _, d := range ws.Days {
					n += len(d.Courses)
				}
				if want := tc.golden[week]; n != want {
					t.Errorf("第%d周: 课程数 = %d，期望 %d", week, n, want)
				} else {
					t.Logf("%s 第%2d周: %d 门课 ✓", tc.term, week, n)
				}
			}
		})
	}
}

// TestTabShellRejectedAsError 「tab 壳」必须被当作取数失败（返回错误），
// 而不是「成功但空课表」—— 后者会被上游无条件写进缓存 1 小时。
func TestTabShellRejectedAsError(t *testing.T) {
	f := openFixture(t, "coursetable-tabshell.html")
	defer f.Close()

	ws, err := (&courseService{}).parseCourseTableFromHTML(f, 1)
	if err == nil {
		n := 0
		if ws != nil {
			for _, d := range ws.Days {
				n += len(d.Courses)
			}
		}
		t.Fatalf("tab 壳应返回错误，实际 err=nil（课程数=%d）→ 空白会被缓存 1 小时", n)
	}
	t.Logf("tab 壳已正确判为失败：%v", err)
}
