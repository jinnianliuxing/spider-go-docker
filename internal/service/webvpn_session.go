package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"spider-go/internal/common"

	"golang.org/x/net/publicsuffix"
)

// ============================================================================
// WebVPN 认证链路通用动作
// ============================================================================
//
// 密码绑定、扫码绑定、手机号验证码绑定三条路径，在「CAS 换到 ticket 之后」的动作
// 完全一致：
//
//	auth/start 建会话 → CAS 认证拿到 302 ticket
//	  → GET callback?ticket（种 cookie）
//	  → POST api/access/auth/finish（换 webvpn-token）
//	  → GET  api/access/user/info（拿学号/姓名）
//
// 本文件把这几个动作抽成**与调用方 client 无关的自由函数**，三处共用，
// 避免每加一种绑定方式就复制一遍（原先扫码已经复制过一份）。
//
// ⚠️ 所有函数都作用于**传入的 client**，凭据（cookie jar）由调用方持有。

// webVPNUserInfo /api/access/user/info 的响应
type webVPNUserInfo struct {
	UserID           int      `json:"userId"`
	Nickname         string   `json:"nickname"`
	FullName         string   `json:"fullName"`
	IdentityTypeName string   `json:"identityTypeName"`
	OrganizationName string   `json:"organizationName"`
	Groups           []string `json:"groups"`
}

// webvpnBaseURL 从 webvpn_token_url（…/api/access/auth/finish）推导站点根地址。
func webvpnBaseURL(webvpnTokenURL string) (string, error) {
	tokenParsed, err := url.Parse(webvpnTokenURL)
	if err != nil {
		return "", fmt.Errorf("解析 webvpn_token_url 失败: %w", err)
	}
	if tokenParsed.Scheme == "" || tokenParsed.Host == "" {
		return "", fmt.Errorf("webvpn_token_url 不完整: %s", webvpnTokenURL)
	}
	return fmt.Sprintf("%s://%s", tokenParsed.Scheme, tokenParsed.Host), nil
}

