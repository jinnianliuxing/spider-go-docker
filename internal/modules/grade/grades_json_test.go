package grade

import (
	"testing"

	"spider-go/internal/common"
)

// TestParseGradesJSONSessionExpired 固化成绩查询接口的会话失效识别（2026-09-18）。
//
// 登录态无效时教务返回 {"flag1":2,"msgContent":"请先登录系统"}，该响应没有 code / data。
// 旧实现把"code 缺省"当作成功、成绩列表为空，最终在 fetchAllGradesJSON 里
// 触发 CodeJwcNotEvaluated → 用户看到"请先完成教学评价"这一**误导性**提示，
// 既不知道是登录状态问题，也不会被引导去重新输入密码。
func TestParseGradesJSONSessionExpired(t *testing.T) {
	raw := []byte(`{"flag1":2,"msgContent":"请先登录系统"}`)

	grades, total, err := parseGradesJSONBytes(raw)
	if err == nil {
		t.Fatalf("期望返回错误，实际解析出 %d 条（total=%d）", len(grades), total)
	}
	appErr, ok := err.(*common.AppError)
	if !ok || appErr.Code != common.CodeJwcSessionExpired {
		t.Fatalf("期望 CodeJwcSessionExpired，实际 %v", err)
	}
	if appErr.Code == common.CodeJwcNotEvaluated {
		t.Fatal("不得把会话失效误报为『未教评』")
	}
	t.Logf("成绩接口的会话失效被正确识别: %v", err)
}

// TestParseGradesJSONNormal 防回归：正常响应仍能解析出成绩，且不被 flag1 判定影响。
func TestParseGradesJSONNormal(t *testing.T) {
	raw := []byte(`{"msg":"","code":0,"count":1,"data":[{"xnxqid":"2025-2026-1","kch":"A001","kc_mc":"高等数学","zcjstr":"92","xf":"4","jd":"4.2","kcxzmc":"必修","kcsx":"必修"}]}`)

	grades, total, err := parseGradesJSONBytes(raw)
	if err != nil {
		t.Fatalf("正常响应不应报错: %v", err)
	}
	if total != 1 || len(grades) != 1 {
		t.Fatalf("解析异常：total=%d rows=%d", total, len(grades))
	}
	g := grades[0]
	if g.Subject != "高等数学" || g.Score != "92" || g.Credit != 4 {
		t.Errorf("解析结果异常: %+v", g)
	}
}

// TestParseGradesJSONNotEvaluatedStillDetected 防回归：真正的"未教评"提示
// （code 非 0 且 msg 含教评）必须仍然映射为 CodeJwcNotEvaluated，
// 不能被新增的会话判定抢走。
func TestParseGradesJSONNotEvaluatedStillDetected(t *testing.T) {
	raw := []byte(`{"code":1,"msg":"您还有未完成的教学评价，请先完成教评"}`)

	_, _, err := parseGradesJSONBytes(raw)
	appErr, ok := err.(*common.AppError)
	if !ok || appErr.Code != common.CodeJwcNotEvaluated {
		t.Fatalf("期望 CodeJwcNotEvaluated，实际 %v", err)
	}
}
