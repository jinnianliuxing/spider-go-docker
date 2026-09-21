package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"spider-go/internal/cache"
	"spider-go/internal/common"

	"github.com/PuerkitoBio/goquery"
)

// ============================================================================
// 手机号验证码绑定（i中南林 App / CAS 免密短信登录）
// ============================================================================
//
// 真实流程（来自 2026-09-21 HAR 实测，域名 https-cas-csuft-edu-cn-443.webvpn.csuft.edu.cn）：
//
//	1. GET  {cas}/cas/login?service=...            → 200 HTML，种下 SESSION cookie
//	2. 从登录页第 2 个表单（currentMenu=2）取 execution
//	3. POST {cas}/cas/passwordlessTokenSend        → 发短信（body: username=<手机号>&captcha=）
//	4. POST {cas}/cas/login?service=...            → 302 + Location(ticket) + Set-Cookie: TGC
//	      body: username&password=<短信码>&captcha=&currentMenu=2&failN=-1
//	            &execution&_eventId=submitPasswordlessToken&geolocation&fpVisitorId&trustAgent&submit2
//	5. GET  callback?ticket=...                    → 200
//	6. POST {webvpn}/api/access/auth/finish        → webvpn-token
//	7. GET  {webvpn}/api/access/user/info          → 学号(nickname) / 姓名(fullName)
//
// 步骤 5~7 与密码/扫码绑定完全一致，复用 webvpn_session.go 的公共实现，本文件负责 1~4。
//
// ⚠️ 与密码登录的两个关键差异（照抄密码流程必踩）：
//  1. **短信码是明文提交**（HAR: password=456741），不做 RSA 加密；
//  2. 登录页有 4 个 form / 3 个 execution，必须取 **currentMenu=2**（手机表单）那一份。
//
// ⚠️ 与扫码绑定的差异：手机绑定同样拿不到教务密码（spwd 恒空），
// 但会话过期后用户**只要再补一次短信验证码**即可恢复（不必重新绑定），
// 这是本项目选择它的原因。绑定成功后仍无法自动重登 —— 见 BindModePhone 注释。

const (
	// phoneSessionTTL 手机号验证码会话有效期。短信码本身通常 5 分钟有效，对齐即可。
	phoneSessionTTL = 5 * time.Minute
	// phoneSMSCodeField 短信验证码在登录表单里的字段名（CAS 复用了 password 字段）
	phoneSMSCodeField = "password"
)

// phoneRegex 中国大陆手机号
var phoneRegex = regexp.MustCompile(`^1[3-9]\d{9}$`)

// PhoneLoginService 手机号验证码登录服务
//
// ⚠️ 与 QrLoginService 一样，会话级方法要求传 uid，内部校验"这个 sessionID 是不是这个用户创建的"，
// 防止 A 发起的验证码被 B 拿去换登录态。
type PhoneLoginService interface {
	// Start 发送短信验证码，返回本次验证会话的 sessionID、脱敏手机号与教务端原话提示。
	Start(ctx context.Context, uid int, phone string) (*PhoneSendResult, error)
	// Complete 提交短信验证码，完成 CAS 登录并返回学号/姓名/客户端。
	Complete(ctx context.Context, sessionID string, uid int, code string) (*PhoneLoginResult, error)
}

// PhoneSendResult 发码结果
//
// ⚠️ Hint 一律来自教务端原话。实测 CAS 对**任何**手机号（含根本不存在的号码）
// 都返回 HTTP 200 + `{"data":{"success":"短信可能会存在延迟或手机未绑定用户"}}`,
// 也就是**它无法告诉我们这个号到底有没有绑定 i中南林 账号**，
// 因此这句「可能收不到」的提示必须原样透给用户，否则用户会一直干等短信。
type PhoneSendResult struct {
	SessionID   string
	MaskedPhone string
	Hint        string
}

// PhoneLoginResult 手机号验证码登录结果
type PhoneLoginResult struct {
	Sid      string       // 学号（user/info 的 nickname）
	Name     string       // 姓名（user/info 的 fullName）
	Phone    string       // 本次登录使用的手机号
	TGC      *http.Cookie // CAS 全局票据；评教等子系统靠它换 ticket，nil 表示本次未取到
	Client   *http.Client // 已带 webvpn-token 的客户端（评教等模块需要 jar）
	Identity string       // 身份类别，如「本科生」
	Org      string       // 院系班级
}

type phoneLoginService struct {
	sessionCache   cache.SessionCache
	casLoginURL    string // webvpn 反代域的 CAS login_url（含 service 参数）
	webvpnTokenURL string // auth/finish
	timeout        time.Duration
}

