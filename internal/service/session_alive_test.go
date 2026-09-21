package service

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"spider-go/internal/common"
)

// ---- 测试替身 ----

// fakeSession 实现 SessionService，仅覆盖被测路径需要的方法。
type fakeSession struct {
	cached          []*http.Cookie
	loginErr        error
	loginCalls      int
	invalidated     int
	cacheAfterLogin bool // 登录成功后是否提供新 cookies
}

func (f *fakeSession) LoginAndCache(ctx context.Context, uid int, username, password string) error {
	f.loginCalls++
	if f.loginErr != nil {
		return f.loginErr
	}
	if f.cacheAfterLogin {
		f.cached = []*http.Cookie{{Name: "bzb_jsxsd", Value: "fresh"}}
	}
	return nil
}

func (f *fakeSession) GetCachedCookies(ctx context.Context, uid int) ([]*http.Cookie, error) {
	return f.cached, nil
}

func (f *fakeSession) InvalidateSession(ctx context.Context, uid int) error {
	f.invalidated++
	f.cached = nil
	return nil
}

func (f *fakeSession) LoginAndCacheWithConfig(ctx context.Context, uid int, username, password, loginURL, redirectURL string, cookieCache CookieCache) error {
	return nil
}

func (f *fakeSession) LoginAndGetClient(ctx context.Context, username, password string) (*http.Client, error) {
	return nil, nil
}

func (f *fakeSession) LoginCheck(ctx context.Context, username, password string) error { return nil }

func (f *fakeSession) StartPhoneMFALogin(ctx context.Context, uid int, username, password string) (string, string, error) {
	return "", "", nil
}

func (f *fakeSession) CompletePhoneMFALogin(ctx context.Context, challengeID string, code string) error {
	return nil
}

// CacheLoginSession 外部登录会话缓存（测试用空实现；扫码/手机号链路不参与会话保活测试）
func (f *fakeSession) CacheLoginSession(ctx context.Context, uid int, client *http.Client, tgc *http.Cookie) error {
	return nil
}

// fakeCrawler 实现 CrawlerService，按预设正文作答。
type fakeCrawler struct {
	body  string
	err   error
	calls int
}

func (f *fakeCrawler) FetchWithCookies(ctx context.Context, method, targetURL string, cookies []*http.Cookie, formData url.Values) (io.ReadCloser, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return io.NopCloser(strings.NewReader(f.body)), nil
}

const (
	probeURL      = "https://example.invalid/jsxsd/kscj/cjcx_list?xsfs=all"
	sessionExpire = `{"flag1":2,"msgContent":"请先登录系统"}`
	sessionOK     = `{"flag1":1,"data":[{"kcmc":"高等数学","cj":"92"}]}`
)

func resetProbeCache() {
	sessionProbeState.mu.Lock()
	sessionProbeState.ok = make(map[int]time.Time)
	sessionProbeState.mu.Unlock()
}

// ---- EnsureSessionAlive ----

// 缓存会话有效：直接复用，不重登。
func TestEnsureSessionAliveCachedOK(t *testing.T) {
	resetProbeCache()
	ss := &fakeSession{cached: []*http.Cookie{{Name: "bzb_jsxsd", Value: "cached"}}}
	cr := &fakeCrawler{body: sessionOK}

	cookies, err := EnsureSessionAlive(context.Background(), ss, cr, 1001, "sid", "spwd", probeURL)
	if err != nil {
		t.Fatalf("期望成功，得到错误: %v", err)
	}
	if len(cookies) == 0 {
		t.Fatal("期望拿到 cookies")
	}
	if ss.loginCalls != 0 || ss.invalidated != 0 {
		t.Errorf("会话有效时不应重登/清会话，实际 login=%d invalidate=%d", ss.loginCalls, ss.invalidated)
	}
	if cr.calls != 1 {
		t.Errorf("期望探测 1 次，实际 %d 次", cr.calls)
	}
}

