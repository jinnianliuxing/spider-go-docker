package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"spider-go/internal/cache"
	"spider-go/internal/common"

	"golang.org/x/net/publicsuffix"
)

// ============================================================================
// i中南林 App 扫码登录（CAS 二维码）
// ============================================================================
//
// 真实流程（来自 2026-09-21 HAR 实测，共 8 步）：
//
//	1. GET  {cas}/cas/login?service=...                 → 200 HTML，种下 SESSION cookie
//	2. GET  {cas}/cas/jwt/publicKey                     → RSA 公钥（本项目 RSAKeyService 已有）
//	3. GET  {cas}/cas/qr/qrcode?r=<随机>                 → image/png 二维码
//	4. POST {cas}/cas/qr/comet                          → 长轮询，返回 status 1/2/3
//	5. POST {cas}/cas/login  (body: qrCodeKey + currentMenu=3) → 302 + TGC
//	6. GET  callback?ticket=ST-...                      → 200
//	7. POST {webvpn}/api/access/auth/finish             → webvpn-token（本项目的 exchangeWebVPNToken）
//	8. GET  {webvpn}/api/access/user/info               → 学号/姓名
//
// 步骤 6/7/8 本项目已有等价实现，本文件负责 1~5。
//
// ⚠️ 与密码登录的关键差异：扫码**永远拿不到教务密码**。
// 所以扫码绑定的用户其 users.spwd 会是空串，会话过期后无法用密码自动重登，
// 只能让用户重新扫码（这正是产品上接受的代价，见需求：会话过期不要紧，该扫码就扫码）。
//
// status 语义（HAR 中 9 次 comet 轮询实测归纳）：
//
//	1 = 待扫描（二维码已生成，等待手机扫）
//	2 = 已扫描，等待手机端点确认
//	3 = 已确认（apptoken 已签发，可以用 stateKey 换 TGC 了）
//	0 = 本次轮询中断/无更新（继续轮询即可）

const (
	// QrStatusWaiting 等待手机扫码
	QrStatusWaiting = 1
	// QrStatusScanned 已扫码，等待手机确认
	QrStatusScanned = 2
	// QrStatusConfirmed 手机已确认，可换票
	QrStatusConfirmed = 3
	// QrStatusInterrupted comet 本次无更新（HAR 中 entry 11/12 返回 0）
	QrStatusInterrupted = 0
)

// qrSessionTTL 扫码会话有效期。二维码本身很短命（HAR 里前端刷得很勤），
// 这里给 5 分钟：够用户抬手扫码，超时则前端重新出码。
const qrSessionTTL = 5 * time.Minute

// QrLoginService 扫码登录服务
//
// ⚠️ 所有会话级方法都要求传 uid，内部校验"这个 sessionID 是不是这个用户创建的"。
// 校验必须在这里做而不是 handler 里 —— 否则将来任何新的调用方（比如后台任务、
// 第二批接口）都可能漏掉校验，造成 A 扫的码被 B 换成自己的登录态。
type QrLoginService interface {
	// Start 生成二维码。返回 PNG 字节与本次扫码会话的 sessionID（前端凭它轮询）。
	Start(ctx context.Context, uid int) (png []byte, sessionID string, err error)
	// Poll 长轮询一次扫码状态。返回 status(0/1/2/3) 与提示文案。
	Poll(ctx context.Context, sessionID string, uid int) (status int, message string, err error)
	// Complete 手机确认后完成登录，返回学号/姓名/客户端。
	Complete(ctx context.Context, sessionID string, uid int) (*QrLoginResult, error)
}

// QrLoginResult 扫码登录结果
type QrLoginResult struct {
	Sid      string       // 学号（apptoken 的 ATTR_userNo）
	Name     string       // 姓名（apptoken 的 ATTR_name）
	TGC      *http.Cookie // CAS 全局票据；评教等子系统靠它换 ticket，nil 表示本次未取到
	Client   *http.Client // 已带 webvpn-token 的客户端（评教等模块需要 jar）
	Identity string       // 身份类别，如「本科生」
	Org      string       // 院系班级，如「2023森林保护(林业特岗)3班」
}

// qrLoginService 扫码登录实现
type qrLoginService struct {
	sessionCache   cache.SessionCache
	rsaKeyService  RSAKeyService
	casLoginURL    string // webvpn 反代域的 CAS login_url（含 service 参数）
	webvpnTokenURL string // auth/finish
	timeout        time.Duration
}

