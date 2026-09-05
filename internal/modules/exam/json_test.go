package exam

import (
	"testing"
)

func TestParseExamsJSONBytes(t *testing.T) {
	raw := []byte(`{
		"code": 0, "msg": "", "count": 2,
		"data": [
			{"xqmc":"长沙校区","ksxq":"长沙校区","kssj":"2026-01-12 09:00~11:00","js_mc":"树人楼201","kch":"130010072","kskcmc":"自然保护地管理与规划","jsxm":"张志强","zwh":"12","zkzh":"20260001","ksccmc":"第1场","bzywmc":"","skrs":40},
			{"xqmc":"长沙校区","kssj":"2026-01-14 15:00~17:00","js_mc":"树人楼305","kch":"130010050","kskcmc":"林学概论","zwh":"5","skrs":"35"}
		]
	}`)
	exams, total, err := parseExamsJSONBytes(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if total != 2 || len(exams) != 2 {
		t.Fatalf("total=%d len=%d, want 2/2", total, len(exams))
	}
	e := exams[0]
	if e.ClassNo != "130010072" || e.ClassName != "自然保护地管理与规划" || e.Time != "2026-01-12 09:00~11:00" || e.Place != "树人楼201" {
		t.Errorf("字段映射错误: %+v", e)
	}
	if exams[1].SerialNo != "2" || exams[1].Place != "树人楼305" {
		t.Errorf("第二行解析错误: %+v", exams[1])
	}
}

func TestParseExamsJSONError(t *testing.T) {
	raw := []byte(`{"code":500,"msg":"会话已过期","count":0,"data":[]}`)
	_, _, err := parseExamsJSONBytes(raw)
	if err == nil {
		t.Fatal("code=500 应返回错误")
	}
}
