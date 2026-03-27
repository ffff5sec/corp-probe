package yiqicha

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/ffff5sec/corp-probe/internal/model"
)

// ────────────────────────────────────────
// 通用响应结构
// ────────────────────────────────────────

// apiResponse 亿企查 API 通用响应格式。
type apiResponse struct {
	Code    string          `json:"code"`    // "0000" 表示成功
	Msg     string          `json:"msg"`
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
}

// checkResponse 检查 API 响应状态。
func checkResponse(body []byte) (*apiResponse, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("响应为空")
	}
	var resp apiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if resp.Code != "0000" || !resp.Success {
		return nil, fmt.Errorf("API 错误: %s (code: %s)", resp.Msg, resp.Code)
	}
	return &resp, nil
}

// ────────────────────────────────────────
// 搜索结果解析
// ────────────────────────────────────────

// parseSearchResponse 解析企业搜索响应。
func parseSearchResponse(body []byte) ([]model.Company, error) {
	resp, err := checkResponse(body)
	if err != nil {
		return nil, fmt.Errorf("搜索: %w", err)
	}

	var data struct {
		LeftList []searchItem `json:"leftList"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("解析搜索数据失败: %w", err)
	}

	var companies []model.Company
	for _, item := range data.LeftList {
		companies = append(companies, model.Company{
			Name:      item.EntNameNormal,
			CompanyID: item.PID,
			Source:    "yiqicha",
		})
	}

	return companies, nil
}

type searchItem struct {
	PID           string `json:"pid"`
	EntName       string `json:"entName"`       // 含 <em> 标签
	EntNameNormal string `json:"entNameNormal"` // 纯文本名称
	Logo          string `json:"logo"`
	Tag           string `json:"tag"`
}

// ────────────────────────────────────────
// ICP 备案解析
// ────────────────────────────────────────

// parseICPResponse 解析 ICP 备案查询响应。
func parseICPResponse(body []byte, companyName string) ([]model.ICPRecord, error) {
	resp, err := checkResponse(body)
	if err != nil {
		return nil, fmt.Errorf("ICP 查询: %w", err)
	}

	var data struct {
		List       []icpItem `json:"list"`
		TotalCount int       `json:"totalCount"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("解析 ICP 数据失败: %w", err)
	}

	var records []model.ICPRecord
	for _, item := range data.List {
		records = append(records, model.ICPRecord{
			ICPNumber:    item.RecordId,
			Domain:       item.SiteDomain,
			SiteName:     item.SiteName,
			SiteType:     item.RecordExpiredName,
			ApprovalDate: item.CheckDate,
			CompanyName:  companyName,
		})
	}

	return records, nil
}

type icpItem struct {
	RecordId          string `json:"recordId"`          // 备案号（如 京ICP备06041231号-1）
	Record            string `json:"record"`            // 备案主体号
	SiteDomain        string `json:"siteDomain"`        // 域名
	SiteName          string `json:"siteName"`          // 网站名称
	SiteHome          string `json:"siteHome"`          // 网站首页
	RecordExpired     int    `json:"recordExpired"`     // 是否过期
	RecordExpiredName string `json:"recordExpiredName"` // 有效/过期
	CheckDate         string `json:"checkDate"`         // 审核日期
	CloudVendorsType  string `json:"cloudVendorsType"`  // 云服务商
}

// ────────────────────────────────────────
// 对外投资解析
// ────────────────────────────────────────

// parseInvestResponse 解析对外投资查询响应，返回投资列表和总数。
func parseInvestResponse(body []byte) ([]model.Investment, int, error) {
	resp, err := checkResponse(body)
	if err != nil {
		return nil, 0, fmt.Errorf("投资查询: %w", err)
	}

	var data struct {
		List  []investItem `json:"list"`
		Total int          `json:"total"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, 0, fmt.Errorf("解析投资数据失败: %w", err)
	}

	var investments []model.Investment
	for _, item := range data.List {
		investments = append(investments, model.Investment{
			CompanyName: cleanHTML(item.EntName),
			CreditCode:  item.CreditCode,
			Ratio:       parsePercentage(item.InvestRate),
			Amount:      item.InvestMoney,
			LegalPerson: item.LegalPerson,
			Status:      item.EntStatus,
			CompanyID:   item.PID,
		})
	}

	return investments, data.Total, nil
}

type investItem struct {
	PID         string `json:"pid"`
	EntName     string `json:"entName"`
	CreditCode  string `json:"creditCode"`
	InvestRate  string `json:"investRate"`  // 投资比例
	InvestMoney string `json:"investMoney"` // 投资金额
	LegalPerson string `json:"legalPerson"`
	EntStatus   string `json:"entStatus"`   // 经营状态
}

// ────────────────────────────────────────
// 微信公众号解析
// ────────────────────────────────────────

// parseWechatResponse 解析微信公众号查询响应。
func parseWechatResponse(body []byte, companyName string) ([]model.OfficialAccount, error) {
	resp, err := checkResponse(body)
	if err != nil {
		return nil, fmt.Errorf("公众号查询: %w", err)
	}

	var data struct {
		List []wechatItem `json:"list"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("解析公众号数据失败: %w", err)
	}

	var accounts []model.OfficialAccount
	for _, item := range data.List {
		accounts = append(accounts, model.OfficialAccount{
			Name:        item.WechatName,
			WechatID:    item.WechatId,
			Description: item.WechatIntroduction,
			QRCode:      item.QrCode,
			CompanyName: companyName,
		})
	}

	return accounts, nil
}

type wechatItem struct {
	WechatName         string `json:"wechatName"`
	WechatId           string `json:"wechatId"`
	WechatIntroduction string `json:"wechatIntroduction"`
	QrCode             string `json:"qrCode"`
}

// ────────────────────────────────────────
// 工具函数
// ────────────────────────────────────────

// cleanHTML 移除 HTML 标签。
func cleanHTML(s string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	return re.ReplaceAllString(s, "")
}

// parsePercentage 将百分比字符串转换为浮点数。
func parsePercentage(s string) float64 {
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, "%")
	var v float64
	fmt.Sscanf(s, "%f", &v)
	return v
}