// NewQrLoginService 创建扫码登录服务
func NewQrLoginService(
	sessionCache cache.SessionCache,
	rsaKeyService RSAKeyService,
	casLoginURL string,
	webvpnTokenURL string,
) QrLoginService {
	return &qrLoginService{
		sessionCache:   sessionCache,
		rsaKeyService:  rsaKeyService,
		casLoginURL:    casLoginURL,
		webvpnTokenURL: webvpnTokenURL,
		timeout:        30 * time.Second,
	}
}

// casBase 从 login_url 推导 CAS 基址（保留 webvpn 反代域，勿替换成原始 cas.csuft.edu.cn）
func (s *qrLoginService) casBase() (string, error) {
	u, err := url.Parse(s.casLoginURL)
	if err != nil {
		return "", common.NewAppError(common.CodeJwcParseFailed, "解析 CAS 登录地址失败")
	}
	return u.Scheme + "://" + u.Host, nil
}

// resolveLoginURL 返回本次扫码应当使用的 CAS 登录地址。
//
// ⚠️ 与 session_service.go 的 resolveLoginURL 同因同治：
// 学校会**轮换 CAS 认证方式的 externalId**，config 里硬编码的 login_url 中的
// externalId（如 eAF0IG5N）会过期。若直接拿它去 CAS 换票，虽然 CAS 能返回 ticket，
// 但 WebVPN 的 auth/finish 会因「externalId 不是它当前登记的那个」而报
// `{"code":20012,"message":"找不到认证方式"}`。
//
// 浏览器真实流程是：authentication/all 拿 externalId → auth/start 拿 login_url
// → CAS 认证 → callback?ticket → auth/finish。本方法复刻前三步。
//
// 失败时回退到配置值（与密码绑定一致的降级策略），避免因 auth/start 抖动直接不可用。
func (s *qrLoginService) resolveLoginURL(ctx context.Context, client *http.Client) string {
	if s.webvpnTokenURL == "" {
		return s.casLoginURL
	}
	dynamicURL, err := webvpnStartAuthSession(ctx, client, s.webvpnTokenURL)
	if err != nil {
		log.Printf("[QrLogin] auth/start 失败，回退到配置的 login_url: %v", err)
		return s.casLoginURL
	}
	return dynamicURL
}

// startWebVPNAuthSession 调 WebVPN auth/start 创建认证会话，返回本次登录的 CAS login_url。
// 逻辑与 jwcSessionService.startWebVPNAuthSession 一致，只是作用于本服务自己的 client。
// serviceParam 从给定的 login_url 中取出 service 参数，/cas/login 提交时要原样带上。
//
// ⚠️ 必须传**会话里存的那份动态 login_url**，不能用配置里的静态值 ——
// 动态值里的 service 才是与本次 auth/start 会话匹配的那个。
func (s *qrLoginService) serviceParam(loginURL string) string {
	u, err := url.Parse(loginURL)
	if err != nil {
		return ""
	}
	return u.Query().Get("service")
}

// newQrClient 创建带 cookie jar 的客户端。
// ⚠️ 必须用 jar 保持 SESSION cookie，否则 comet 轮询会认不出会话。
func (s *qrLoginService) newQrClient() (*http.Client, error) {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, common.NewAppError(common.CodeInternalError, "创建扫码会话失败")
	}
	return &http.Client{
		Jar:     jar,
		Timeout: s.timeout,
		// 必须手动处理 302：要读 Location 里的 ticket，不能让库自动跟随
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

// Start 生成二维码
func (s *qrLoginService) Start(ctx context.Context, uid int) ([]byte, string, error) {
	base, err := s.casBase()
	if err != nil {
		return nil, "", err
	}
	client, err := s.newQrClient()
	if err != nil {
		return nil, "", err
	}

	// ⚠️ 必须先动态解析 login_url：配置里的 externalId 会过期，
	// 用过期值换来的 ticket 会在 WebVPN auth/finish 处报 20012「找不到认证方式」。
	loginURL := s.resolveLoginURL(ctx, client)

	// 步骤 1：GET CAS 登录页，种下 SESSION cookie
	// 这一步不能省：没有 SESSION，qrcode 与 comet 都会失败。
	res, err := client.Get(loginURL)
	if err != nil {
		if isTimeoutError(err) {
			return nil, "", common.NewAppError(common.CodeJwcLoginTimeout, "连接教务认证服务超时")
		}
		return nil, "", common.NewAppError(common.CodeJwcRequestFailed, "连接教务认证服务失败")
	}
	io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	res.Body.Close()

	// 步骤 3：取二维码 PNG。r 只是防缓存随机数，随便给。
	qrURL := fmt.Sprintf("%s/cas/qr/qrcode?r=%d", base, time.Now().UnixNano()/1e6)
	qrResp, err := client.Get(qrURL)
	if err != nil {
		if isTimeoutError(err) {
			return nil, "", common.NewAppError(common.CodeJwcLoginTimeout, "获取二维码超时")
		}
		return nil, "", common.NewAppError(common.CodeJwcRequestFailed, "获取二维码失败")
	}
	defer qrResp.Body.Close()
	if qrResp.StatusCode != http.StatusOK {
		return nil, "", common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("获取二维码返回异常: %d", qrResp.StatusCode))
	}
	png, err := io.ReadAll(io.LimitReader(qrResp.Body, 4<<20))
	if err != nil || len(png) == 0 {
		return nil, "", common.NewAppError(common.CodeJwcParseFailed, "二维码内容为空")
	}

	// ⚠️ 关键：客户端（含 SESSION cookie jar）必须持久化。
	// 后续 comet 轮询与 /cas/login 换票都要复用同一份 cookie，
	// 所以这里把「整个 client 的 cookie」存进 Redis，下一步重建 client 时灌回去。
	sessionID, err := s.persistQrSession(ctx, uid, client, base, loginURL)
	if err != nil {
		return nil, "", err
	}
	log.Printf("[QrLogin] 二维码已生成, uid=%d, sessionID=%s, size=%d", uid, sessionID, len(png))
	return png, sessionID, nil
}

