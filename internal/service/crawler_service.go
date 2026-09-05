package service

import (
	"context"
	"io"
	"net/http"
	"net/url"
	pkgerrors "spider-go/pkg/errors"
	pkghttpclient "spider-go/pkg/httpclient"
)

// CrawlerService 爬虫服务接口（适配层）
type CrawlerService interface {
	// FetchWithCookies 使用 cookies 发起请求
	FetchWithCookies(ctx context.Context, method, targetURL string, cookies []*http.Cookie, formData url.Values) (io.ReadCloser, error)
}

// crawlerServiceAdapter 爬虫服务适配器
type crawlerServiceAdapter struct {
	crawler pkghttpclient.Crawler
}

// NewHttpCrawlerService 创建 HTTP 爬虫服务（适配 pkg/httpclient）
func NewHttpCrawlerService() CrawlerService {
	return &crawlerServiceAdapter{
		crawler: pkghttpclient.NewCrawler(),
	}
}

// FetchWithCookies 使用 cookies 发起请求
func (a *crawlerServiceAdapter) FetchWithCookies(ctx context.Context, method, targetURL string, cookies []*http.Cookie, formData url.Values) (io.ReadCloser, error) {
	body, err := a.crawler.FetchWithCookies(ctx, method, targetURL, cookies, formData)
	if err != nil {
		return nil, pkgerrors.NewAppError(pkgerrors.CodeJwcRequestFailed, err.Error())
	}
	return body, nil
}
