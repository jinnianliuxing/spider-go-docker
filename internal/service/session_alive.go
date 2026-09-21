package service

import (
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"spider-go/internal/common"
)

// 本文件集中实现「教务会话是否真的有效」的判定与自愈，供成绩 / 课表 / 考试等
// 所有数据查询共用。
//
// 背景：Redis 中的会话缓存 TTL 为 1 小时，但教务端会话寿命可能更短，
// 且服务重启后 Redis 可能保留教务端已单方面失效的旧会话。此时数据查询会拿到
// "未登录"响应，而旧实现把它当作"正常但没有数据"，于是表现为
// 课表空白 / 等级考试"暂无数据"这类静默失败；更糟的是坏 cookie 不会被清除，
// 同一次故障会持续到 TTL 到期。这里提供统一的探测 + 自愈入口：
//
//	EnsureSessionAlive → GetCookiesOrLoginEx（取或登录）→ ProbeSessionAlive（验活）
//	                                                      ↓ 失效
//	                              InvalidateSession → 重新登录 → 复验

// sessionProbeTTL 同一用户的"有效"探测结论在该时长内复用，
// 避免一次页面加载里的多个请求（如成绩 + 成绩分析）重复探测同一会话。
const sessionProbeTTL = 30 * time.Second

// sessionProbeMaxEntries 结论表的容量上限，超出即整体重置，防止长期运行的内存增长。
const sessionProbeMaxEntries = 4096

var sessionProbeState = struct {
	mu sync.Mutex
	ok map[int]time.Time
}{ok: make(map[int]time.Time)}

// SessionLooksValid 判断响应正文是否表明教务会话仍然有效。
//
// 判据（实测确定）：
// 强智教务系统的数据接口在登录态无效时固定返回紧凑 JSON
//
//	{"flag1":2,"msgContent":"请先登录系统"}
//
// 会话有效时同一接口返回数据，绝不会出现"请先登录系统"。
// 只匹配明确文案、不判 flag1 数值，避免其他接口正常返回 flag1:2 时被误踢。
//
// 注意：教务端对无效 cookie 也会**重新下发** bzb_jsxsd（匿名会话），
// 所以"cookie 值是否变化"是伪判据，不可使用。
func SessionLooksValid(text string) bool {
	if text == "" {
		// 空响应无法判定，按有效处理，避免误判引发无谓的强制重登
		return true
	}
	if strings.Contains(text, "请先登录系统") {
		return false
	}
	if isSessionExpiredDoc(text) {
		return false
	}
	// 登录页特征：密码输入框
	if strings.Contains(text, `name="password"`) || strings.Contains(text, `id="password"`) {
		return false
	}
	return true
}

// isSessionExpiredDoc 判断响应正文是否因登录态失效被踢回登录页
func isSessionExpiredDoc(text string) bool {
	return strings.Contains(text, "用户没有登录") ||
		strings.Contains(text, "请重新登录") ||
		strings.Contains(text, "请先登录") ||
		strings.Contains(text, "正在登录") ||
		strings.Contains(text, "用户未登录") ||
		strings.Contains(text, "登录超时") ||
		strings.Contains(text, "会话已过期") ||
		strings.Contains(text, "会话超时") ||
		strings.Contains(text, "userPassword") ||
		strings.Contains(text, "LoginToXk")
}

// ProbeSessionAlive 用给定 cookies 请求 probeURL，探测教务会话是否真的有效。
//
// 返回 false 表示**确定**已失效；返回 true 表示可用或无法判定（请求失败、响应为空），
// 后者交回调用方按原逻辑处理，避免因探测本身出错而强制用户重登。
//
// 请求经 CrawlerService 发出，因而自带数据接口必需的 X-Requested-With 与 Referer。
func ProbeSessionAlive(ctx context.Context, uid int, crawler CrawlerService, probeURL string, cookies []*http.Cookie) bool {
	if probeURL == "" || crawler == nil {
		// 没有可用的探测条件：无法判定，按有效处理，
		// 否则会把"没配探测地址"变成每次请求都强制重新登录。
		return true
	}
	if len(cookies) == 0 {
		return false
	}

	// 短时间内的"有效"结论直接复用（失效结论不缓存，重登后需要立刻复验）
	if ok, t := sessionProbeCached(uid); ok && time.Since(t) < sessionProbeTTL {
		return true
	}

	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	body, err := crawler.FetchWithCookies(probeCtx, "GET", probeURL, cookies, nil)
	if err != nil {
		return true
	}
	defer body.Close()

	// 判据位于响应首部，只读少量字节，避免为探测拉取整个成绩/课表响应体
	raw, readErr := io.ReadAll(io.LimitReader(body, 1024))
	if readErr != nil {
		return true
	}

	if SessionLooksValid(string(raw)) {
		sessionProbeMarkOK(uid)
		return true
	}
	return false
}