// qrSessionState 落库的扫码会话状态别名（实际结构定义在 cache 包，避免循环依赖）
type qrSessionState = cache.QrSessionState

// persistQrSession 把当前 cookie 存进 Redis，返回 sessionID。
// loginURL 必须是**本次动态解析**出来的地址（含有效的 externalId），
// 后续 Complete 换票要原样提交它的 service 参数。
func (s *qrLoginService) persistQrSession(ctx context.Context, uid int, client *http.Client, base, loginURL string) (string, error) {
	sessionID, err := randomToken()
	if err != nil {
		return "", common.NewAppError(common.CodeInternalError, "生成扫码会话标识失败")
	}
	// ⚠️ 关键：cookie 必须按「会带上它的请求 URL」来查，否则查不到。
	// SESSION cookie 的 Path 是 /cas，若用 https://host（路径为空）去查，
	// jar 会按 RFC6265 路径匹配规则判定不适用 → 返回空集合。
	// 详见 collectJarCookies。
	cookies := collectJarCookies(client, base)
	state := &qrSessionState{
		UID:       uid,
		Base:      base,
		Cookies:   cookies,
		FpVisitor: mustRandomToken(),
		LoginURL:  loginURL,
	}
	if err := s.sessionCache.SetQrSession(ctx, sessionID, state, qrSessionTTL); err != nil {
		return "", common.NewAppError(common.CodeCacheError, "保存扫码会话失败")
	}
	return sessionID, nil
}

// collectJarCookies 取出 jar 中所有与 CAS 站点相关的 cookie（去重）。
//
// ⚠️ 坑：cookiejar 的 Cookies(u) 按 RFC6265 做「路径匹配」——只有 cookie 的 Path
// 是请求路径的前缀时才会被返回。CAS 下发的 SESSION cookie Path 是 /cas，
// 若直接传 https://host（路径为空）会拿到空集合，导致 comet 轮询因缺 SESSION 返回
// {"code":1,"message":"expired"}。因此这里按若干个真实会走到的路径分别查询后合并。
func collectJarCookies(client *http.Client, base string) []*http.Cookie {
	if client == nil || client.Jar == nil {
		return nil
	}
	paths := []string{"/", "/cas", "/cas/login", "/cas/qr", "/cas/qr/qrcode", "/cas/qr/comet"}
	seen := make(map[string]bool)
	out := make([]*http.Cookie, 0, 4)
	for _, p := range paths {
		u, err := url.Parse(base + p)
		if err != nil {
			continue
		}
		for _, c := range client.Jar.Cookies(u) {
			key := c.Name + "\x00" + c.Domain + "\x00" + c.Path
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, c)
		}
	}
	return out
}

