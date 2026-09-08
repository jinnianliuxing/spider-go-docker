package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"spider-go/internal/cache"
	"spider-go/internal/common"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/publicsuffix"
)

type CookieCache interface {
	GetCookies(ctx context.Context, uid int) ([]*http.Cookie, error)
	SetCookies(ctx context.Context, uid int, cookies []*http.Cookie, expiration time.Duration) error
	DeleteCookies(ctx context.Context, uid int) error
	HasCookies(ctx context.Context, uid int) (bool, error)
}

type SessionService interface {
	LoginAndCache(ctx context.Context, uid int, username, password string) error
	GetCachedCookies(ctx context.Context, uid int) ([]*http.Cookie, error)
	InvalidateSession(ctx context.Context, uid int) error
	LoginAndCacheWithConfig(ctx context.Context, uid int, username, password string, loginURL, redirectURL string, cookieCache CookieCache) error
	LoginAndGetClient(ctx context.Context, username, password string) (*http.Client, error)
	LoginCheck(ctx context.Context, username, password string) error
	StartPhoneMFALogin(ctx context.Context, uid int, username, password string) (challengeID string, maskedPhone string, err error)
	CompletePhoneMFALogin(ctx context.Context, challengeID string, code string) error
}

type jwcSessionService struct {
	sessionCache    cache.SessionCache
	rsaKeyService   RSAKeyService
	mode            string
	loginURL        string
	redirectURL     string
	webvpnTokenURL  string
	mfaDetectURL    string
	captchaURL      string
	captchaImageURL string
	timeout         time.Duration
	cacheExpire     time.Duration
	tgcExpire       time.Duration

	pendingMFAMu sync.Mutex
	pendingMFA   map[string]*pendingMFASession
}

type pendingMFASession struct {
	uid         int
	username    string
	password    string
	client      *http.Client
	execution   string
	fpVisitorId string
	mfaState    string
	gid         string
	attestURL   string
	loginURL    string
	redirectURL string
	cookieCache CookieCache
	expiresAt   time.Time
}

func NewJwcSessionService(
	sessionCache cache.SessionCache,
	rsaKeyService RSAKeyService,
	mode string,
	loginURL string,
	redirectURL string,
	webvpnTokenURL string,
	mfaDetectURL string,
	captchaURL string,
	captchaImageURL string,
) SessionService {
	return &jwcSessionService{
		sessionCache:    sessionCache,
		rsaKeyService:   rsaKeyService,
		mode:            mode,
		loginURL:        loginURL,
		redirectURL:     redirectURL,
		webvpnTokenURL:  webvpnTokenURL,
		mfaDetectURL:    mfaDetectURL,
		captchaURL:      captchaURL,
		captchaImageURL: captchaImageURL,
		timeout:         30 * time.Second,
		cacheExpire:     time.Hour,
		pendingMFA:      make(map[string]*pendingMFASession),
	}
}

func (s *jwcSessionService) LoginAndCache(ctx context.Context, uid int, username, password string) error {
	var err error
	for i := 0; i < 1; i++ {
		if s.mode == "webvpn" {
			err = s.loginAndCacheOnceByWebVPN(ctx, uid, username, password)
		} else {
			err = s.loginAndCacheOnce(ctx, uid, username, password)
		}
		if err == nil {
			return nil
		}
		time.Sleep(time.Second * time.Duration(i+1))
	}
	if appErr, ok := err.(*common.AppError); ok {
		return common.NewAppError(appErr.Code, fmt.Sprintf("%s (已重试)", appErr.Message))
	}
	return common.NewAppError(common.CodeJwcLoginFailed, fmt.Sprintf("登录失败，请重试: %v", err))
}

func (s *jwcSessionService) loginAndCacheOnce(ctx context.Context, uid int, username, password string) error {
	return s.LoginAndCacheWithConfig(ctx, uid, username, password, s.loginURL, s.redirectURL, s.sessionCache)
}

func (s *jwcSessionService) followGET(client *http.Client, start string, maxHops int) (*http.Response, string, error) {
	cur := start
	var lastReqURL *url.URL
	for i := 0; i < maxHops; i++ {
		req, _ := http.NewRequest("GET", cur, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0")
		resp, err := client.Do(req)
		if err != nil {
			return nil, cur, err
		}
		if resp.StatusCode/100 != 3 {
			// 非 3xx 时，检测 JS 跳转（如 sso.jsp 返回 window.location.href 而非 HTTP 302）
			loc := resp.Header.Get("Location")
			if loc == "" {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
				_ = resp.Body.Close()
				loc = extractJSRedirect(string(body))
				if loc != "" {
					// 有 JS 跳转，继续跟随
					if lastReqURL == nil {
						lastReqURL, _ = url.Parse(cur)
					}
					locURL, err := url.Parse(loc)
					if err != nil {
						return nil, cur, common.NewAppError(common.CodeJwcParseFailed, "location 无法解析")
					}
					cur = lastReqURL.ResolveReference(locURL).String()
					lastReqURL = locURL
					continue
				}
				// 无跳转：用读到的 body 重建响应体，保证调用方能读到完整内容
				resp.Body = io.NopCloser(bytes.NewReader(body))
			}
			return resp, cur, nil
		}
		loc := resp.Header.Get("Location")
		_ = resp.Body.Close()
		if loc == "" {
			return nil, cur, common.NewAppError(common.CodeJwcRequestFailed, "重定向缺少 Location")
		}
		if lastReqURL == nil {
			lastReqURL, _ = url.Parse(cur)
		}
		locURL, err := url.Parse(loc)
		if err != nil {
			return nil, cur, common.NewAppError(common.CodeJwcParseFailed, "location 无法解析")
		}
		cur = lastReqURL.ResolveReference(locURL).String()
		lastReqURL = locURL
	}
	return nil, cur, common.NewAppError(common.CodeJwcRequestFailed, "重定向层级过多")
}

func (s *jwcSessionService) GetCachedCookies(ctx context.Context, uid int) ([]*http.Cookie, error) {
	return s.sessionCache.GetCookies(ctx, uid)
}

func (s *jwcSessionService) InvalidateSession(ctx context.Context, uid int) error {
	return s.sessionCache.DeleteCookies(ctx, uid)
}

func (s *jwcSessionService) encryptPassword(password string) (string, error) {
	publicKey := s.rsaKeyService.GetPublicKey()
	if publicKey == "" {
		return "", common.NewAppError(common.CodeInternalError, "RSA 公钥未初始化")
	}
	block, _ := pem.Decode([]byte(publicKey))
	if block == nil {
		return "", common.NewAppError(common.CodeJwcLoginFailed, "RSA 公钥格式无效")
	}
	pubInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", common.NewAppError(common.CodeJwcLoginFailed, fmt.Sprintf("解析 RSA 公钥失败: %v", err))
	}
	pub := pubInterface.(*rsa.PublicKey)
	encryptedBytes, err := rsa.EncryptPKCS1v15(rand.Reader, pub, []byte(password))
	if err != nil {
		return "", common.NewAppError(common.CodeJwcLoginFailed, fmt.Sprintf("RSA 加密失败: %v", err))
	}
	return "__RSA__" + base64.StdEncoding.EncodeToString(encryptedBytes), nil
}