// NewPhoneLoginService 创建手机号验证码登录服务
func NewPhoneLoginService(
	sessionCache cache.SessionCache,
	casLoginURL string,
	webvpnTokenURL string,
) PhoneLoginService {
	return &phoneLoginService{
		sessionCache:   sessionCache,
		casLoginURL:    casLoginURL,
		webvpnTokenURL: webvpnTokenURL,
		timeout:        30 * time.Second,
	}
}

// resolveLoginURL 返回本次登录应当使用的 CAS 登录地址。
//
// 与扫码/密码一致：学校会轮换 CAS 认证方式的 externalId，config 里硬编码的 login_url
// 会过期；先走 auth/start 动态取，失败才回退配置值。
func (s *phoneLoginService) resolveLoginURL(ctx context.Context, client *http.Client) string {
	if s.webvpnTokenURL == "" {
		return s.casLoginURL
	}
	dynamicURL, err := webvpnStartAuthSession(ctx, client, s.webvpnTokenURL)
	if err != nil {
		log.Printf("[PhoneLogin] auth/start 失败，回退到配置的 login_url: %v", err)
		return s.casLoginURL
	}
	return dynamicURL
}

// casBase 从 login_url 推导 CAS 基址（保留 webvpn 反代域，勿替换成原始 cas.csuft.edu.cn）
func casBase(loginURL string) (string, error) {
	u, err := url.Parse(loginURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", common.NewAppError(common.CodeJwcParseFailed, "解析 CAS 登录地址失败")
	}
	return u.Scheme + "://" + u.Host, nil
}

// extractSMSFormExecution 从 CAS 登录页 HTML 中取「手机表单」的 execution。
//
// 🔥 登录页同时渲染 4 个表单（drcom / 密码 currentMenu=1 / 手机 currentMenu=2 / 扫码 currentMenu=3），
// 因此存在 3 个 name="execution" 的 hidden input。本次 HAR 实测三者取值相同（共享一个流程令牌），
// 但**绝不能依赖这个巧合**——必须按 currentMenu=2（或 _eventId=submitPasswordlessToken）定位手机表单。
//
// 兜底顺序：手机表单 → _eventId=submitPasswordlessToken 的表单 → 页面上第一个 execution。
func extractSMSFormExecution(doc *goquery.Document) string {
	if exec := doc.Find("form:has(input[name='currentMenu'][value='2'])").Find("input[name='execution']").First().AttrOr("value", ""); exec != "" {
		return exec
	}
	if exec := doc.Find("form:has(input[name='_eventId'][value='submitPasswordlessToken'])").Find("input[name='execution']").First().AttrOr("value", ""); exec != "" {
		return exec
	}
	return doc.Find("input[name='execution']").First().AttrOr("value", "")
}

// maskPhone 脱敏手机号：18797306640 → 187****6640
func maskPhone(phone string) string {
	if len(phone) != 11 {
		return phone
	}
	return phone[:3] + "****" + phone[7:]
}

// Start 发送短信验证码并建立本次验证会话。
func (s *phoneLoginService) Start(ctx context.Context, uid int, phone string) (*PhoneSendResult, error) {
	phone = strings.TrimSpace(phone)
	if !phoneRegex.MatchString(phone) {
		return nil, common.NewAppError(common.CodeInvalidParams, "请输入正确的 11 位手机号")
	}
	if s.casLoginURL == "" {
		return nil, common.NewAppError(common.CodeInternalError, "CAS 登录地址未配置")
	}

	client, err := newCASClient(s.timeout)
	if err != nil {
		return nil, err
	}

	// 步骤 1：GET CAS 登录页 —— 必须做，登录页的 SESSION cookie 是后续发码/换票的前提
	loginURL := s.resolveLoginURL(ctx, client)
	res, err := client.Get(loginURL)
	if err != nil {
		if isTimeoutError(err) {
			return nil, common.NewAppError(common.CodeJwcLoginTimeout, "连接教务认证服务超时")
		}
		return nil, common.NewAppError(common.CodeJwcRequestFailed, "连接教务认证服务失败")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, common.NewAppError(common.CodeJwcLoginFailed,
			fmt.Sprintf("获取登录页返回异常: %d", res.StatusCode))
	}
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "解析登录页面失败")
	}
	execution := extractSMSFormExecution(doc)
	if execution == "" {
		return nil, common.NewAppError(common.CodeJwcLoginFailed, "登录页未找到手机验证表单，学校认证页可能已改版")
	}

	base, err := casBase(loginURL)
	if err != nil {
		return nil, err
	}

	// 步骤 3：发短信
	// ⚠️ 必须带 X-Requested-With，否则 CAS 不认这是 AJAX 调用。
	sendURL := base + "/cas/passwordlessTokenSend"
	form := url.Values{"username": {phone}, "captcha": {""}}
	req, err := http.NewRequestWithContext(ctx, "POST", sendURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, common.NewAppError(common.CodeInternalError, "构造发送验证码请求失败")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Origin", base)
	req.Header.Set("Referer", loginURL)

	sendResp, err := client.Do(req)
	if err != nil {
		if isTimeoutError(err) {
			return nil, common.NewAppError(common.CodeJwcLoginTimeout, "发送验证码超时")
		}
		return nil, common.NewAppError(common.CodeJwcRequestFailed, "发送验证码请求失败")
	}
	defer sendResp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(sendResp.Body, 1<<20))
	hint, err := parsePasswordlessSendResult(sendResp.StatusCode, body)
	if err != nil {
		return nil, err
	}

	// 保存会话（含 SESSION cookie + execution），供 Complete 换票
	sessionID, err := randomToken()
	if err != nil {
		return nil, common.NewAppError(common.CodeInternalError, "生成验证会话标识失败")
	}
	state := &cache.PhoneSessionState{
		UID:       uid,
		Phone:     phone,
		Base:      base,
		Cookies:   collectJarCookies(client, base),
		LoginURL:  loginURL,
		Execution: execution,
		FpVisitor: mustRandomHex32(),
	}
	if err := s.sessionCache.SetPhoneSession(ctx, sessionID, state, phoneSessionTTL); err != nil {
		return nil, common.NewAppError(common.CodeCacheError, "保存验证会话失败")
	}
	log.Printf("[PhoneLogin] 验证码已发送: uid=%d phone=%s sessionID=%s hint=%q",
		uid, maskPhone(phone), sessionID, hint)
	return &PhoneSendResult{SessionID: sessionID, MaskedPhone: maskPhone(phone), Hint: hint}, nil
}

