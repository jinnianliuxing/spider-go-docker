package grade

import (
	"os"
	"testing"

	"spider-go/internal/common"
)

// 夹具来自真实浏览器 HAR（2026-09-18 22:51 抓取）对 layui 数据接口的实测响应：
//
//	GET /jsxsd/kscj/djkscj_list?type=listData&pageNum=1&pageSize=20
//	Header: X-Requested-With: XMLHttpRequest
//	→ {"msg":"","code":0,"data":[{...3 条...}],"count":3}
//
// 注意：该接口返回的 Content-Type 是 text/html（不是 application/json），
// 所以只能用「首字符是否 {」来判断，不能依赖 Content-Type。
const fixtureGradeDir = "../../../.workbuddy/fixtures"

// TestParseLevelGradesJSONFromHAR 用真实响应验证 layui 分页 JSON 解析。
func TestParseLevelGradesJSONFromHAR(t *testing.T) {
	raw, err := os.ReadFile(fixtureGradeDir + "/levelgrade-listdata.json")
	if err != nil {
		t.Skipf("夹具不存在，跳过: %v", err)
	}

	if !looksLikeJSON(raw) {
		t.Fatalf("looksLikeJSON 判定失败（真实响应确为 JSON）")
	}

	rows, total, err := parseLevelGradesJSONBytes(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if total != 3 {
		t.Errorf("count = %d，期望 3", total)
	}
	if len(rows) != 3 {
		t.Fatalf("解析出 %d 条，期望 3 条", len(rows))
	}

	wantScore := []string{"387", "396", "430"}
	wantTime := []string{"2024-12-14", "2025-12-13", "2026-06-13"}
	for i, r := range rows {
		t.Logf("#%d %s | %s | %s", i+1, r.CourseName, r.LevGrade, r.Time)
		if r.CourseName != "英语等级考试(CET4)" {
			t.Errorf("#%d 课程名 = %q", i+1, r.CourseName)
		}
		if r.LevGrade != wantScore[i] {
			t.Errorf("#%d 成绩 = %q，期望 %q", i+1, r.LevGrade, wantScore[i])
		}
		if r.Time != wantTime[i] {
			t.Errorf("#%d 时间 = %q，期望 %q", i+1, r.Time, wantTime[i])
		}
	}
}

// TestParseLevelGradesHTMLShellFails 固化"会话失效时的失败路径"：
// 若拿到的是 layui 页面壳（<table id="djkscj_table"> 内没有任何数据行），
// 当前 HTML 解析器会因找不到 #dataList 而报 CodeJwcParseFailed。
// 而 isAuthenticationError 明确把 CodeJwcParseFailed 排除在认证错误之外，
// 于是"会话失效"最终以"未找到等级考试数据"的形式暴露给用户。
func TestParseLevelGradesHTMLShellFails(t *testing.T) {
	f, err := os.Open(fixtureGradeDir + "/levelgrade-shell.html")
	if err != nil {
		t.Skipf("夹具不存在，跳过: %v", err)
	}
	defer f.Close()

	rows, herr := (&gradeService{}).parseLevelGradesFromHTML(f)
	t.Logf("页面壳：rows=%d err=%v", len(rows), herr)
	if herr == nil {
		t.Log("注意：解析器未报错（行为已改变，请复核 isAuthenticationError 的放行条件）")
	}
}

// TestParseLevelGradesSessionExpired 固化"会话失效识别"的修复（2026-09-18）。
//
// 登录态无效时，教务对该数据接口返回：
//
//	{"flag1":2,"msgContent":"请先登录系统"}
//
// 该响应既没有 code 也没有 data。旧实现把"code 缺省"当作成功、数据为空，
// 结果前端只看到"暂无数据"，且坏 cookie 不会被清除——
// 一次会话失效会让等级考试成绩持续不可用直到 Redis TTL 到期。
//
// 修复后必须返回认证类错误（CodeJwcSessionExpired），
// 上层 isAuthenticationError 才会走"清会话 + 重登 + 前端弹重绑"的路径。
func TestParseLevelGradesSessionExpired(t *testing.T) {
	raw := []byte(`{"flag1":2,"msgContent":"请先登录系统"}`)

	rows, total, err := parseLevelGradesJSONBytes(raw)
	if err == nil {
		t.Fatalf("期望返回错误，实际解析出 %d 条（total=%d）", len(rows), total)
	}
	appErr, ok := err.(*common.AppError)
	if !ok || appErr.Code != common.CodeJwcSessionExpired {
		t.Fatalf("期望 CodeJwcSessionExpired，实际 %v", err)
	}
	t.Logf("会话失效被正确识别: %v", err)
}

// TestParseLevelGradesNormalStillOK 防回归：正常响应不受会话判定影响。
func TestParseLevelGradesNormalStillOK(t *testing.T) {
	raw := []byte(`{"msg":"","code":0,"count":1,"data":[{"skkcdjmc":"英语等级考试(CET4)","kssj":"2024-12-14","fslcj":"425"}]}`)

	rows, total, err := parseLevelGradesJSONBytes(raw)
	if err != nil {
		t.Fatalf("正常响应不应报错: %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("解析异常：total=%d rows=%d", total, len(rows))
	}
	if rows[0].LevGrade != "425" {
		t.Errorf("成绩 = %q，期望 425", rows[0].LevGrade)
	}
}