func (s *jwcSessionService) GenerateRandomFingerPrintHash() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func (s *jwcSessionService) LoginAndCacheWithConfig(ctx context.Context, uid int, username, password string, loginURL, redirectURL string, cookieCache CookieCache) error {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return common.NewAppError(common.CodeJwcLoginFailed, "创建会话失败")
	}
	client := &http.Client{Jar: jar, Timeout: s.timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Get(loginURL)
	if err != nil {
		if isTimeoutError(err) {
			return common.NewAppError(common.CodeJwcLoginTimeout, "教务系统连接超时，请稍后重试")
		}
		return common.NewAppError(common.CodeJwcLoginFailed, "连接系统失败")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		if res.StatusCode >= 500 {
			return common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("教务系统服务器错误: %d", res.StatusCode))
		}
		return common.NewAppError(common.CodeJwcLoginFailed, fmt.Sprintf("响应异常: %d", res.StatusCode))
	}
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return common.NewAppError(common.CodeJwcParseFailed, "解析登录页面失败")
	}
	execution := doc.Find("input[name='execution']").AttrOr("value", "")
	if execution == "" {
		return common.NewAppError(common.CodeJwcLoginFailed, "找不到 execution")
	}
	encryptedPwd, err := s.encryptPassword(password)
	if err != nil {
		return common.NewAppError(common.CodeJwcLoginFailed, fmt.Sprintf("密码加密失败: %v", err))
	}
	fpVisitorId, err := s.GenerateRandomFingerPrintHash()
	if err != nil {
		return common.NewAppError(common.CodeInternalError, "生成设备指纹失败")
	}
	needMFA, mfaState, err := s.detectMFA(ctx, client, username, password, fpVisitorId)
	if err != nil {
		return err
	}
	if needMFA {
		return common.NewAppError(common.CodeJwcMFARequired, "需要多因素认证，请前往i中南林APP进行验证")
	}
	form := url.Values{"username": {username}, "password": {encryptedPwd}, "captcha": {""}, "currentMenu": {"1"}, "failN": {"0"}, "mfaState": {mfaState}, "execution": {execution}, "_eventId": {"submit"}, "geolocation": {""}, "fpVisitorId": {fpVisitorId}, "trustAgent": {""}, "submit1": {"Login1"}}
	req, err := http.NewRequest("POST", loginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return common.NewAppError(common.CodeInternalError, "构造登录请求失败")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", loginURL)
	resp, err := client.Do(req)
	if err != nil {
		if isTimeoutError(err) {
			return common.NewAppError(common.CodeJwcLoginTimeout, "教务系统登录请求超时，请稍后重试")
		}
		return common.NewAppError(common.CodeJwcRequestFailed, "教务系统网络连接失败，请检查网络")
	}
	resp.Body.Close()
	if resp.StatusCode == 200 {
		return common.NewAppError(common.CodeJwcLoginFailed, "用户名或密码错误")
	} else if resp.StatusCode == 302 {
	} else if resp.StatusCode >= 500 {
		return common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("教务系统服务器错误: %d", resp.StatusCode))
	} else {
		return statusError(resp.StatusCode)
	}
	casURL, _ := url.Parse(loginURL)
	for _, cookie := range client.Jar.Cookies(casURL) {
		if cookie.Name == "TGC" {
			_ = s.sessionCache.SetTGC(ctx, uid, cookie, s.cacheExpire)
			break
		}
	}
	finalResp, finalURL, err := s.followGET(client, redirectURL, 8)
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			return appErr
		}
		if isTimeoutError(err) {
			return common.NewAppError(common.CodeJwcRequestFailed, "跟随重定向超时，教务系统响应缓慢")
		}
		return common.NewAppError(common.CodeJwcRequestFailed, "跟随重定向失败，教务系统可能暂时不可用")
	}
	defer finalResp.Body.Close()
	uFinal, _ := url.Parse(finalURL)
	cookies := s.collectJwxtCookies(client.Jar, uFinal.Host)
	if len(cookies) == 0 {
		return common.NewAppError(common.CodeJwcRequestFailed, "登录成功但未能获取会话Cookie，请检查教务系统网络连接")
	}
	return cookieCache.SetCookies(ctx, uid, cookies, s.cacheExpire)
}

