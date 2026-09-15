package grade

import "testing"

// TestSessionLooksValid 固化"会话有效性判据"的实测结论。
//
// 背景：教务端对失效会话的响应**不含登录页特征**（不是 HTML 登录页），
// 而是紧凑 JSON：{"flag1":2,"msgContent":"请先登录系统"}。
// 早期实现只匹配"用户没有登录""请重新登录"等文案，导致漏判——
// 服务器上陈旧会话被复用、请求静默失败、用户收不到成绩单邮件。
func TestSessionLooksValid(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "教务端未登录标准响应（实测，最可靠判据）",
			text: `{"flag1":2,"msgContent":"请先登录系统"}`,
			want: false,
		},
		{
			name: "flag1=2 但无明确文案（不判失效，避免误踢）",
			text: `{"flag1":2}`,
			want: true,
		},
		{
			name: "登录页特征：密码输入框",
			text: `<input type="password" name="password" id="password">`,
			want: false,
		},
		{
			name: "SSO 未完成，带 LoginToXk",
			text: `<script>var url="/jsxsd/framework/main.jsp";LoginToXk();</script>`,
			want: false,
		},
		{
			name: "明确的未登录文案",
			text: `<div>用户没有登录，请重新登录</div>`,
			want: false,
		},
		{
			name: "成绩数据（会话有效，flag1=1）",
			text: `{"flag1":1,"data":[{"kcmc":"高等数学","cj":"92"}]}`,
			want: true,
		},
		{
			name: "空成绩列表（会话有效，无数据）",
			text: `{"flag1":1,"data":[]}`,
			want: true,
		},
		{
			name: "空响应（无法判定，按有效处理以免误踢）",
			text: ``,
			want: true,
		},
		{
			name: "HTML 页面但无登录特征（会话语境下视为有效）",
			text: `<table><tr><td>高等数学</td><td>92</td></tr></table>`,
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sessionLooksValid(tc.text); got != tc.want {
				t.Errorf("sessionLooksValid(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

// TestIsSessionExpiredDoc 覆盖扩展后的关键词列表
func TestIsSessionExpiredDoc(t *testing.T) {
	expired := []string{
		"用户没有登录",
		"请重新登录",
		"请先登录系统",
		"正在登录",
		"用户未登录",
		"登录超时",
		"会话已过期",
		"会话超时",
		`<input name="userPassword">`,
		"LoginToXk",
	}
	for _, s := range expired {
		if !isSessionExpiredDoc(s) {
			t.Errorf("isSessionExpiredDoc(%q) = false, want true", s)
		}
	}

	valid := []string{
		`{"flag1":1,"data":[{"kcmc":"大学英语"}]}`,
		"<table><tr><td>正常成绩表</td></tr></table>",
	}
	for _, s := range valid {
		if isSessionExpiredDoc(s) {
			t.Errorf("isSessionExpiredDoc(%q) = true, want false", s)
		}
	}
}