func sessionProbeCached(uid int) (bool, time.Time) {
	sessionProbeState.mu.Lock()
	defer sessionProbeState.mu.Unlock()
	t, ok := sessionProbeState.ok[uid]
	return ok, t
}

func sessionProbeMarkOK(uid int) {
	sessionProbeState.mu.Lock()
	defer sessionProbeState.mu.Unlock()
	if len(sessionProbeState.ok) >= sessionProbeMaxEntries {
		sessionProbeState.ok = make(map[int]time.Time)
	}
	sessionProbeState.ok[uid] = time.Now()
}

// GetCookiesOrLoginEx 获取会话 cookies：优先复用缓存，缓存缺失时用库中凭据登录。
//
// 第二个返回值表示 cookies 是否来自缓存——缓存会话可能是陈旧的，
// 调用方可据此决定是否做有效性预检（见 EnsureSessionAlive）。
func GetCookiesOrLoginEx(ctx context.Context, ss SessionService, uid int, sid, spwd string) ([]*http.Cookie, bool, error) {
	cookies, err := ss.GetCachedCookies(ctx, uid)
	if err != nil {
		return nil, false, common.NewAppError(common.CodeCacheError, "缓存错误")
	}

	if len(cookies) > 0 {
		return cookies, true, nil
	}

	// 尝试登录教务系统
	if err := ss.LoginAndCache(ctx, uid, sid, spwd); err != nil {
		// 密码错误等认证类失败 → 转换为"绑定已失效"，让前端提示重新输入密码
		return nil, false, common.ToBindExpired(err)
	}

	cookies, err = ss.GetCachedCookies(ctx, uid)
	if err != nil {
		return nil, false, common.NewAppError(common.CodeCacheError, "读取缓存失败")
	}
	if len(cookies) == 0 {
		// 登录声称成功了但缓存没有 cookies，
		// 说明教务系统返回了 302 但目标系统不可达（例如校园网外访问教务系统）
		return nil, false, common.NewAppError(common.CodeJwcRequestFailed, "教务系统网络连接异常，请稍后重试")
	}

	return cookies, false, nil
}

// EnsureSessionAlive 获取**可用**的教务会话，是各数据查询统一的取会话入口。
//
// 与直接读缓存的区别：命中缓存的会话会先探测一次，确认教务端仍然认它。
// 失效时清除缓存并用库中密码重新登录（必要时走多因素认证），
// 仍不可用则返回认证类错误，由上层引导用户重新输入教务密码——
// 而不是带着坏 cookie 继续请求，把"未登录"响应当成"无数据"。
//
// probeURL 传各模块自己的数据接口即可（会话是同一个，用哪个接口探测都行）。
func EnsureSessionAlive(ctx context.Context, ss SessionService, crawler CrawlerService, uid int, sid, spwd, probeURL string) ([]*http.Cookie, error) {
	cookies, fromCache, err := GetCookiesOrLoginEx(ctx, ss, uid, sid, spwd)
	if err != nil {
		return nil, err
	}
	if !fromCache {
		// 刚登录拿到的会话必然新鲜，无需多花一次往返
		return cookies, nil
	}

	if ProbeSessionAlive(ctx, uid, crawler, probeURL, cookies) {
		return cookies, nil
	}

	log.Printf("[session] uid=%d 缓存会话已失效，清除后重新登录", uid)
	if invErr := ss.InvalidateSession(ctx, uid); invErr != nil {
		log.Printf("[session] uid=%d 清除会话缓存失败: %v", uid, invErr)
	}

	cookies, _, err = GetCookiesOrLoginEx(ctx, ss, uid, sid, spwd)
	if err != nil {
		return nil, err
	}
	if !ProbeSessionAlive(ctx, uid, crawler, probeURL, cookies) {
		// 重新登录后教务端仍不认可：多为教务侧异常或密码已变更。
		// 返回认证类错误，前端据此引导用户重新输入教务密码（可触发短信验证）。
		return nil, common.NewAppError(common.CodeJwcSessionExpired,
			"教务系统登录状态异常，请重新输入教务密码后重试")
	}
	return cookies, nil
}
