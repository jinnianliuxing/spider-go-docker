package service

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"spider-go/internal/common"
	"sync"
	"time"
)

// RSAKeyService RSA 公钥服务接口
type RSAKeyService interface {
	// GetPublicKey 获取当前的 RSA 公钥
	GetPublicKey() string
	// FetchAndUpdate 从服务器获取并更新 RSA 公钥
	FetchAndUpdate() error
	// GetLastUpdated 获取最后更新时间
	GetLastUpdated() time.Time
}

// rsaKeyServiceImpl RSA 公钥服务实现
type rsaKeyServiceImpl struct {
	mu            sync.RWMutex
	publicKey     string
	rsaKeyURL     string
	lastUpdatedAt time.Time
	httpTimeout   time.Duration
}

// NewRSAKeyService 创建 RSA 公钥服务
func NewRSAKeyService(rsaKeyURL string) RSAKeyService {
	return &rsaKeyServiceImpl{
		rsaKeyURL:   rsaKeyURL,
		httpTimeout: 10 * time.Second,
	}
}

// GetPublicKey 获取当前的 RSA 公钥
func (s *rsaKeyServiceImpl) GetPublicKey() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.publicKey
}

// FetchAndUpdate 从服务器获取并更新 RSA 公钥
func (s *rsaKeyServiceImpl) FetchAndUpdate() error {
	client := &http.Client{Timeout: s.httpTimeout}
	resp, err := client.Get(s.rsaKeyURL)
	if err != nil {
		return common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("获取 RSA 公钥失败: %v", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("RSA 公钥服务器返回错误: %d", resp.StatusCode))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return common.NewAppError(common.CodeJwcRequestFailed, fmt.Sprintf("读取 RSA 公钥响应失败: %v", err))
	}

	publicKey := string(body)

	// 更新公钥（线程安全）
	s.mu.Lock()
	s.publicKey = publicKey
	s.lastUpdatedAt = time.Now()
	s.mu.Unlock()

	log.Printf("RSA 公钥已更新，时间: %s", s.lastUpdatedAt.Format("2006-01-02 15:04:05"))

	return nil
}

// GetLastUpdated 获取最后更新时间
func (s *rsaKeyServiceImpl) GetLastUpdated() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastUpdatedAt
}
