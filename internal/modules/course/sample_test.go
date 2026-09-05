package course

import (
	"fmt"
	"os"
	"testing"
)

// 用真实抓取的课表样本验证新解析器（手动执行：go test -run TestParseRealSample）
func TestParseRealSample(t *testing.T) {
	f, err := os.Open(`D:/下载/课表_files/xskb_list(1).html`)
	if err != nil {
		t.Skip("样本文件不存在，跳过")
	}
	defer f.Close()

	ws, err := (&courseService{}).parseCourseTableFromHTML(f, 10)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	t.Logf("weekno=%d", ws.WeekNo)
	total := 0
	for _, d := range ws.Days {
		for _, c := range d.Courses {
			total++
			t.Logf("周%d 第%d-%d节 %s | %s | %s", d.Weekday, c.StartPeriod, c.EndPeriod, c.Name, c.Teacher, c.Classroom)
		}
	}
	if total == 0 {
		t.Fatal("未解析到任何课程")
	}
	fmt.Println("total courses:", total)
}
