// Package yiqicha 实现亿企查（www.yiqicha.com）数据源适配器。
// 纯 JSON API，使用 JWT Token 认证，支持企业搜索、ICP 备案、对外投资、公众号等。
package yiqicha

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ffff5sec/corp-probe/internal/model"
	"github.com/ffff5sec/corp-probe/internal/source"
)

const (
	baseURL = "https://www.yiqicha.com"

	// API 端点
	searchAPI     = "/api/search/api/v1/index/dropDownSearch"          // 企业搜索
	headerAPI     = "/api/yqc/api/v1/business/header"                 // 企业头信息
	comInfoAPI    = "/api/yqc/api/v1/business/comInfo"                // 企业详情
	icpListAPI    = "/api/yqc/api/v1/datalist/getInWebDomainRecordList" // ICP 备案列表
	investAPI     = "/api/yqc/api/v1/business/investList"             // 对外投资
	controlEpAPI  = "/api/yqc/api/v1/business/controlEpList"          // 控股企业
	branchAPI     = "/api/yqc/api/v1/business/branchList"             // 分支机构
	wechatAPI     = "/api/yqc/api/v1/datalist/getWechatAccountInfoList" // 微信公众号
	appAPI        = "/api/yqc/api/v1/datalist/getBuExpoInfoList"      // APP/应用
)

// Client 亿企查数据源客户端。
type Client struct {
	base  *source.BaseClient
	token string // JWT 认证令牌
	cid   string // 客户端标识
}

// New 创建亿企查客户端。
// token 为 JWT 令牌，cid 为客户端标识（如 "yqc-pc-pc-xxx"）。
func New(token, cid, proxy string, timeout, delay time.Duration, retry int) *Client {
	return &Client{
		base:  source.NewBaseClient("", proxy, timeout, delay, retry),
		token: token,
		cid:   cid,
	}
}

// Name 返回数据源标识名。
func (c *Client) Name() string {
	return "yiqicha"
}

// headers 返回亿企查 API 请求需要的公共请求头。
func (c *Client) headers() map[string]string {
	return map[string]string{
		"token":          c.token,
		"cid":            c.cid,
		"browertype":     "Chrome",
		"browerversion":  "146.0.0.0",
		"content-type":   "application/json",
		"accept":         "application/json",
		"version":        "1.1",
		"Referer":        "https://www.yiqicha.com/",
		"Origin":         "https://www.yiqicha.com",
	}
}

// postJSON 发起 POST 请求并返回响应体。
func (c *Client) postJSON(ctx context.Context, path string, body interface{}) ([]byte, error) {
	reqURL := baseURL + path
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("序列化请求体失败: %w", err)
	}
	return c.base.DoPost(ctx, reqURL, jsonBody, c.headers())
}

// getJSON 发起 GET 请求并返回响应体。
func (c *Client) getJSON(ctx context.Context, path string) ([]byte, error) {
	reqURL := baseURL + path
	return c.base.DoGet(ctx, reqURL, c.headers())
}

// SearchCompany 根据关键词搜索企业。
func (c *Client) SearchCompany(ctx context.Context, keyword string) ([]model.Company, error) {
	body := map[string]interface{}{
		"keyword":     keyword,
		"pageCurrent": "1",
		"pageSize":    "10",
	}

	resp, err := c.postJSON(ctx, searchAPI, body)
	if err != nil {
		return nil, fmt.Errorf("搜索企业失败: %w", err)
	}

	companies, err := parseSearchResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("解析搜索结果失败: %w", err)
	}

	return companies, nil
}

// searchAndGetPID 搜索企业并返回第一个匹配结果的 PID。
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
func (c *Client) QueryICP(ctx context.Context, companyName string) ([]model.ICPRecord, error) {
	pid, err := c.searchAndGetPID(ctx, companyName)
	if err != nil {
		return nil, err
	}

	body := map[string]interface{}{
		"pid":              pid,
		"pageCurrent":      1,
		"pageSize":         50,
		"startRow":         0,
		"isHistory":        "0",
		"keyword":          "",
		"cloudVendorsType": nil,
		"sort":             "",
		"sortField":        "",
	}

	resp, err := c.postJSON(ctx, icpListAPI, body)
	if err != nil {
		return nil, fmt.Errorf("查询 ICP 备案失败: %w", err)
	}

	records, err := parseICPResponse(resp, companyName)
	if err != nil {
		return nil, err
	}

	return records, nil
}

// QueryInvestments 查询企业控股企业（用于股权穿透）。
// 使用 controlEpList 接口获取控股子公司及持股比例。
func (c *Client) QueryInvestments(ctx context.Context, companyID string) ([]model.Investment, error) {
	var allInvestments []model.Investment
	page := 1

	for {
		body := map[string]interface{}{
			"pid":                companyID,
			"pageCurrent":        page,
			"pageSize":           20,
			"startRow":           (page - 1) * 20,
			"entName":            "",
			"entStatus":          "",
			"sort":               "",
			"sortField":          "",
			"cityCode":           "",
			"districtCode":       "",
			"provinceCode":       "",
			"industryFirstCode":  "",
			"industrySecondCode": "",
			"industryThirdCode":  "",
		}

		resp, err := c.postJSON(ctx, controlEpAPI, body)
		if err != nil {
			return nil, fmt.Errorf("查询控股企业失败 (第%d页): %w", page, err)
		}

		investments, total, err := parseInvestResponse(resp)
		if err != nil {
			return nil, err
		}

		allInvestments = append(allInvestments, investments...)

		if len(allInvestments) >= total || len(investments) == 0 {
			break
		}
		page++
	}

	return allInvestments, nil
}

// QueryApps 查询企业名下的移动应用。
func (c *Client) QueryApps(_ context.Context, _ string) ([]model.AppInfo, error) {
	return nil, source.ErrNotSupported
}

// QueryMiniPrograms 亿企查暂不支持小程序查询。
func (c *Client) QueryMiniPrograms(_ context.Context, _ string) ([]model.MiniProgram, error) {
	return nil, source.ErrNotSupported
}

// QueryOfficialAccounts 查询企业名下的微信公众号。
func (c *Client) QueryOfficialAccounts(ctx context.Context, companyName string) ([]model.OfficialAccount, error) {
	pid, err := c.searchAndGetPID(ctx, companyName)
	if err != nil {
		return nil, err
	}

	body := map[string]interface{}{
		"pid":         pid,
		"pageCurrent": 1,
		"pageSize":    20,
	}

	resp, err := c.postJSON(ctx, wechatAPI, body)
	if err != nil {
		return nil, fmt.Errorf("查询公众号失败: %w", err)
	}

	accounts, err := parseWechatResponse(resp, companyName)
	if err != nil {
		return nil, err
	}

	return accounts, nil
}

// 确保 Client 实现了 Source 接口（编译期检查）。
var _ source.Source = (*Client)(nil)