// parsePasswordlessSendResult 解析 /cas/passwordlessTokenSend 的响应。
//
// 实测成功响应形如：{"data":{"success":"短信可能会存在延迟或手机未绑定用户"}}
// —— **成功时没有 code 字段**，且 `success` 是**提示文案字符串**而非布尔值。
// ⚠️ 实测把号码换成 13800000000（根本不存在）也照样返回这句 success，
// 说明**教务端不校验号码是否绑定**，所以这句提示只能原样透给用户当"可能收不到"的预警。
// 失败形态未抓到，因此按"有 success 即成功"判定，其余情况尽量把服务端给的 code/message/error 透出来。
// 返回的 hint 即教务端原话，可能为空。
func parsePasswordlessSendResult(statusCode int, body []byte) (string, error) {
	if statusCode != http.StatusOK {
		return "", common.NewAppError(common.CodeJwcRequestFailed,
			fmt.Sprintf("发送验证码返回异常: %d", statusCode))
	}
	var r struct {
		Code    *int   `json:"code"`
		Message string `json:"message"`
		Data    *struct {
			Success string `json:"success"`
			Error   string `json:"error"`
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		log.Printf("[PhoneLogin] 发码响应无法解析: %.300s", string(body))
		return "", common.NewAppError(common.CodeJwcParseFailed, "发送验证码响应无法解析")
	}
	if (r.Code == nil || *r.Code == 0) && r.Data != nil && r.Data.Success != "" {
		return strings.TrimSpace(r.Data.Success), nil
	}
	msg := ""
	if r.Data != nil {
		msg = firstNonEmpty(r.Data.Error, r.Data.Message)
	}
	msg = firstNonEmpty(msg, r.Message)
	if msg == "" {
		msg = "验证码发送失败，请确认该手机号已绑定 i中南林 账号"
		log.Printf("[PhoneLogin] 发码失败且无提示文案: code=%v body=%.300s", r.Code, string(body))
	}
	return "", common.NewAppError(common.CodeJwcLoginFailed, msg)
}

// firstNonEmpty 返回第一个非空字符串
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// loadPhoneClient 从 Redis 取回验证会话并重建带 cookie 的 client。
// uid 用于归属校验：sessionID 属于别人的一律拒绝，防止串号。
func (s *phoneLoginService) loadPhoneClient(ctx context.Context, sessionID string, uid int) (*http.Client, *cache.PhoneSessionState, error) {
	st, err := s.sessionCache.GetPhoneSession(ctx, sessionID)
	if err != nil {
		return nil, nil, common.NewAppError(common.CodeCacheError, "读取验证会话失败")
	}
	if st == nil {
		return nil, nil, common.NewAppError(common.CodeJwcLoginFailed, "验证会话已过期，请重新获取验证码")
	}
	// 🔒 归属校验：防串号的关键，绝不能省
	if st.UID != uid {
		log.Printf("[PhoneLogin] 验证会话归属不匹配: sessionID=%s, 会话归属uid=%d, 请求uid=%d", sessionID, st.UID, uid)
		return nil, nil, common.NewAppError(common.CodeJwcLoginFailed, "验证会话不属于当前用户")
	}
	client, err := newCASClient(s.timeout)
	if err != nil {
		return nil, nil, err
	}
	restoreJarCookies(client, st.Base, st.Cookies)
	return client, st, nil
}

// deletePhoneSession 静默清理会话（失败只记日志，不影响主流程）
func (s *phoneLoginService) deletePhoneSession(ctx context.Context, sessionID string) {
	if err := s.sessionCache.DeletePhoneSession(ctx, sessionID); err != nil {
		log.Printf("[PhoneLogin] 清理验证会话失败: sessionID=%s err=%v", sessionID, err)
	}
}

// Complete 提交短信验证码：CAS 换票 → WebVPN 令牌交换 → 取学号姓名。
//
// 本方法只负责"拿到一次有效的教务登录态"，是否写绑定由上层（user 模块）决定 ——
// 于是它同时服务于两个场景：
//   - 首次/换绑定：上层校验学号一致后写 bind_mode=phone；
//   - 会话过期后补验证码：学号一致，上层只刷新会话缓存，不动绑定。
func (s *phoneLoginService) Complete(ctx context.Context, sessionID string, uid int, code string) (*PhoneLoginResult, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, common.NewAppError(common.CodeInvalidParams, "请输入短信验证码")
	}

	client, st, err := s.loadPhoneClient(ctx, sessionID, uid)
	if err != nil {
		return nil, err
	}

	// 步骤 4：POST /cas/login（currentMenu=2，短信码走 password 字段，**明文**）
	form := url.Values{
		"username":        {st.Phone},
		phoneSMSCodeField: {code},
		"captcha":         {""},
		"currentMenu":     {"2"},
		"failN":           {"-1"},
		"execution":       {st.Execution},
		"_eventId":        {"submitPasswordlessToken"},
		"geolocation":     {""},
		"fpVisitorId":     {st.FpVisitor},
		"trustAgent":      {""},
		"submit2":         {"Login2"},
	}
	// service 参数由 st.LoginURL 的 query 携带，body 里不再重复（与 HAR 一致）
	req, err := http.NewRequestWithContext(ctx, "POST", st.LoginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, common.NewAppError(common.CodeInternalError, "构造手机号登录请求失败")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Origin", st.Base)
	req.Header.Set("Referer", st.LoginURL)

	resp, err := client.Do(req)
	if err != nil {
		if isTimeoutError(err) {
			return nil, common.NewAppError(common.CodeJwcLoginTimeout, "手机号登录请求超时")
		}
		return nil, common.NewAppError(common.CodeJwcRequestFailed, "手机号登录请求失败")
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		// CAS 返回 200 = 没通过：验证码错误/已过期，或该手机号未绑定 i中南林 账号
		return nil, common.NewAppError(common.CodeJwcLoginFailed, "验证码错误或已过期，请重新获取")
	}
	if resp.StatusCode != http.StatusFound {
		if resp.StatusCode >= 500 {
			return nil, common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("教务认证服务错误: %d", resp.StatusCode))
		}
		return nil, common.NewAppError(common.CodeJwcLoginFailed, fmt.Sprintf("手机号登录响应异常: %d", resp.StatusCode))
	}

	loginLocation := resp.Header.Get("Location")
	if loginLocation == "" {
		return nil, common.NewAppError(common.CodeJwcLoginFailed, "手机号登录未返回跳转地址")
	}
	tgc := extractTGCCookie(client, st.LoginURL)

	// 步骤 5+6：WebVPN 令牌交换
	if err := webvpnExchangeToken(ctx, client, s.webvpnTokenURL, loginLocation); err != nil {
		return nil, err
	}
	// 步骤 7：取用户信息
	info, err := webvpnFetchUserInfo(ctx, client, s.webvpnTokenURL)
	if err != nil {
		return nil, err
	}
	if info.Nickname == "" && info.FullName == "" {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "未能从登录结果中解析出用户身份")
	}

	// 一次性会话：换票成功后立刻销毁，验证码不可重放
	s.deletePhoneSession(ctx, sessionID)

	log.Printf("[PhoneLogin] 手机号登录成功: phone=%s sid=%s name=%s", maskPhone(st.Phone), info.Nickname, info.FullName)
	if tgc == nil {
		log.Printf("[PhoneLogin] 本次未拿到 TGC，评教功能对该用户不可用: uid=%d", uid)
	}

	return &PhoneLoginResult{
		Sid:      info.Nickname, // user/info 的 nickname 即学号
		Name:     info.FullName, // fullName 即姓名
		Phone:    st.Phone,
		TGC:      tgc,
		Client:   client,
		Identity: info.IdentityTypeName,
		Org:      info.OrganizationName,
	}, nil
}
