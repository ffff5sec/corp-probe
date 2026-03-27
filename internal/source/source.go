// Package source 定义数据源统一接口和共享 HTTP 基础设施。
// 各数据源（爱企查、企查查等）实现 Source 接口，未支持的方法返回 ErrNotSupported。
package source

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/imroc/req/v3"

	"github.com/ffff5sec/corp-probe/internal/model"
)

// ErrNotSupported 数据源不支持该操作时返回。
var ErrNotSupported = errors.New("该数据源不支持此操作")

// ErrAuthExpired 认证信息（Cookie 等）过期时返回。
var ErrAuthExpired = errors.New("认证信息已过期，请更新 Cookie")

// Source 统一数据源接口。
// 不是所有数据源都实现全部方法，未实现的方法应返回 ErrNotSupported。
type Source interface {
	// Name 返回数据源标识名（如 "aiqicha"）。
	Name() string

	// SearchCompany 根据关键词搜索企业列表。
	SearchCompany(ctx context.Context, keyword string) ([]model.Company, error)

	// QueryICP 查询企业的 ICP 备案记录。
	QueryICP(ctx context.Context, companyName string) ([]model.ICPRecord, error)

	// QueryInvestments 查询企业对外投资（股权穿透用）。
	QueryInvestments(ctx context.Context, companyID string) ([]model.Investment, error)

	// QueryApps 查询企业名下的移动应用。
	QueryApps(ctx context.Context, companyName string) ([]model.AppInfo, error)

	// QueryMiniPrograms 查询企业名下的小程序。
	QueryMiniPrograms(ctx context.Context, companyName string) ([]model.MiniProgram, error)

	// QueryOfficialAccounts 查询企业名下的公众号。
	QueryOfficialAccounts(ctx context.Context, companyName string) ([]model.OfficialAccount, error)
}

// ────────────────────────────────────────
// BaseClient — 共享 HTTP 基础设施（基于 req/v3，支持 TLS 指纹伪装）
// ────────────────────────────────────────

// BaseClient 提供所有数据源共享的 HTTP 请求能力。
// 使用 req/v3 库模拟 Chrome TLS 指纹，绕过基于 JA3/JA4 的反爬检测。
type BaseClient struct {
	Cookie  string        // 认证 Cookie
	proxy   string        // 代理地址
	timeout time.Duration // 请求超时
	Retry   int           // 最大重试次数
	Delay   time.Duration // 请求间隔
}

// 默认 User-Agent，模拟 Chrome 浏览器。
const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// NewBaseClient 创建 BaseClient 实例。
// 与 ENScan_GO 对齐：SetTLSClientConfig + SetTLSFingerprintChrome，不用 ImpersonateChrome。
func NewBaseClient(cookie, proxy string, timeout, delay time.Duration, retry int) *BaseClient {
	return &BaseClient{
		Cookie:  cookie,
		proxy:   proxy,
		timeout: timeout,
		Retry:   retry,
		Delay:   delay,
	}
}

// newReqClient 每次请求创建新的 req.Client（与 ENScan_GO 一致）。
// 避免客户端复用导致的指纹关联检测。
func (b *BaseClient) newReqClient() *req.Client {
	c := req.C()
	// 与 ENScan_GO 完全一致：先 SetTLSClientConfig，再 SetTLSFingerprintChrome
	// 不能用 EnableForceHTTP1（会破坏 Chrome ALPN 指纹）
	// 不能用 EnableInsecureSkipVerify（会覆盖 TLS 配置）
	c.SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true})
	c.SetTLSFingerprintChrome()
	c.SetTimeout(b.timeout)
	c.SetCommonHeaders(map[string]string{
		"User-Agent": defaultUserAgent,
		"Accept":     "text/html, application/xhtml+xml, image/jxr, */*",
		"Referer":    "https://aiqicha.baidu.com/",
	})
	if b.proxy != "" {
		c.SetProxyURL(b.proxy)
	}
	return c
}

// DoGet 发起 GET 请求，自动注入 Cookie 并执行重试策略。
func (b *BaseClient) DoGet(ctx context.Context, reqURL string, headers map[string]string) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt <= b.Retry; attempt++ {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}

		// 非首次请求时指数退避
		if attempt > 0 {
			backoff := b.Delay * time.Duration(math.Pow(2, float64(attempt-1)))
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			time.Sleep(backoff)
		}

		r := b.newReqClient().R()
		if b.Cookie != "" {
			r.SetHeader("Cookie", b.Cookie)
		}
		for k, v := range headers {
			r.SetHeader(k, v)
		}

		resp, err := r.Get(reqURL)
		if err != nil {
			lastErr = fmt.Errorf("请求失败 (第%d次): %w", attempt+1, err)
			continue
		}

		// 检查安全验证
		body := resp.Bytes()
		if strings.Contains(string(body), "百度安全验证") {
			lastErr = fmt.Errorf("触发安全验证，请在浏览器中完成验证后重试")
			time.Sleep(5 * time.Second)
			continue
		}

		if resp.StatusCode == 302 || resp.StatusCode == 401 {
			return nil, ErrAuthExpired
		}
		if resp.StatusCode == 403 {
			return nil, fmt.Errorf("IP 被禁止访问，请更换 IP 或配置代理")
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("HTTP %d (第%d次)", resp.StatusCode, attempt+1)
			continue
		}

		return body, nil
	}

	return nil, fmt.Errorf("请求 %s 失败，已重试 %d 次: %w", reqURL, b.Retry, lastErr)
}

// DoPost 发起 POST 请求。
func (b *BaseClient) DoPost(ctx context.Context, reqURL string, bodyData interface{}, headers map[string]string) ([]byte, error) {
	r := b.newReqClient().R()
	if b.Cookie != "" {
		r.SetHeader("Cookie", b.Cookie)
	}
	for k, v := range headers {
		r.SetHeader(k, v)
	}

	resp, err := r.SetBody(bodyData).Post(reqURL)
	if err != nil {
		return nil, fmt.Errorf("POST 请求失败: %w", err)
	}
	return resp.Bytes(), nil
}
