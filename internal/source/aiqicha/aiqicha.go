// Package aiqicha 实现爱企查（aiqicha.baidu.com）数据源适配器。
// 支持企业搜索、ICP 备案查询、对外投资查询、小程序和公众号发现。
// APP 查询不支持（由七麦/酷安数据源提供）。
package aiqicha

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/ffff5sec/corp-probe/internal/model"
	"github.com/ffff5sec/corp-probe/internal/source"
)

const (
	baseURL = "https://aiqicha.baidu.com"

	// API 端点（与 ENScan_GO 对齐）
	searchPath      = "/s"                         // 企业搜索（HTML 页面，内嵌 JSON）
	icpPath         = "/detail/icpinfoAjax"        // ICP 备案查询
	investPath      = "/detail/investajax"         // 对外投资查询
	appPath         = "/c/appinfoAjax"             // APP 查询
	wechatPath      = "/c/wechatoaAjax"            // 微信公众号查询
	basicInfoPath   = "/detail/basicAllDataAjax"   // 企业基本信息
)

// Client 爱企查数据源客户端。
type Client struct {
	base *source.BaseClient
}

// New 创建爱企查客户端。
func New(cookie, proxy string, timeout, delay time.Duration, retry int) *Client {
	return &Client{
		base: source.NewBaseClient(cookie, proxy, timeout, delay, retry),
	}
}

// ajaxHeaders 返回 AJAX 请求需要的额外请求头。
// 爱企查的 AJAX 接口会校验 Referer 和 Accept 头，缺少会返回认证错误。
func (c *Client) ajaxHeaders(pid string) map[string]string {
	return map[string]string{
		"Accept":           "application/json, text/plain, */*",
		"Referer":          fmt.Sprintf("%s/company/%s", baseURL, pid),
		"X-Requested-With": "XMLHttpRequest",
	}
}

// Name 返回数据源标识名。
func (c *Client) Name() string {
	return "aiqicha"
}

// SearchCompany 根据关键词搜索企业。
// 通过爱企查搜索页面获取企业列表，解析页面内嵌的 JSON 数据。
func (c *Client) SearchCompany(ctx context.Context, keyword string) ([]model.Company, error) {
	reqURL := fmt.Sprintf("%s%s?q=%s&t=0", baseURL, searchPath, url.QueryEscape(keyword))

	body, err := c.base.DoGet(ctx, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("搜索企业失败: %w", err)
	}

	companies, err := parseSearchHTML(body)
	if err != nil {
		return nil, fmt.Errorf("解析搜索结果失败: %w", err)
	}

	return companies, nil
}

// searchAndGetPID 搜索企业并返回第一个匹配结果的 PID。
// 这是其他查询方法的前置步骤。
func (c *Client) searchAndGetPID(ctx context.Context, companyName string) (string, error) {
	companies, err := c.SearchCompany(ctx, companyName)
	if err != nil {
		return "", err
	}
	if len(companies) == 0 {
		return "", fmt.Errorf("未找到企业: %s", companyName)
	}
	return companies[0].CompanyID, nil
}

// QueryICP 查询企业 ICP 备案记录。
// 先搜索获取 PID，再调用 ICP 接口。
func (c *Client) QueryICP(ctx context.Context, companyName string) ([]model.ICPRecord, error) {
	pid, err := c.searchAndGetPID(ctx, companyName)
	if err != nil {
		return nil, err
	}

	reqURL := fmt.Sprintf("%s%s?pid=%s", baseURL, icpPath, pid)
	body, err := c.base.DoGet(ctx, reqURL, c.ajaxHeaders(pid))
	if err != nil {
		return nil, fmt.Errorf("查询 ICP 备案失败: %w", err)
	}

	records, err := parseICPResponse(body, companyName)
	if err != nil {
		return nil, err
	}

	return records, nil
}

// QueryInvestments 查询企业对外投资，支持分页获取全部数据。
func (c *Client) QueryInvestments(ctx context.Context, companyID string) ([]model.Investment, error) {
	var allInvestments []model.Investment
	pageSize := 100
	page := 1

	for {
		reqURL := fmt.Sprintf("%s%s?pid=%s&p=%d&size=%d",
			baseURL, investPath, companyID, page, pageSize)

		body, err := c.base.DoGet(ctx, reqURL, c.ajaxHeaders(companyID))
		if err != nil {
			return nil, fmt.Errorf("查询对外投资失败 (第%d页): %w", page, err)
		}

		investments, total, err := parseInvestResponse(body)
		if err != nil {
			return nil, err
		}

		allInvestments = append(allInvestments, investments...)

		// 判断是否还有更多页
		if len(allInvestments) >= total || len(investments) == 0 {
			break
		}
		page++
	}

	return allInvestments, nil
}

// QueryMiniPrograms 爱企查暂不支持小程序独立查询。
func (c *Client) QueryMiniPrograms(_ context.Context, _ string) ([]model.MiniProgram, error) {
	return nil, source.ErrNotSupported
}

// QueryOfficialAccounts 查询企业名下的微信公众号。
func (c *Client) QueryOfficialAccounts(ctx context.Context, companyName string) ([]model.OfficialAccount, error) {
	pid, err := c.searchAndGetPID(ctx, companyName)
	if err != nil {
		return nil, err
	}

	reqURL := fmt.Sprintf("%s%s?pid=%s&size=10&p=1", baseURL, wechatPath, pid)
	body, err := c.base.DoGet(ctx, reqURL, c.ajaxHeaders(pid))
	if err != nil {
		return nil, fmt.Errorf("查询公众号失败: %w", err)
	}

	accounts, err := parseOfficialAccountResponse(body, companyName)
	if err != nil {
		return nil, err
	}

	return accounts, nil
}

// QueryApps 查询企业名下的 APP。
func (c *Client) QueryApps(ctx context.Context, companyName string) ([]model.AppInfo, error) {
	pid, err := c.searchAndGetPID(ctx, companyName)
	if err != nil {
		return nil, err
	}

	reqURL := fmt.Sprintf("%s%s?pid=%s&size=10&p=1", baseURL, appPath, pid)
	body, err := c.base.DoGet(ctx, reqURL, c.ajaxHeaders(pid))
	if err != nil {
		return nil, fmt.Errorf("查询 APP 失败: %w", err)
	}

	apps, err := parseAppResponse(body, companyName)
	if err != nil {
		return nil, err
	}

	return apps, nil
}

// 确保未使用的常量不报错。
func init() {
	_ = basicInfoPath
}

// 确保 Client 实现了 Source 接口（编译期检查）。
var _ source.Source = (*Client)(nil)

// ────────────────────────────────────────
// 内部辅助
// ────────────────────────────────────────

// parsePercentage 将百分比字符串转换为浮点数。
// 例如 "51.00%" → 51.0，解析失败返回 0。
func parsePercentage(s string) float64 {
	// 去掉 % 符号
	if len(s) > 0 && s[len(s)-1] == '%' {
		s = s[:len(s)-1]
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}
