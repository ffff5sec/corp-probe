package aiqicha

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/ffff5sec/corp-probe/internal/model"
	"github.com/ffff5sec/corp-probe/internal/source"
)

// ────────────────────────────────────────
// 搜索页面解析
// ────────────────────────────────────────

// extractPageData 从 HTML 中提取 window.pageData 与 window.isSpider 之间的 JSON。
// 与 ENScan_GO 对齐：使用 isSpider 标记作为结束分隔符。
func extractPageData(body []byte) ([]byte, error) {
	html := string(body)

	tag1 := "window.pageData ="
	tag2 := "window.isSpider ="

	idx1 := strings.Index(html, tag1)
	idx2 := strings.Index(html, tag2)

	if idx1 < 0 || idx2 < 0 || idx2 <= idx1 {
		// 回退：用括号计数法
		return extractPageDataByBracket(body)
	}

	// 提取两个标记之间的内容
	str := html[idx1+len(tag1) : idx2]
	str = strings.TrimSpace(str)
	// 去掉末尾的分号
	str = strings.TrimRight(str, "; \n\r\t")

	if len(str) == 0 || str[0] != '{' {
		return nil, fmt.Errorf("pageData 格式异常")
	}

	return []byte(str), nil
}

// extractPageDataByBracket 括号计数法提取 pageData（回退方案）。
func extractPageDataByBracket(body []byte) ([]byte, error) {
	html := string(body)
	marker := "window.pageData"
	idx := strings.Index(html, marker)
	if idx < 0 {
		return nil, fmt.Errorf("页面中未找到 pageData")
	}

	start := strings.Index(html[idx:], "{")
	if start < 0 {
		return nil, fmt.Errorf("pageData 格式异常")
	}
	start += idx

	depth := 0
	for i := start; i < len(html); i++ {
		switch html[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return []byte(html[start : i+1]), nil
			}
		}
	}
	return nil, fmt.Errorf("pageData JSON 未闭合")
}

// parseSearchHTML 从爱企查搜索页面 HTML 中提取企业列表。
func parseSearchHTML(body []byte) ([]model.Company, error) {
	jsonData, err := extractPageData(body)
	if err != nil {
		html := string(body)
		if strings.Contains(html, "accessrestriction") {
			return nil, fmt.Errorf("IP 被限制访问，请配置国内代理")
		}
		if strings.Contains(html, "百度安全验证") {
			return nil, fmt.Errorf("触发安全验证，请在浏览器中完成验证")
		}
		if strings.Contains(html, "登录") && !strings.Contains(html, "isLogin") {
			return nil, source.ErrAuthExpired
		}
		return nil, err
	}

	// 解析外层 JSON，提取 result.resultList
	var pageData struct {
		Result struct {
			ResultList []searchResult `json:"resultList"`
		} `json:"result"`
	}
	if err := json.Unmarshal(jsonData, &pageData); err != nil {
		return nil, fmt.Errorf("解析搜索 JSON 失败: %w", err)
	}

	var companies []model.Company
	for _, r := range pageData.Result.ResultList {
		name := r.TitleName
		if name == "" {
			name = cleanHTML(r.EntName)
		}
		companies = append(companies, model.Company{
			Name:              name,
			CreditCode:        r.RegNo,
			LegalPerson:       r.LegalPerson,
			RegisteredCapital: r.RegCap,
			Status:            r.OpenStatus,
			EstablishedDate:   r.ValidityFrom,
			CompanyID:         r.PID,
			Source:            "aiqicha",
		})
	}

	return companies, nil
}

// searchResult 搜索接口返回的单条企业数据。
type searchResult struct {
	PID          string `json:"pid"`
	EntName      string `json:"entName"`      // 含 <em> 标签的企业名称
	TitleName    string `json:"titleName"`     // 纯文本企业名称
	RegNo        string `json:"regNo"`         // 统一社会信用代码
	LegalPerson  string `json:"legalPerson"`   // 法定代表人
	RegCap       string `json:"regCap"`        // 注册资本
	OpenStatus   string `json:"openStatus"`    // 经营状态
	ValidityFrom string `json:"validityFrom"`  // 成立日期
}

// ────────────────────────────────────────
// 标准 JSON API 响应解析
// ────────────────────────────────────────

// apiResponse 爱企查 AJAX 接口的通用响应格式。
type apiResponse struct {
	Status int             `json:"status"`
	Data   json.RawMessage `json:"data"`
}

// checkAPIResponse 检查 API 响应状态码。
func checkAPIResponse(body []byte) (*apiResponse, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("响应为空（可能需要 TLS 指纹伪装或 IP 被限制）")
	}
	var resp apiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析 API 响应失败: %w", err)
	}
	if resp.Status != 0 {
		return nil, source.ErrAuthExpired
	}
	return &resp, nil
}

// ────────────────────────────────────────
// ICP 备案解析
// ────────────────────────────────────────