// loadQrClient 从 Redis 取回扫码会话并重建带 cookie 的 client。
// uid 用于归属校验：sessionID 属于别人的一律拒绝，防止串号。
func (s *qrLoginService) loadQrClient(ctx context.Context, sessionID string, uid int) (*http.Client, *qrSessionState, error) {
	st, err := s.sessionCache.GetQrSession(ctx, sessionID)
	if err != nil {
		return nil, nil, common.NewAppError(common.CodeCacheError, "读取扫码会话失败")
	}
	if st == nil {
		return nil, nil, common.NewAppError(common.CodeJwcLoginFailed, "二维码已过期，请刷新后重试")
	}
	// 🔒 归属校验：这一步是防串号的关键，绝不能省
	if st.UID != uid {
		log.Printf("[QrLogin] 扫码会话归属不匹配: sessionID=%s, 会话归属uid=%d, 请求uid=%d", sessionID, st.UID, uid)
		return nil, nil, common.NewAppError(common.CodeJwcLoginFailed, "扫码会话不属于当前用户")
	}
	client, err := s.newQrClient()
	if err != nil {
		return nil, nil, err
	}
	// 把 cookie 灌回 jar（含 SESSION）。按每条 cookie 自己的 Path 逐条设置，
	// 保证恢复后的 Path/Domain 与服务端下发的完全一致（详见 restoreJarCookies）。
	restoreJarCookies(client, st.Base, st.Cookies)
	return client, st, nil
}