func (s *jwcSessionService) exchangeWebVPNToken(ctx context.Context, client *http.Client, finalURL string) error {
	if s.webvpnTokenURL == "" {
		return common.NewAppError(common.CodeJwcLoginFailed, "WebVPN Token URL 未配置")
	}
	parsedURL, err := url.Parse(finalURL)
	if err != nil {
		return common.NewAppError(common.CodeJwcParseFailed, "解析 WebVPN 回调 URL 失败")
	}
	parts := strings.Split(parsedURL.Path, "/")
	if len(parts) < 4 {
		return common.NewAppError(common.CodeJwcParseFailed, "WebVPN 回调 URL 格式错误")
	}
	externalID := parts[len(parts)-1]
	if externalID == "" {
		return common.NewAppError(common.CodeJwcParseFailed, "WebVPN externalId 为空")
	}
	ticket := parsedURL.Query().Get("ticket")
	if ticket == "" {
		return common.NewAppError(common.CodeJwcParseFailed, "WebVPN ticket 为空")
	}
	// 浏览器流程：CAS 302 后先 GET callback 页面（其前端 JS 再调 auth/finish），
	// 该请求可能携带服务端下发的会话 Cookie，对齐浏览器行为
	cbReq, err := http.NewRequestWithContext(ctx, "GET", finalURL, nil)
	if err == nil {
		cbReq.Header.Set("User-Agent", "Mozilla/5.0")
		if cbResp, err := client.Do(cbReq); err == nil {
			cbHead, _ := io.ReadAll(io.LimitReader(cbResp.Body, 512))
			cbResp.Body.Close()
			log.Printf("[WebVPN] callback GET status=%d location=%q bodyHead=%.120s",
				cbResp.StatusCode, cbResp.Header.Get("Location"), string(cbHead))
		} else {
			log.Printf("[WebVPN] callback GET 失败(继续): %v", err)
		}
	}
	deviceID, err := s.GenerateRandomFingerPrintHash()
	if err != nil {
		return common.NewAppError(common.CodeInternalError, "生成设备ID失败")
	}
	// callbackUrl 必须不包含 ticket 参数
	callbackURL := fmt.Sprintf("%s://%s%s", parsedURL.Scheme, parsedURL.Host, parsedURL.Path)
	dataBytes, _ := json.Marshal(map[string]string{"callbackUrl": callbackURL, "ticket": ticket, "deviceId": deviceID})
	reqBodyBytes, _ := json.Marshal(map[string]string{"externalId": externalID, "data": string(dataBytes)})
	tokenReq, err := http.NewRequestWithContext(ctx, "POST", s.webvpnTokenURL, strings.NewReader(string(reqBodyBytes)))
	if err != nil {
		return common.NewAppError(common.CodeInternalError, "构造 token 请求失败")
	}
	tokenReq.Header.Set("Content-Type", "application/json")
	tokenReq.Header.Set("User-Agent", "Mozilla/5.0")
	tokenReq.Header.Set("Origin", "https://webvpn.csuft.edu.cn")
	tokenReq.Header.Set("Referer", finalURL)
	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		if isTimeoutError(err) {
			return common.NewAppError(common.CodeJwcLoginTimeout, "WebVPN 令牌交换请求超时")
		}
		return common.NewAppError(common.CodeJwcRequestFailed, "WebVPN 令牌交换请求失败")
	}
	defer tokenResp.Body.Close()
	if tokenResp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(tokenResp.Body, 2048))
		log.Printf("[WebVPN] 令牌交换失败: status=%d url=%s reqBody=%s respBody=%s",
			tokenResp.StatusCode, s.webvpnTokenURL, string(reqBodyBytes), string(bodyBytes))
		return common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("WebVPN 令牌交换返回异常: %d", tokenResp.StatusCode))
	}
	var tokenResult struct {
		Code int `json:"code"`
		Data *struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenResult); err != nil {
		return common.NewAppError(common.CodeJwcParseFailed, "解析 WebVPN 令牌响应失败")
	}
	if tokenResult.Code != 0 || tokenResult.Data == nil || tokenResult.Data.Token == "" {
		return common.NewAppError(common.CodeJwcLoginFailed, "WebVPN 令牌交换失败")
	}
	return nil
}