// parseICPResponse 解析 ICP 备案查询响应。
// domain 字段可能是字符串或 JSON 数组，需要兼容处理。
func parseICPResponse(body []byte, companyName string) ([]model.ICPRecord, error) {
	resp, err := checkAPIResponse(body)
	if err != nil {
		return nil, fmt.Errorf("ICP 查询: %w", err)
	}

	var data struct {
		List []json.RawMessage `json:"list"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("解析 ICP 数据失败: %w", err)
	}

	var records []model.ICPRecord
	for _, raw := range data.List {
		var item icpItem
		if err := json.Unmarshal(raw, &item); err != nil {
			continue
		}

		// domain 可能是字符串或 JSON 数组
		domains := parseDomainField(raw)
		for _, domain := range domains {
			records = append(records, model.ICPRecord{
				ICPNumber:    item.IcpNo,
				Domain:       domain,
				SiteName:     item.SiteName,
				SiteType:     item.NatureName,
				ApprovalDate: item.AuditDate,
				CompanyName:  companyName,
			})
		}
	}

	return records, nil
}

type icpItem struct {
	IcpNo      string `json:"icpNo"`      // 备案号
	SiteName   string `json:"siteName"`    // 网站名称
	NatureName string `json:"natureName"`  // 备案类型
	AuditDate  string `json:"auditDate"`   // 审核日期
}

// parseDomainField 解析 domain 字段。
// ENScan_GO 显示 domain 是一个 JSON 数组，而非普通字符串。
func parseDomainField(raw json.RawMessage) []string {
	// 先尝试提取 domain 字段
	var obj struct {
		Domain json.RawMessage `json:"domain"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil || obj.Domain == nil {
		return nil
	}

	// 尝试解析为数组
	var arr []string
	if err := json.Unmarshal(obj.Domain, &arr); err == nil {
		return arr
	}

	// 尝试解析为字符串
	var s string
	if err := json.Unmarshal(obj.Domain, &s); err == nil {
		return splitDomains(s)
	}

	return nil
}

// ────────────────────────────────────────
// 对外投资解析
// ────────────────────────────────────────

func parseInvestResponse(body []byte) ([]model.Investment, int, error) {
	resp, err := checkAPIResponse(body)
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
			CompanyName: item.EntName,
			CreditCode:  item.RegNo,
			Ratio:       parsePercentage(item.RegRate),
			Amount:      item.RegCapital,
			LegalPerson: item.LegalPerson,
			Status:      item.OpenStatus,
			CompanyID:   item.PID,
		})
	}

	return investments, data.Total, nil
}

type investItem struct {
	PID         string `json:"pid"`
	EntName     string `json:"entName"`
	RegNo       string `json:"regNo"`
	RegRate     string `json:"regRate"`     // ENScan_GO 用 regRate
	RegCapital  string `json:"regCapital"`
	LegalPerson string `json:"legalPerson"`
	OpenStatus  string `json:"openStatus"`
}

// ────────────────────────────────────────
// 微信公众号解析
// ────────────────────────────────────────

func parseOfficialAccountResponse(body []byte, companyName string) ([]model.OfficialAccount, error) {
	resp, err := checkAPIResponse(body)
	if err != nil {
		return nil, fmt.Errorf("公众号查询: %w", err)
	}

	var data struct {
		List []officialAccountItem `json:"list"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("解析公众号数据失败: %w", err)
	}

	var accounts []model.OfficialAccount
	for _, item := range data.List {
		accounts = append(accounts, model.OfficialAccount{
			Name:        item.WechatName,
			WechatID:    item.WechatID,
			Description: item.WechatIntruduction,
			QRCode:      item.QRCode,
			CompanyName: companyName,
		})
	}

	return accounts, nil
}

type officialAccountItem struct {
	WechatName        string `json:"wechatName"`
	WechatID          string `json:"wechatId"`
	WechatIntruduction string `json:"wechatIntruduction"` // 注意：爱企查的拼写是 Intruduction
	QRCode            string `json:"qrcode"`
}

// ────────────────────────────────────────
// APP 解析
// ────────────────────────────────────────

func parseAppResponse(body []byte, companyName string) ([]model.AppInfo, error) {
	resp, err := checkAPIResponse(body)
	if err != nil {
		return nil, fmt.Errorf("APP 查询: %w", err)
	}

	var data struct {
		List []appItem `json:"list"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("解析 APP 数据失败: %w", err)
	}

	var apps []model.AppInfo
	for _, item := range data.List {
		apps = append(apps, model.AppInfo{
			Name:        item.Name,
			BundleID:    item.BundleID,
			Platform:    item.Classify,
			Description: item.LogoBrief,
			Version:     item.Version,
			Developer:   companyName,
		})
	}

	return apps, nil
}

type appItem struct {
	Name      string `json:"name"`
	Classify  string `json:"classify"`
	Version   string `json:"version"`
	LogoBrief string `json:"logoBrief"`
	BundleID  string `json:"bundleId"`
}

// ────────────────────────────────────────
// 工具函数
// ────────────────────────────────────────

// cleanHTML 移除 HTML 标签（如 <em> 高亮标记）。
func cleanHTML(s string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	return re.ReplaceAllString(s, "")
}

// splitDomains 将可能包含多个域名的字符串拆分为切片。
func splitDomains(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	for _, sep := range []string{",", ";", "，", "；", " "} {
		if strings.Contains(s, sep) {
			var result []string
			for _, p := range strings.Split(s, sep) {
				p = strings.TrimSpace(p)
				if p != "" {
					result = append(result, p)
				}
			}
			return result
		}
	}
	return []string{s}
}