// 缓存会话失效：清缓存 → 重新登录 → 复验通过。
// 这是修复"课表只有第一周/等级考试暂无数据"的核心路径。
func TestEnsureSessionAliveRecoversFromExpiredCache(t *testing.T) {
	resetProbeCache()
	ss := &fakeSession{
		cached:          []*http.Cookie{{Name: "bzb_jsxsd", Value: "stale"}},
		cacheAfterLogin: true,
	}
	// 第一次探测返回失效，重登后返回正常
	cr := &sequentialCrawler{bodies: []string{sessionExpire, sessionOK}}

	cookies, err := EnsureSessionAlive(context.Background(), ss, cr, 1002, "sid", "spwd", probeURL)
	if err != nil {
		t.Fatalf("期望自愈成功，得到错误: %v", err)
	}
	if len(cookies) == 0 {
		t.Fatal("期望拿到重登后的 cookies")
	}
	if ss.invalidated != 1 {
		t.Errorf("期望清会话 1 次，实际 %d 次", ss.invalidated)
	}
	if ss.loginCalls != 1 {
		t.Errorf("期望重新登录 1 次，实际 %d 次", ss.loginCalls)
	}
	if cr.calls != 2 {
		t.Errorf("期望探测 2 次（失效 + 复验），实际 %d 次", cr.calls)
	}
}

// 重登后仍失效：返回认证类错误，让前端引导重新输入教务密码，
// 而不是继续把"未登录"响应当成"无数据"。
func TestEnsureSessionAliveFailsAfterRelogin(t *testing.T) {
	resetProbeCache()
	ss := &fakeSession{
		cached:          []*http.Cookie{{Name: "bzb_jsxsd", Value: "stale"}},
		cacheAfterLogin: true,
	}
	cr := &fakeCrawler{body: sessionExpire}

	_, err := EnsureSessionAlive(context.Background(), ss, cr, 1003, "sid", "spwd", probeURL)
	if err == nil {
		t.Fatal("期望返回错误")
	}
	appErr, ok := err.(*common.AppError)
	if !ok || appErr.Code != common.CodeJwcSessionExpired {
		t.Fatalf("期望 CodeJwcSessionExpired，实际 %v", err)
	}
	if ss.invalidated != 1 || ss.loginCalls != 1 {
		t.Errorf("期望清会话 1 次、重登 1 次，实际 invalidate=%d login=%d", ss.invalidated, ss.loginCalls)
	}
}

// 无缓存会话：直接登录，不必探测（新会话必然新鲜），省一次往返。
func TestEnsureSessionAliveNoCacheSkipsProbe(t *testing.T) {
	resetProbeCache()
	ss := &fakeSession{cacheAfterLogin: true}
	cr := &fakeCrawler{body: sessionOK}

	cookies, err := EnsureSessionAlive(context.Background(), ss, cr, 1004, "sid", "spwd", probeURL)
	if err != nil {
		t.Fatalf("期望成功，得到错误: %v", err)
	}
	if len(cookies) == 0 {
		t.Fatal("期望拿到 cookies")
	}
	if ss.loginCalls != 1 {
		t.Errorf("期望登录 1 次，实际 %d 次", ss.loginCalls)
	}
	if cr.calls != 0 {
		t.Errorf("新登录的会话无需探测，实际探测 %d 次", cr.calls)
	}
}

// 登录失败（含密码错误）：统一转成"绑定已失效"，不触发探测。
func TestEnsureSessionAliveLoginFailure(t *testing.T) {
	resetProbeCache()
	ss := &fakeSession{loginErr: common.NewAppError(common.CodeJwcLoginFailed, "用户名或密码错误")}
	cr := &fakeCrawler{body: sessionOK}

	_, err := EnsureSessionAlive(context.Background(), ss, cr, 1005, "sid", "bad", probeURL)
	if err == nil {
		t.Fatal("期望返回错误")
	}
	appErr, ok := err.(*common.AppError)
	if !ok || appErr.Code != common.CodeJwcBindExpired {
		t.Fatalf("期望 CodeJwcBindExpired，实际 %v", err)
	}
	if cr.calls != 0 {
		t.Errorf("登录失败时不应探测，实际 %d 次", cr.calls)
	}
}

// ---- ProbeSessionAlive ----