// webvpnStartAuthSession 调 WebVPN auth/start 创建认证会话，返回本次登录的 CAS login_url。
//
// 学校会轮换 CAS 认证方式的 externalId，config 里硬编码 login_url 中的 externalId 会过期，
// 直接拿它换来的 ticket 会在 auth/finish 处报 `20012 找不到认证方式`。
// 浏览器真实流程是：authentication/all 拿 externalId → auth/start 拿 login_url
// → CAS 认证 → callback?ticket → auth/finish。本函数复刻前三步中的前两步。
func webvpnStartAuthSession(ctx context.Context, client *http.Client, webvpnTokenURL string) (string, error) {
	baseURL, err := webvpnBaseURL(webvpnTokenURL)
	if err != nil {
		return "", err
	}

	// 1. 认证方式列表：找 CAS（authType=4）的 externalId（每次调用都可能不同）
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

	// 2. auth/start 创建认证会话，拿本次登录的 CAS login_url
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

// webvpnExchangeToken 用 CAS 302 回跳地址里的 ticket 换 webvpn-token。
//
// finalURL 形如 https://webvpn.csuft.edu.cn/callback/cas/<externalId>?ticket=ST-...
func webvpnExchangeToken(ctx context.Context, client *http.Client, webvpnTokenURL, finalURL string) error {
	if webvpnTokenURL == "" {
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
	ticket := parsedURL.Query().Get("ticket")
	if externalID == "" || ticket == "" {
		return common.NewAppError(common.CodeJwcParseFailed, "WebVPN externalId 或 ticket 为空")
	}

	// 先 GET 一次 callback 页（浏览器行为：页面 JS 再调 auth/finish），可能下发额外 cookie
	cbReq, err := http.NewRequestWithContext(ctx, "GET", finalURL, nil)
	if err == nil {
		cbReq.Header.Set("User-Agent", "Mozilla/5.0")
		if cbResp, err := client.Do(cbReq); err == nil {
			io.Copy(io.Discard, io.LimitReader(cbResp.Body, 4096))
			cbResp.Body.Close()
		}
	}

	// callbackUrl 必须不包含 ticket 参数
	callbackURL := fmt.Sprintf("%s://%s%s", parsedURL.Scheme, parsedURL.Host, parsedURL.Path)
	dataBytes, _ := json.Marshal(map[string]string{
		"callbackUrl": callbackURL,
		"ticket":      ticket,
		"deviceId":    mustRandomToken(),
	})
	reqBodyBytes, _ := json.Marshal(map[string]string{"externalId": externalID, "data": string(dataBytes)})

	req, err := http.NewRequestWithContext(ctx, "POST", webvpnTokenURL, strings.NewReader(string(reqBodyBytes)))
	if err != nil {
		return common.NewAppError(common.CodeInternalError, "构造令牌交换请求失败")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	// Origin 必须指向 WebVPN 主域：之前误用 CAS 反代域作 Origin，会被服务端拒绝。
	if baseURL, err := webvpnBaseURL(webvpnTokenURL); err == nil {
		req.Header.Set("Origin", baseURL)
	} else {
		req.Header.Set("Origin", "https://webvpn.csuft.edu.cn")
	}
	req.Header.Set("Referer", finalURL)

	resp, err := client.Do(req)
	if err != nil {
		if isTimeoutError(err) {
			return common.NewAppError(common.CodeJwcLoginTimeout, "WebVPN 令牌交换超时")
		}
		return common.NewAppError(common.CodeJwcRequestFailed, "WebVPN 令牌交换失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		log.Printf("[WebVPN] 令牌交换失败: status=%d body=%.300s", resp.StatusCode, string(body))
		return common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("WebVPN 令牌交换返回异常: %d", resp.StatusCode))
	}
	var tokenResult struct {
		Code int `json:"code"`
		Data *struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResult); err != nil {
		return common.NewAppError(common.CodeJwcParseFailed, "解析令牌响应失败")
	}
	if tokenResult.Code != 0 || tokenResult.Data == nil || tokenResult.Data.Token == "" {
		return common.NewAppError(common.CodeJwcLoginFailed, "WebVPN 令牌交换失败")
	}
	return nil
}

// webvpnFetchUserInfo 取当前 webvpn 会话对应的用户信息（nickname=学号、fullName=姓名）。
func webvpnFetchUserInfo(ctx context.Context, client *http.Client, webvpnTokenURL string) (*webVPNUserInfo, error) {
	baseURL, err := webvpnBaseURL(webvpnTokenURL)
	if err != nil {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "解析 WebVPN 地址失败")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/api/access/user/info", nil)
	if err != nil {
		return nil, common.NewAppError(common.CodeInternalError, "构造用户信息请求失败")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", baseURL+"/")

	resp, err := client.Do(req)
	if err != nil {
		if isTimeoutError(err) {
			return nil, common.NewAppError(common.CodeJwcLoginTimeout, "获取用户信息超时")
		}
		return nil, common.NewAppError(common.CodeJwcRequestFailed, "获取用户信息失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("获取用户信息返回异常: %d", resp.StatusCode))
	}
	var result struct {
		Code int             `json:"code"`
		Data *webVPNUserInfo `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, common.NewAppError(common.CodeJwcParseFailed, "解析用户信息失败")
	}
	if result.Code != 0 || result.Data == nil {
		return nil, common.NewAppError(common.CodeJwcLoginFailed, fmt.Sprintf("获取用户信息失败，错误码: %d", result.Code))
	}
	return result.Data, nil
}

// extractTGCCookie 从 jar 中取出 CAS 全局票据（TGC）。
//
// ⚠️ Go 的 cookiejar 在 Cookies() 返回时**不带 Domain/Path**（字段为空）。
// 若把这种"裸" cookie 交给评教模块手工回灌（SetCookies），
// jar 会把 Domain 设成调用方传入的 host、Path 设成 "/"，
// 导致 TGC 被发到错误的域/路径 → 教评系统拿不到 ticket（表现为「未能获取 userToken」）。
// 这里就地补全 Domain/Path，落 Redis 后即可原样回灌。
func extractTGCCookie(client *http.Client, loginURL string) *http.Cookie {
	if client == nil || client.Jar == nil {
		return nil
	}
	u, err := url.Parse(loginURL)
	if err != nil {
		return nil
	}
	for _, c := range client.Jar.Cookies(u) {
		if c.Name != "TGC" {
			continue
		}
		// 补全 Domain：优先用 cookie 自身声明的，否则用登录 URL 的 host
		host := c.Domain
		if host == "" {
			host = u.Hostname()
		}
		// 补全 Path：CAS 的 TGC 固定下发在 /cas 下
		path := c.Path
		if path == "" {
			path = "/cas"
		}
		return &http.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Path:     path,
			Domain:   host,
			Secure:   c.Secure,
			HttpOnly: c.HttpOnly,
			SameSite: c.SameSite,
		}
	}
	return nil
}

// newCASClient 创建「带 cookie jar 且不自动跟随 3xx」的客户端。
//
// ⚠️ 必须手动处理 302：要读 Location 里的 ticket，不能让库自动跟随。
// ⚠️ 必须用 jar 保持 SESSION cookie，否则 CAS 认不出同一会话。
func newCASClient(timeout time.Duration) (*http.Client, error) {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, common.NewAppError(common.CodeInternalError, "创建会话失败")
	}
	return &http.Client{
		Jar:     jar,
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

// restoreJarCookies 把缓存里的 cookie 灌回新 client 的 jar。
//
// ⚠️ 坑：cookiejar.SetCookies(u, cs) 会按 u 的路径去归一化 cookie.Path ——
// 传 https://host（路径为空）会把 SESSION 的 Path(/cas) 覆盖成 "/"。
// 为与真实浏览器一致，这里按**每条 cookie 自己的 path** 逐条设置。
func restoreJarCookies(client *http.Client, base string, cookies []*http.Cookie) {
	if client == nil || client.Jar == nil {
		return
	}
	for _, c := range cookies {
		cookie := &http.Cookie{
			Name:   c.Name,
			Value:  c.Value,
			Path:   c.Path,
			Domain: c.Domain,
		}
		p := c.Path
		if p == "" {
			p = "/"
		}
		u, err := url.Parse(base + p)
		if err != nil {
			u, _ = url.Parse(base + "/")
		}
		client.Jar.SetCookies(u, []*http.Cookie{cookie})
	}
}
