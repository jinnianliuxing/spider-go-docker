package grade

import (
	"testing"
)

func TestParseGradesJSONBytes(t *testing.T) {
	// 字符串与数字混合的类型容错测试
	raw := []byte(`{
		"code": 0, "msg": "", "count": 3,
		"data": [
			{"xnxqid":"2025-2026-2","kch":"130010072","kc_mc":"自然保护地管理与规划","ksdw":"林学院","zcjstr":"88","cjbs":"","xf":"2.0","zxs":"16.0","jd":"3.7","ksxz":"正常考试","kcxzmc":"必修"},
			{"xnxqid":"2025-2026-2","kch":"130010050","kc_mc":"林学概论","zcjstr":90,"xf":3,"jd":4,"ksxz":"正常考试","kcxzmc":"选修"},
			{"xnxqid":"2025-2026-1","kch":"100000001","kc_mc":"补考课","zcjstr":"75","xf":"2","jd":"2.0","ksxz":"补考","kcxzmc":"必修"}
		]
	}`)
	grades, total, err := parseGradesJSONBytes(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(grades) != 3 {
		t.Fatalf("grades = %d, want 3", len(grades))
	}
	g := grades[0]
	if g.Term != "2025-2026-2" || g.Code != "130010072" || g.Subject != "自然保护地管理与规划" || g.Score != "88" {
		t.Errorf("字段映射错误: %+v", g)
	}
	if g.Credit != 2.0 || g.Gpa != 3.7 {
		t.Errorf("字符串数值解析错误: credit=%v gpa=%v", g.Credit, g.Gpa)
	}
	if grades[1].Credit != 3.0 || grades[1].Gpa != 4.0 || grades[1].Score != "90" {
		t.Errorf("数字类型解析错误: %+v", grades[1])
	}
	if grades[1].Status != 0 {
		t.Errorf("正常考试 status 应为 0: %+v", grades[1])
	}
	if grades[2].Status != 1 {
		t.Errorf("补考 status 应为 1: %+v", grades[2])
	}
}

func TestParseGradesJSONCode200(t *testing.T) {
	raw := []byte(`{"code":200,"msg":"ok","count":0,"data":[{"xnxqid":"2025-2026-1","kc_mc":"X","zcjstr":"60","xf":"1","jd":"1"}]}`)
	grades, _, err := parseGradesJSONBytes(raw)
	if err != nil || len(grades) != 1 {
		t.Fatalf("code=200 应视为成功: err=%v len=%d", err, len(grades))
	}
}

func TestParseGradesJSONNotEvaluated(t *testing.T) {
	raw := []byte(`{"code":500,"msg":"请先完成教学评价","count":0,"data":[]}`)
	_, _, err := parseGradesJSONBytes(raw)
	if err == nil {
		t.Fatal("教评提示应返回错误")
	}
}

func TestParseGradesJSONEmptyCode(t *testing.T) {
	// code 缺省也应视为成功
	raw := []byte(`{"data":[{"kc_mc":"Y","zcjstr":"59","xf":"1","jd":"0"}]}`)
	grades, _, err := parseGradesJSONBytes(raw)
	if err != nil || len(grades) != 1 {
		t.Fatalf("code 缺省应视为成功: err=%v len=%d", err, len(grades))
	}
}