// startWebVPNAuthSession 调用 WebVPN auth/start 创建认证会话，返回本次登录使用的动态 CAS login_url。
// 学校会轮换 CAS 认证方式的 externalId，config 中硬编码的 login_url 里的 externalId 会过期，
// 导致 auth/finish 报"找不到认证方式"。浏览器真实流程是：authentication/all 拿 externalId
// → auth/start 拿 login_url → CAS 认证 → callback?ticket → auth/finish。
func (s *jwcSessionService) startWebVPNAuthSession(ctx context.Context, client *http.Client) (string, error) {
	if s.mode != "webvpn" || s.webvpnTokenURL == "" {
		return "", nil // 非 webvpn 模式无需处理
	}
	// 从 webvpnTokenURL (https://webvpn.csuft.edu.cn/api/access/auth/finish) 推导 API 基址
	tokenParsed, err := url.Parse(s.webvpnTokenURL)
	if err != nil {
		return "", fmt.Errorf("解析 webvpn_token_url 失败: %w", err)
	}
	baseURL := fmt.Sprintf("%s://%s", tokenParsed.Scheme, tokenParsed.Host)

	// 1. 获取认证方式列表，找 CAS 认证（authType=4）的 externalId
	listReq, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/api/access/authentication/all", nil)
	if err != nil {
		return "", fmt.Errorf("构造认证列表请求失败: %w", err)
	}
	listReq.Header.Set("User-Agent", "Mozilla/5.0")
	listReq.Header.Set("Referer", baseURL+"/login")
	listResp, err := client.Do(listReq)
	if err != nil {
		return "", fmt.Errorf("获取认证方式列表失败: %w", err)
	}
	defer listResp.Body.Close()
	var listResult struct {
		Code int `json:"code"`
		Data *struct {
			List []struct {
				ExternalID string `json:"externalId"`
				Name       string `json:"name"`
				AuthType   int    `json:"authType"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listResult); err != nil {
		return "", fmt.Errorf("解析认证方式列表失败: %w", err)
	}
	if listResult.Code != 0 || listResult.Data == nil {
		return "", fmt.Errorf("认证方式列表响应异常: code=%d", listResult.Code)
	}
	var casExternalID string
	for _, m := range listResult.Data.List {
		if m.AuthType == 4 { // CAS 认证
			casExternalID = m.ExternalID
			break
		}
	}
	if casExternalID == "" {
		return "", fmt.Errorf("未找到 CAS 认证方式")
	}

	// 2. 调用 auth/start 创建认证会话，拿本次登录的 CAS login_url
	callbackURL := baseURL + "/callback/cas/" + casExternalID
	startData, _ := json.Marshal(map[string]string{"callbackUrl": callbackURL})
	startBody, _ := json.Marshal(map[string]string{"externalId": casExternalID, "data": string(startData)})
	startReq, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/api/access/auth/start", strings.NewReader(string(startBody)))
	if err != nil {
		return "", fmt.Errorf("构造 auth/start 请求失败: %w", err)
	}
	startReq.Header.Set("Content-Type", "application/json")
	startReq.Header.Set("User-Agent", "Mozilla/5.0")
	startReq.Header.Set("Origin", baseURL)
	startReq.Header.Set("Referer", baseURL+"/login")
	startResp, err := client.Do(startReq)
	if err != nil {
		return "", fmt.Errorf("auth/start 请求失败: %w", err)
	}
	defer startResp.Body.Close()
	var startResult struct {
		Code int `json:"code"`
		Data *struct {
			Action *struct {
				LoginURL string `json:"login_url"`
			} `json:"action"`
		} `json:"data"`
	}
	if err := json.NewDecoder(startResp.Body).Decode(&startResult); err != nil {
		return "", fmt.Errorf("解析 auth/start 响应失败: %w", err)
	}
	if startResult.Code != 0 || startResult.Data == nil || startResult.Data.Action == nil || startResult.Data.Action.LoginURL == "" {
		return "", fmt.Errorf("auth/start 响应异常: code=%d", startResult.Code)
	}
	log.Printf("[WebVPN] auth/start 成功, externalId=%s", casExternalID)
	return startResult.Data.Action.LoginURL, nil
}

// loginAndCacheOnceByWebVPN WebVPN 登录逻辑
func (s *jwcSessionService) loginAndCacheOnceByWebVPN(ctx context.Context, uid int, username, password string) error {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return common.NewAppError(common.CodeJwcLoginFailed, "创建会话失败")
	}
	client := &http.Client{Jar: jar, Timeout: s.timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}

	// 0. WebVPN 新流程：先调用 auth/start 创建认证会话，动态获取本次登录用的 CAS login_url
	// （学校会轮换 CAS 认证方式的 externalId，config 中硬编码的 login_url 会失效）
	loginURL := s.loginURL
	if dynamicURL, err := s.startWebVPNAuthSession(ctx, client); err != nil {
		log.Printf("[WebVPN] auth/start 失败，回退到配置的 login_url: %v", err)
	} else if dynamicURL != "" {
		loginURL = dynamicURL
		log.Printf("[WebVPN] 已通过 auth/start 获取本次登录地址")
	}

	// 1. GET CAS 登录页
	res, err := client.Get(loginURL)
	if err != nil {
		if isTimeoutError(err) {
			return common.NewAppError(common.CodeJwcLoginTimeout, "教务系统连接超时，请稍后重试")
		}
		return common.NewAppError(common.CodeJwcLoginFailed, "连接系统失败")
	}
	defer res.Body.Close()

	var loginLocation string // CAS 登录 302 的 Location

	if res.StatusCode == 302 {
		// 已有 TGC，直接跟随重定向获取 Location
		loginLocation = res.Header.Get("Location")
	} else if res.StatusCode == http.StatusOK {
		// 解析登录页
		doc, err := goquery.NewDocumentFromReader(res.Body)
		if err != nil {
			return common.NewAppError(common.CodeJwcParseFailed, "解析登录页面失败")
		}
		execution := doc.Find("input[name='execution']").AttrOr("value", "")
		if execution == "" {
			return common.NewAppError(common.CodeJwcLoginFailed, "找不到 execution")
		}

		encryptedPwd, err := s.encryptPassword(password)
		if err != nil {
			return common.NewAppError(common.CodeJwcLoginFailed, fmt.Sprintf("密码加密失败: %v", err))
		}
		fpVisitorId, err := s.GenerateRandomFingerPrintHash()
		if err != nil {
			return common.NewAppError(common.CodeInternalError, "生成设备指纹失败")
		}
		needMFA, mfaState, err := s.detectMFA(ctx, client, username, password, fpVisitorId)
		if err != nil {
			return err
		}
		if needMFA {
			return common.NewAppError(common.CodeJwcMFARequired, "需要多因素认证，请前往i中南林APP进行验证")
		}

		form := url.Values{"username": {username}, "password": {encryptedPwd}, "captcha": {""}, "currentMenu": {"1"}, "failN": {"0"}, "mfaState": {mfaState}, "execution": {execution}, "_eventId": {"submit"}, "geolocation": {""}, "fpVisitorId": {fpVisitorId}, "trustAgent": {""}, "submit1": {"Login1"}}
		req, err := http.NewRequest("POST", loginURL, strings.NewReader(form.Encode()))
		if err != nil {
			return common.NewAppError(common.CodeInternalError, "构造登录请求失败")
		}
		req.Header.Set("User-Agent", "Mozilla/5.0")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Referer", loginURL)

		resp, err := client.Do(req)
		if err != nil {
			if isTimeoutError(err) {
				return common.NewAppError(common.CodeJwcLoginTimeout, "教务系统登录请求超时，请稍后重试")
			}
			return common.NewAppError(common.CodeJwcRequestFailed, "教务系统网络连接失败，请检查网络")
		}
		resp.Body.Close()

		if resp.StatusCode == 200 {
			return common.NewAppError(common.CodeJwcLoginFailed, "用户名或密码错误")
		}
		if resp.StatusCode != 302 {
			if resp.StatusCode >= 500 {
				return common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("教务系统服务器错误: %d", resp.StatusCode))
			}
			return statusError(resp.StatusCode)
		}

		// 获取 302 Location
		loginLocation = resp.Header.Get("Location")

		// 缓存 TGC
		casURL, _ := url.Parse(loginURL)
		for _, cookie := range client.Jar.Cookies(casURL) {
			if cookie.Name == "TGC" {
				_ = s.sessionCache.SetTGC(ctx, uid, cookie, s.cacheExpire)
				break
			}
		}
	} else {
		return common.NewAppError(common.CodeJwcLoginFailed, fmt.Sprintf("响应异常: %d", res.StatusCode))
	}

	// 2. WebVPN 令牌交换（用 302 Location 中的 ticket）
	if loginLocation != "" {
		log.Printf("[WebVPN] CAS 登录后 302 到: %s", loginLocation)
		if err := s.exchangeWebVPNToken(ctx, client, loginLocation); err != nil {
			log.Printf("[WebVPN] 令牌交换失败: %v", err)
			return err
		}
		log.Printf("[WebVPN] 令牌交换成功")
	}

	// 3. 访问教务系统
	finalResp, finalURL, err := s.followGET(client, s.redirectURL, 8)
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			return appErr
		}
		if isTimeoutError(err) {
			return common.NewAppError(common.CodeJwcRequestFailed, "重定向超时，教务系统响应缓慢")
		}
		return common.NewAppError(common.CodeJwcRequestFailed, "重定向失败，教务系统可能暂时不可用")
	}
	bodyBytes, _ := io.ReadAll(finalResp.Body)
	finalResp.Body.Close()
	log.Printf("[WebVPN] followGET 最终页: url=%s len=%d title=%s", finalURL, len(bodyBytes), extractTitle(bodyBytes))

	// 3.5 会话校验：新版 jwxt 通过 sso.jsp SSO 免密进入（无需本地表单登录）。
	// followGET 访问 redirect_url(sso.jsp) 会走 sso.jsp → CAS(TGC 自动认证) → sso.jsp?ticket → xsMainV.htmlx 跳转链，
	// 服务端在跳转过程中自动建立 bzb_jsxsd 会话 Cookie。
	// 若最终页仍是 CAS 登录页/出错页，说明 SSO 未建立成功。
	if strings.Contains(string(bodyBytes), "LoginToXk") || strings.Contains(string(bodyBytes), "userPassword") || extractTitle(bodyBytes) == "登录" {
		log.Printf("[WebVPN] SSO 进入失败，最终页疑似登录页: url=%s", finalURL)
		return common.NewAppError(common.CodeJwcLoginFailed, "教务系统 SSO 登录失败，会话未建立")
	}

	uFinal, _ := url.Parse(finalURL)
	// 关键：bzb_jsxsd 的 Path 是 /jsxsd，SERVERID_jsxsd 的 Path 是 /。
	// 用 Path="/" 取 cookie 会漏掉 path=/jsxsd 的 bzb_jsxsd，导致后续接口报"请先登录系统"。
	// 必须用 /jsxsd/ 路径收集，才能同时拿到两个 cookie。
	cookies := s.collectJwxtCookies(client.Jar, uFinal.Host)
	// 诊断：打印最终收集到的 cookie 名（不含值）
	cookieNames := make([]string, 0, len(cookies))
	for _, c := range cookies {
		cookieNames = append(cookieNames, c.Name+"(path="+c.Path+")")
	}
	log.Printf("[WebVPN] 会话 Cookie 列表: %v", cookieNames)
	if len(cookies) == 0 {
		return common.NewAppError(common.CodeJwcRequestFailed, "登录成功但未能获取会话Cookie，请检查教务系统网络连接")
	}
	return s.sessionCache.SetCookies(ctx, uid, cookies, s.cacheExpire)
}

func (s *jwcSessionService) LoginAndGetClient(ctx context.Context, username, password string) (*http.Client, error) {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcLoginFailed, "创建会话失败")
	}
	client := &http.Client{Jar: jar, Timeout: s.timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Get(s.loginURL)
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcLoginFailed, "连接系统失败")
	}
	defer res.Body.Close()
	if res.StatusCode == 302 {
		return client, nil
	}
	if res.StatusCode != http.StatusOK {
		return nil, common.NewAppError(common.CodeJwcLoginFailed, fmt.Sprintf("响应异常: %d", res.StatusCode))
	}
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "解析登录页面失败")
	}
	execution := doc.Find("input[name='execution']").AttrOr("value", "")
	if execution == "" {
		return nil, common.NewAppError(common.CodeJwcLoginFailed, "找不到 execution")
	}
	encryptedPwd, err := s.encryptPassword(password)
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcLoginFailed, fmt.Sprintf("密码加密失败: %v", err))
	}
	fpVisitorId, err := s.GenerateRandomFingerPrintHash()
	if err != nil {
		return nil, common.NewAppError(common.CodeInternalError, "生成设备指纹失败")
	}
	needMFA, mfaState, err := s.detectMFA(ctx, client, username, password, fpVisitorId)
	if err != nil {
		return nil, err
	}
	if needMFA {
		return nil, common.NewAppError(common.CodeJwcMFARequired, "需要多因素认证，请前往i中南林APP进行验证")
	}
	// 表单字段必须与 loginAndCacheOnceByWebVPN/LoginCheck 保持一致：
	// CAS 已启用 MFA 校验(mfaEnabled=true)，缺少 mfaState/captcha/currentMenu 等字段
	// 会被 CAS 拒绝并返回登录页，被误报为"用户名或密码错误"。
	form := url.Values{"username": {username}, "password": {encryptedPwd}, "captcha": {""}, "currentMenu": {"1"}, "failN": {"0"}, "mfaState": {mfaState}, "execution": {execution}, "_eventId": {"submit"}, "geolocation": {""}, "fpVisitorId": {fpVisitorId}, "trustAgent": {""}, "submit1": {"Login1"}}
	req, err := http.NewRequest("POST", s.loginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcLoginFailed, "构造登录请求失败")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", s.loginURL)
	resp, err := client.Do(req)
	if err != nil {
		if isTimeoutError(err) {
			return nil, common.NewAppError(common.CodeJwcLoginTimeout, "教务系统登录请求超时")
		}
		return nil, common.NewAppError(common.CodeJwcRequestFailed, "教务系统网络连接失败")
	}
	resp.Body.Close()
	if resp.StatusCode == 200 {
		return nil, common.NewAppError(common.CodeJwcLoginFailed, "用户名或密码错误")
	} else if resp.StatusCode == 302 {
	} else if resp.StatusCode >= 500 {
		return nil, common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("教务系统服务器错误: %d", resp.StatusCode))
	} else {
		return nil, statusError(resp.StatusCode)
	}
	return client, nil
}

func (s *jwcSessionService) LoginCheck(ctx context.Context, username, password string) error {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return common.NewAppError(common.CodeJwcLoginFailed, "创建会话失败")
	}
	client := &http.Client{Jar: jar, Timeout: s.timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Get(s.loginURL)
	if err != nil {
		return common.NewAppError(common.CodeJwcLoginFailed, "连接系统失败")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return common.NewAppError(common.CodeJwcLoginFailed, fmt.Sprintf("响应异常: %d", res.StatusCode))
	}
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return common.NewAppError(common.CodeJwcParseFailed, "解析登录页面失败")
	}
	execution := doc.Find("input[name='execution']").AttrOr("value", "")
	if execution == "" {
		return common.NewAppError(common.CodeJwcLoginFailed, "找不到 execution")
	}
	encryptedPwd, err := s.encryptPassword(password)
	if err != nil {
		return common.NewAppError(common.CodeJwcLoginFailed, fmt.Sprintf("密码加密失败: %v", err))
	}
	fpVisitorId, err := s.GenerateRandomFingerPrintHash()
	if err != nil {
		return common.NewAppError(common.CodeInternalError, "生成设备指纹失败")
	}
	needMFA, mfaState, err := s.detectMFA(ctx, client, username, password, fpVisitorId)
	if err != nil {
		return err
	}
	if needMFA {
		return common.NewAppError(common.CodeJwcMFARequired, "需要多因素认证，请前往i中南林APP进行验证")
	}
	form := url.Values{"username": {username}, "password": {encryptedPwd}, "captcha": {""}, "currentMenu": {"1"}, "failN": {"0"}, "mfaState": {mfaState}, "execution": {execution}, "_eventId": {"submit"}, "geolocation": {""}, "fpVisitorId": {fpVisitorId}, "trustAgent": {"false"}, "submit1": {"Login1"}}
	req, err := http.NewRequest("POST", s.loginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return common.NewAppError(common.CodeJwcLoginFailed, "构造登录请求失败")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", s.loginURL)
	resp, err := client.Do(req)
	if err != nil {
		if isTimeoutError(err) {
			return common.NewAppError(common.CodeJwcLoginTimeout, "教务系统登录请求超时")
		}
		return common.NewAppError(common.CodeJwcRequestFailed, "教务系统网络连接失败")
	}
	resp.Body.Close()
	if resp.StatusCode == 200 {
		return common.NewAppError(common.CodeJwcLoginFailed, "用户名或密码错误")
	} else if resp.StatusCode == 302 {
	} else if resp.StatusCode >= 500 {
		return common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("教务系统服务器错误: %d", resp.StatusCode))
	} else {
		return statusError(resp.StatusCode)
	}
	return nil
}

// collectJwxtCookies 收集 jwxt 域下所有会话 Cookie。
// 关键：bzb_jsxsd 的 Path 是 /jsxsd，SERVERID_jsxsd 的 Path 是 /。
// 用 Path="/" 取 cookie 会漏掉 path=/jsxsd 的 bzb_jsxsd，导致后续接口报"请先登录系统"。
// 这里用 /jsxsd/ 路径收集（能同时匹配 path=/ 和 path=/jsxsd 的 cookie），并按 Name 去重。
func (s *jwcSessionService) collectJwxtCookies(jar http.CookieJar, host string) []*http.Cookie {
	if host == "" {
		return nil
	}
	// 优先用 /jsxsd/ 路径（覆盖 bzb_jsxsd），同时兜底用 / 路径补齐
	jsxsdURL := &url.URL{Scheme: "https", Host: host, Path: "/jsxsd/"}
	cookies := jar.Cookies(jsxsdURL)
	// 补充 path=/ 的 cookie（如 SERVERID_jsxsd，虽然 /jsxsd/ 也能匹配到，但保险起见合并）
	rootURL := &url.URL{Scheme: "https", Host: host, Path: "/"}
	rootCookies := jar.Cookies(rootURL)

	seen := make(map[string]bool)
	var merged []*http.Cookie
	for _, c := range cookies {
		if !seen[c.Name] {
			seen[c.Name] = true
			merged = append(merged, c)
		}
	}
	for _, c := range rootCookies {
		if !seen[c.Name] {
			seen[c.Name] = true
			merged = append(merged, c)
		}
	}
	return merged
}

// extractTitle 从 HTML 中提取 <title> 内容（用于诊断日志）
func extractTitle(body []byte) string {
	m := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`).FindSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(string(m[1]))
}

// extractJSRedirect 从页面中提取 JS 跳转目标 URL（如 sso.jsp 返回的 window.location.href）。
func extractJSRedirect(body string) string {
	re := regexp.MustCompile(`window\.location(?:\.href)?\s*=\s*['"]([^'"]+)['"]`)
	m := re.FindStringSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			return true
		}
		var netErr net.Error
		if errors.As(urlErr.Err, &netErr) && netErr.Timeout() {
			return true
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}

func (s *jwcSessionService) detectMFA(ctx context.Context, client *http.Client, username, password, fpVisitorID string) (bool, string, error) {
	if s.mfaDetectURL == "" {
		return false, "", nil
	}
	encryptedPwd, err := s.encryptPassword(password)
	if err != nil {
		return false, "", err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", s.mfaDetectURL, strings.NewReader(url.Values{"username": {username}, "password": {encryptedPwd}, "fpVisitorId": {fpVisitorID}}.Encode()))
	if err != nil {
		return false, "", common.NewAppError(common.CodeInternalError, "创建 MFA 检测请求失败")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil {
		if isTimeoutError(err) {
			return false, "", common.NewAppError(common.CodeJwcLoginTimeout, "MFA 检测请求超时")
		}
		return false, "", common.NewAppError(common.CodeJwcRequestFailed, "MFA 检测请求失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, "", nil
	}
	var mfaResponse struct {
		Code int `json:"code"`
		Data struct {
			Need  bool   `json:"need"`
			State string `json:"state"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&mfaResponse); err != nil {
		return false, "", nil
	}
	if mfaResponse.Code != 0 {
		return false, "", nil
	}
	return mfaResponse.Data.Need, mfaResponse.Data.State, nil
}

// MFA phone verification flows below (unchanged)
type mfaPhoneChallenge struct {
	GID             string `json:"gid"`
	SecurePhone     string `json:"securePhone"`
	AttestServerURL string `json:"attestServerUrl"`
}

func casBaseURL(loginURL string) (string, error) {
	u, err := url.Parse(loginURL)
	if err != nil {
		return "", err
	}
	return u.Scheme + "://" + u.Host, nil
}

func (s *jwcSessionService) initPhoneMFA(ctx context.Context, client *http.Client, base, state string) (*mfaPhoneChallenge, error) {
	initURL := base + "/cas/mfa/initByType/securephone?state=" + url.QueryEscape(state)
	req, _ := http.NewRequestWithContext(ctx, "GET", initURL, nil)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil {
		if isTimeoutError(err) {
			return nil, common.NewAppError(common.CodeJwcLoginTimeout, "MFA 初始化请求超时")
		}
		return nil, common.NewAppError(common.CodeJwcRequestFailed, "MFA 初始化请求失败")
	}
	defer resp.Body.Close()
	var result struct {
		Code int               `json:"code"`
		Data mfaPhoneChallenge `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "解析 MFA 初始化响应失败")
	}
	if result.Code != 0 || result.Data.GID == "" {
		return nil, common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("MFA 初始化失败，错误码: %d", result.Code))
	}
	return &result.Data, nil
}

func (s *jwcSessionService) sendPhoneMFACode(ctx context.Context, attestURL, gid string) error {
	sendURL := strings.TrimRight(attestURL, "/") + "/api/guard/securephone/send"
	bodyBytes, _ := json.Marshal(map[string]string{"gid": gid})
	req, _ := http.NewRequestWithContext(ctx, "POST", sendURL, strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	hc := &http.Client{Timeout: s.timeout}
	resp, err := hc.Do(req)
	if err != nil {
		if isTimeoutError(err) {
			return common.NewAppError(common.CodeJwcLoginTimeout, "发送验证码请求超时")
		}
		return common.NewAppError(common.CodeJwcRequestFailed, "发送验证码请求失败")
	}
	defer resp.Body.Close()
	var result struct {
		Code int `json:"code"`
		Data struct {
			Result string `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return common.NewAppError(common.CodeJwcParseFailed, "解析发送验证码响应失败")
	}
	if result.Code != 0 || result.Data.Result != "ok" {
		return common.NewAppError(common.CodeJwcRequestFailed, "发送验证码失败，请稍后重试")
	}
	return nil
}

func (s *jwcSessionService) validatePhoneMFACode(ctx context.Context, attestURL, gid, code string) error {
	validURL := strings.TrimRight(attestURL, "/") + "/api/guard/securephone/valid"
	bodyBytes, _ := json.Marshal(map[string]string{"gid": gid, "code": code})
	req, _ := http.NewRequestWithContext(ctx, "POST", validURL, strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	hc := &http.Client{Timeout: s.timeout}
	resp, err := hc.Do(req)
	if err != nil {
		if isTimeoutError(err) {
			return common.NewAppError(common.CodeJwcLoginTimeout, "校验验证码请求超时")
		}
		return common.NewAppError(common.CodeJwcRequestFailed, "校验验证码请求失败")
	}
	defer resp.Body.Close()
	var result struct {
		Code int `json:"code"`
		Data struct {
			Result string `json:"result"`
			Status int    `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return common.NewAppError(common.CodeJwcParseFailed, "解析校验验证码响应失败")
	}
	if result.Code != 0 || result.Data.Result != "ok" {
		return common.NewAppError(common.CodeJwcLoginFailed, "验证码错误，请重新输入")
	}
	return nil
}

func (s *jwcSessionService) cleanExpiredMFASessions() {
	now := time.Now()
	for id, sess := range s.pendingMFA {
		if now.After(sess.expiresAt) {
			delete(s.pendingMFA, id)
		}
	}
}

func (s *jwcSessionService) StartPhoneMFALogin(ctx context.Context, uid int, username, password string) (string, string, error) {
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	client := &http.Client{Jar: jar, Timeout: s.timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Get(s.loginURL)
	if err != nil {
		if isTimeoutError(err) {
			return "", "", common.NewAppError(common.CodeJwcLoginTimeout, "教务系统连接超时")
		}
		return "", "", common.NewAppError(common.CodeJwcLoginFailed, "连接系统失败")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", "", common.NewAppError(common.CodeJwcLoginFailed, fmt.Sprintf("响应异常: %d", res.StatusCode))
	}
	doc, _ := goquery.NewDocumentFromReader(res.Body)
	execution := doc.Find("input[name='execution']").AttrOr("value", "")
	if execution == "" {
		return "", "", common.NewAppError(common.CodeJwcLoginFailed, "找不到 execution")
	}
	fpVisitorId, _ := s.GenerateRandomFingerPrintHash()
	needMFA, state, err := s.detectMFA(ctx, client, username, password, fpVisitorId)
	if err != nil {
		return "", "", err
	}
	if !needMFA {
		return "", "", common.NewAppError(common.CodeJwcLoginFailed, "该账号当前不需要短信验证")
	}
	base, _ := casBaseURL(s.loginURL)
	challenge, err := s.initPhoneMFA(ctx, client, base, state)
	if err != nil {
		return "", "", err
	}
	if err := s.sendPhoneMFACode(ctx, challenge.AttestServerURL, challenge.GID); err != nil {
		return "", "", err
	}
	challengeID, _ := s.GenerateRandomFingerPrintHash()
	s.pendingMFAMu.Lock()
	s.cleanExpiredMFASessions()
	s.pendingMFA[challengeID] = &pendingMFASession{uid: uid, username: username, password: password, client: client, execution: execution, fpVisitorId: fpVisitorId, mfaState: state, gid: challenge.GID, attestURL: challenge.AttestServerURL, loginURL: s.loginURL, redirectURL: s.redirectURL, cookieCache: s.sessionCache, expiresAt: time.Now().Add(5 * time.Minute)}
	s.pendingMFAMu.Unlock()
	return challengeID, challenge.SecurePhone, nil
}

func (s *jwcSessionService) CompletePhoneMFALogin(ctx context.Context, challengeID string, code string) error {
	s.pendingMFAMu.Lock()
	session, ok := s.pendingMFA[challengeID]
	if ok && time.Now().After(session.expiresAt) {
		delete(s.pendingMFA, challengeID)
		ok = false
	}
	s.pendingMFAMu.Unlock()
	if !ok {
		return common.NewAppError(common.CodeJwcLoginFailed, "验证会话不存在或已过期")
	}
	if err := s.validatePhoneMFACode(ctx, session.attestURL, session.gid, code); err != nil {
		return err
	}
	encryptedPwd, _ := s.encryptPassword(session.password)
	form := url.Values{"username": {session.username}, "password": {encryptedPwd}, "captcha": {""}, "currentMenu": {"1"}, "failN": {"0"}, "mfaState": {session.mfaState}, "execution": {session.execution}, "_eventId": {"submit"}, "geolocation": {""}, "fpVisitorId": {session.fpVisitorId}, "trustAgent": {"false"}, "submit1": {"Login1"}}
	req, _ := http.NewRequestWithContext(ctx, "POST", session.loginURL, strings.NewReader(form.Encode()))
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", session.loginURL)
	resp, err := session.client.Do(req)
	if err != nil {
		if isTimeoutError(err) {
			return common.NewAppError(common.CodeJwcLoginTimeout, "教务系统登录请求超时")
		}
		return common.NewAppError(common.CodeJwcRequestFailed, "教务系统网络连接失败")
	}
	resp.Body.Close()
	if resp.StatusCode == 200 {
		return common.NewAppError(common.CodeJwcLoginFailed, "用户名或密码错误")
	} else if resp.StatusCode == 302 {
	} else if resp.StatusCode >= 500 {
		return common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("教务系统服务器错误: %d", resp.StatusCode))
	} else {
		return statusError(resp.StatusCode)
	}
	for _, cookie := range session.client.Jar.Cookies(func() *url.URL { u, _ := url.Parse(session.loginURL); return u }()) {
		if cookie.Name == "TGC" {
			_ = s.sessionCache.SetTGC(ctx, session.uid, cookie, s.cacheExpire)
			break
		}
	}
	finalResp, finalURL, err := s.followGET(session.client, session.redirectURL, 8)
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			return appErr
		}
		if isTimeoutError(err) {
			return common.NewAppError(common.CodeJwcRequestFailed, "跟随重定向超时")
		}
		return common.NewAppError(common.CodeJwcRequestFailed, "跟随重定向失败")
	}
	defer finalResp.Body.Close()
	uFinal, _ := url.Parse(finalURL)
	cookies := s.collectJwxtCookies(session.client.Jar, uFinal.Host)
	if err := session.cookieCache.SetCookies(ctx, session.uid, cookies, s.cacheExpire); err != nil {
		return common.NewAppError(common.CodeCacheError, "缓存会话失败")
	}
	s.pendingMFAMu.Lock()
	delete(s.pendingMFA, challengeID)
	s.pendingMFAMu.Unlock()
	return nil
}

// statusError 把教务系统/教评系统返回的 HTTP 状态码转换为对应的业务错误。
// 401 表示登录态已失效（会话过期、被踢出或凭证无效），前端据此弹出「重新输入教务密码」弹窗；
// 其余非预期状态码按普通请求失败处理（可安全降级到数据库缓存）。
func statusError(statusCode int) error {
	if statusCode == 401 {
		return common.NewAppError(common.CodeJwcSessionExpired, "教务系统登录状态已失效（401），请重新输入教务密码")
	}
	if statusCode >= 500 {
		return common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("教务系统服务器错误: %d", statusCode))
	}
	return common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("教务系统返回异常状态码: %d", statusCode))
}
