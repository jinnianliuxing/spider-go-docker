package httpclient

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	pkgerrors "spider-go/pkg/errors"
	"strings"
	"time"
)

// Crawler HTTP 爬虫客户端接口
type Crawler interface {
	// FetchWithCookies 使用 cookies 发起请求
	FetchWithCookies(ctx context.Context, method, targetURL string, cookies []*http.Cookie, formData url.Values) (io.ReadCloser, error)
}

// crawler 爬虫客户端实现
type crawler struct {
	timeout time.Duration
}

// NewCrawler 创建 HTTP 爬虫客户端
func NewCrawler() Crawler {
	return &crawler{
		timeout: 30 * time.Second,
	}
}

// NewCrawlerWithTimeout 创建带自定义超时的 HTTP 爬虫客户端
func NewCrawlerWithTimeout(timeout time.Duration) Crawler {
	return &crawler{
		timeout: timeout,
	}
}

// FetchWithCookies 使用 cookies 发起请求
func (c *crawler) FetchWithCookies(ctx context.Context, method, targetURL string, cookies []*http.Cookie, formData url.Values) (io.ReadCloser, error) {
	var body io.Reader
	if formData != nil {
		body = strings.NewReader(formData.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, body)
	if err != nil {
		return nil, pkgerrors.NewAppError(pkgerrors.CodeHttpRequestFailed, "failed to create request")
	}

	// 添加 cookies
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	// 设置请求头
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36 Edg/153.0.0.0")
	if formData != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	// 新版教务系统（强智 layui）的数据接口依赖这两个头区分 AJAX 请求并返回 JSON：
	// 缺少 X-Requested-With 时，cjcx_list / djkscj_list 等接口可能返回 HTML 框架页而非数据。
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	if ref := refererFor(targetURL); ref != "" {
		req.Header.Set("Referer", ref)
	}

	client := &http.Client{
		Timeout: c.timeout,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, pkgerrors.NewAppError(pkgerrors.CodeHttpRequestFailed, "request failed")
	}

	// 诊断模式：设置 SPIDER_DUMP_DIR 环境变量后，把每个接口的响应原样落盘，
	// 便于首次运行后直接分析真实数据结构而无需抓包
	if dumpDir := os.Getenv("SPIDER_DUMP_DIR"); dumpDir != "" {
		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, pkgerrors.NewAppError(pkgerrors.CodeHttpRequestFailed, "read response failed")
		}
		dumpResponse(targetURL, resp.StatusCode, raw)
		if resp.StatusCode != http.StatusOK {
			return nil, pkgerrors.NewAppError(pkgerrors.CodeInvalidResponse, "unexpected status code")
		}
		return io.NopCloser(bytes.NewReader(raw)), nil
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, pkgerrors.NewAppError(pkgerrors.CodeInvalidResponse, "unexpected status code")
	}

	return resp.Body, nil
}

// refererFor 根据目标 URL 生成 Referer 头。
// 新版教务系统的数据接口会校验 Referer（须来自对应模块入口页）。
// 映射：cjcx_list→cjcx_frm、xskb_list.do→xskb/xskb_list.do、xsksap_list→xsksap_query。
func refererFor(targetURL string) string {
	u, err := url.Parse(targetURL)
	if err != nil {
		return ""
	}
	base := u.Scheme + "://" + u.Host
	path := u.Path
	switch {
	case strings.Contains(path, "/kscj/cjcx_list"),
		strings.Contains(path, "/kscj/djkscj_list"):
		return base + "/jsxsd/kscj/cjcx_frm"
	case strings.Contains(path, "/xskb/xskb_list"):
		return base + "/jsxsd/xskb/xskb_list.do"
	case strings.Contains(path, "/xsks/xsksap_list"):
		return base + "/jsxsd/xsks/xsksap_query"
	default:
		return ""
	}
}

// dumpResponse 把响应体写入 SPIDER_DUMP_DIR 目录（含状态码与 URL，不含任何 Cookie/凭据）
func dumpResponse(targetURL string, statusCode int, raw []byte) {
	dir := os.Getenv("SPIDER_DUMP_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}

	name := "response"
	if u, err := url.Parse(targetURL); err == nil && u.Host != "" {
		name = u.Host + strings.ReplaceAll(u.Path, "/", "_")
	}
	ts := time.Now().Format("20060102_150405.000000")
	file := filepath.Join(dir, ts+"_"+name+".txt")

	var sb strings.Builder
	sb.WriteString("URL: ")
	sb.WriteString(targetURL)
	sb.WriteString("\nSTATUS: ")
	sb.WriteString(strings.TrimSpace(http.StatusText(statusCode)) + "\n----\n")
	sb.Write(raw)
	_ = os.WriteFile(file, []byte(sb.String()), 0o644)
}