// Poll 长轮询扫码状态
func (s *qrLoginService) Poll(ctx context.Context, sessionID string, uid int) (int, string, error) {
	client, st, err := s.loadQrClient(ctx, sessionID, uid)
	if err != nil {
		return 0, "", err
	}

	// 步骤 4：POST /cas/qr/comet
	// ⚠️ 必须带 X-Requested-With，且 Content-Length 为 0（无 body），否则 CAS 不认。
	cometURL := st.Base + "/cas/qr/comet"
	req, err := http.NewRequestWithContext(ctx, "POST", cometURL, nil)
	if err != nil {
		return 0, "", common.NewAppError(common.CodeInternalError, "构造轮询请求失败")
	}
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", st.LoginURL)
	req.Header.Set("Content-Length", "0")

	resp, err := client.Do(req)
	if err != nil {
		if isTimeoutError(err) {
			// 长轮询超时是正常的：CAS 会在无更新时挂起请求，超时后前端再轮一次即可
			return QrStatusInterrupted, "等待扫码…", nil
		}
		return 0, "", common.NewAppError(common.CodeJwcRequestFailed, "轮询扫码状态失败")
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var result struct {
		Code int `json:"code"`
		Data *struct {
			QrCode *struct {
				Status   string      `json:"status"`
				AppToken interface{} `json:"apptoken"`
			} `json:"qrCode"`
			StateKey string `json:"stateKey"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		// CAS 偶尔返回空白（长轮询断开），当作"无更新"继续轮询，不报错打断用户
		return QrStatusInterrupted, "等待扫码…", nil
	}
	if result.Code != 0 || result.Data == nil || result.Data.QrCode == nil {
		// code=1/message=expired 表示会话已失效（cookie 丢失或二维码过期），
		// 返回"中断"让前端提示刷新，不当作硬错误。
		return QrStatusInterrupted, "等待扫码…", nil
	}

	status := parseQrStatus(result.Data.QrCode.Status)

	// stateKey 是换票时的 qrCodeKey，必须存下来
	if result.Data.StateKey != "" && result.Data.StateKey != st.StateKey {
		st.StateKey = result.Data.StateKey
		_ = s.sessionCache.SetQrSession(ctx, sessionID, st, qrSessionTTL)
	}

	switch status {
	case QrStatusWaiting:
		return status, "请用「i中南林」App 扫描二维码", nil
	case QrStatusScanned:
		return status, "已扫描，请在手机上确认登录", nil
	case QrStatusConfirmed:
		return status, "已确认，正在登录…", nil
	default:
		return QrStatusInterrupted, "等待扫码…", nil
	}
}

// parseQrStatus 把 comet 返回的 status 字符串转成 int。
// HAR 中 status 是字符串 "1"/"2"/"3"，但也见过裸数字，都兼容。
func parseQrStatus(s string) int {
	switch strings.TrimSpace(s) {
	case "1":
		return QrStatusWaiting
	case "2":
		return QrStatusScanned
	case "3":
		return QrStatusConfirmed
	default:
		return QrStatusInterrupted
	}
}

// Complete 手机确认后完成登录：用 qrCodeKey 换 TGC，再走 webvpn 令牌交换，最后取学号姓名。
func (s *qrLoginService) Complete(ctx context.Context, sessionID string, uid int) (*QrLoginResult, error) {
	client, st, err := s.loadQrClient(ctx, sessionID, uid)
	if err != nil {
		return nil, err
	}
	if st.StateKey == "" {
		return nil, common.NewAppError(common.CodeJwcLoginFailed, "尚未完成扫码确认，请先扫码")
	}

	// 步骤 5：POST /cas/login，body 换成 qrCodeKey（**不再需要 username/password**）
	form := url.Values{
		"qrCodeKey":   {st.StateKey},
		"currentMenu": {"3"},
		"geolocation": {""},
		"fpVisitorId": {st.FpVisitor},
		"trustAgent":  {""},
	}
	svc := s.serviceParam(st.LoginURL)
	if svc != "" {
		form.Set("service", svc)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", st.LoginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, common.NewAppError(common.CodeInternalError, "构造扫码登录请求失败")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", st.LoginURL)

	resp, err := client.Do(req)
	if err != nil {
		if isTimeoutError(err) {
			return nil, common.NewAppError(common.CodeJwcLoginTimeout, "扫码登录请求超时")
		}
		return nil, common.NewAppError(common.CodeJwcRequestFailed, "扫码登录请求失败")
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		// CAS 返回 200 = 没通过，通常是二维码已过期或已被用过
		return nil, common.NewAppError(common.CodeJwcLoginFailed, "扫码登录未通过，二维码可能已过期，请刷新重试")
	}
	if resp.StatusCode != http.StatusFound {
		if resp.StatusCode >= 500 {
			return nil, common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("教务认证服务错误: %d", resp.StatusCode))
		}
		return nil, common.NewAppError(common.CodeJwcLoginFailed, fmt.Sprintf("扫码登录响应异常: %d", resp.StatusCode))
	}

	// 拿到 302 Location（含 ticket），并从 jar 抓 TGC
	loginLocation := resp.Header.Get("Location")
	if loginLocation == "" {
		return nil, common.NewAppError(common.CodeJwcLoginFailed, "扫码登录未返回跳转地址")
	}
	tgc := extractTGCCookie(client, st.LoginURL)

	// 步骤 6+7：走 WebVPN 令牌交换（与密码/手机号绑定共用同一实现）
	if err := webvpnExchangeToken(ctx, client, s.webvpnTokenURL, loginLocation); err != nil {
		return nil, err
	}

	// 步骤 8：取用户信息，拿学号/姓名
	info, err := webvpnFetchUserInfo(ctx, client, s.webvpnTokenURL)
	if err != nil {
		return nil, err
	}
	if info.Nickname == "" && info.FullName == "" {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "未能从扫码结果中解析出用户身份")
	}

	log.Printf("[QrLogin] 扫码登录成功: sid=%s name=%s identity=%s", info.Nickname, info.FullName, info.IdentityTypeName)

	// TGC 一并返回给上层落库：评教模块需要拿它去 CAS 换 ticket，
	// 否则扫码用户（无密码）无法访问教评系统。
	if tgc != nil {
		log.Printf("[QrLogin] 已获取 TGC, 将随结果返回供上层缓存, path=%s", tgc.Path)
	}

	return &QrLoginResult{
		Sid:      info.Nickname, // user/info 的 nickname 即学号
		Name:     info.FullName, // fullName 即姓名
		TGC:      tgc,           // CAS 全局票据，评教等子系统需要
		Client:   client,
		Identity: info.IdentityTypeName,
		Org:      info.OrganizationName,
	}, nil
}

// fetchWebVPNUserInfo 取当前 webvpn 会话对应的用户信息
// exchangeWebVPNTokenForQr 与 session_service.go 的 exchangeWebVPNToken 同逻辑，
// 但作用于本文件自己的 client。抽出来是因为那边是 jwcSessionService 的方法。
// extractTGC 从 cookie jar 里找 TGC
// extractTGC 从 jar 中取出 CAS 全局票据（TGC）。
//
// ⚠️ Go 的 cookiejar 在 Cookies() 返回时**不带 Domain/Path**（字段为空）。
// 若把这种"裸" cookie 交给评教模块手工回灌（SetCookies），
// jar 会把 Domain 设成调用方传入的 host、Path 设成 "/"，
// 导致 TGC 被发到错误的域/路径 → 教评系统拿不到 ticket（表现为「未能获取 userToken」）。
// 这里就地补全 Domain/Path，落 Redis 后即可原样回灌。
// randomToken 生成 32 字节随机 hex
func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// mustRandomToken 尽力生成，失败时返回基于时间的兜底值（仅用于非关键指纹字段）
func mustRandomToken() string {
	if t, err := randomToken(); err == nil {
		return t
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// mustRandomHex32 生成 32 位 hex 字符串，供 CAS 的 fpVisitorId 用
// （浏览器侧实测就是 32 位 hex；失败时兜底成 32 位左填零的时间戳）。
func mustRandomHex32() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("%032d", time.Now().UnixNano()%1000000000000000000)
}