// 探测请求本身失败（网络/超时）时按"有效"处理，避免误判引发强制重登。
func TestProbeSessionAliveRequestErrorTreatedAsAlive(t *testing.T) {
	resetProbeCache()
	cr := &fakeCrawler{err: io.ErrUnexpectedEOF}
	cookies := []*http.Cookie{{Name: "bzb_jsxsd", Value: "x"}}

	if !ProbeSessionAlive(context.Background(), 1006, cr, probeURL, cookies) {
		t.Fatal("探测请求失败时应按有效处理，避免误踢")
	}
}

// 未配置探测地址时按"有效"处理，否则每次请求都会强制重登。
func TestProbeSessionAliveNoProbeURL(t *testing.T) {
	resetProbeCache()
	cr := &fakeCrawler{body: sessionExpire}
	cookies := []*http.Cookie{{Name: "bzb_jsxsd", Value: "x"}}

	if !ProbeSessionAlive(context.Background(), 1007, cr, "", cookies) {
		t.Fatal("未配置探测地址时应按有效处理")
	}
	if cr.calls != 0 {
		t.Errorf("不应发出探测请求，实际 %d 次", cr.calls)
	}
}

// 有效结论在 TTL 内复用，避免一次页面加载里的多个请求重复探测。
func TestProbeSessionAliveCachesPositiveResult(t *testing.T) {
	resetProbeCache()
	cr := &fakeCrawler{body: sessionOK}
	cookies := []*http.Cookie{{Name: "bzb_jsxsd", Value: "x"}}

	for i := 0; i < 3; i++ {
		if !ProbeSessionAlive(context.Background(), 1008, cr, probeURL, cookies) {
			t.Fatal("期望探测有效")
		}
	}
	if cr.calls != 1 {
		t.Errorf("TTL 内应只探测 1 次，实际 %d 次", cr.calls)
	}
}

// 失效结论不缓存：否则重登后复验会被缓存挡住，永远判失效。
func TestProbeSessionAliveDoesNotCacheNegative(t *testing.T) {
	resetProbeCache()
	cr := &fakeCrawler{body: sessionExpire}
	cookies := []*http.Cookie{{Name: "bzb_jsxsd", Value: "x"}}

	for i := 0; i < 2; i++ {
		if ProbeSessionAlive(context.Background(), 1009, cr, probeURL, cookies) {
			t.Fatal("期望探测失效")
		}
	}
	if cr.calls != 2 {
		t.Errorf("失效结论不应缓存，期望探测 2 次，实际 %d 次", cr.calls)
	}
}

// ---- isRetryableLoginError ----

// 只有瞬时故障才重试；确定性失败（密码错、需短信验证）重试无益。
func TestIsRetryableLoginError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"超时 → 可重试", common.NewAppError(common.CodeJwcLoginTimeout, "超时"), true},
		{"请求失败 → 可重试", common.NewAppError(common.CodeJwcRequestFailed, "请求失败"), true},
		{"底层网络错误 → 可重试", io.ErrUnexpectedEOF, true},
		{"密码错误 → 不重试", common.NewAppError(common.CodeJwcLoginFailed, "用户名或密码错误"), false},
		{"需要短信验证 → 不重试", common.NewAppError(common.CodeJwcMFARequired, "需要多因素认证"), false},
		{"解析失败 → 不重试", common.NewAppError(common.CodeJwcParseFailed, "找不到 execution"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetryableLoginError(tc.err); got != tc.want {
				t.Errorf("isRetryableLoginError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// sequentialCrawler 按调用次序返回不同正文，用于模拟"先失效、重登后正常"。
type sequentialCrawler struct {
	bodies []string
	calls  int
}

func (s *sequentialCrawler) FetchWithCookies(ctx context.Context, method, targetURL string, cookies []*http.Cookie, formData url.Values) (io.ReadCloser, error) {
	idx := s.calls
	s.calls++
	if idx >= len(s.bodies) {
		idx = len(s.bodies) - 1
	}
	return io.NopCloser(strings.NewReader(s.bodies[idx])), nil
}
