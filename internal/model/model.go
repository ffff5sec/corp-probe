// Package model 定义 corp-probe 所有核心数据结构。
// 各层（source、engine、cache、output）均依赖本包，本包不依赖任何内部包。
package model

import "time"

// ────────────────────────────────────────
// 通用元数据
// ────────────────────────────────────────

// Meta 记录每条数据的来源与时效信息，用于缓存标注和输出展示。
type Meta struct {
	Source    string    `json:"source"`     // 数据来源，如 "aiqicha"
	FromCache bool     `json:"from_cache"` // 是否来自本地缓存
	UpdatedAt time.Time `json:"updated_at"` // 数据获取/更新时间
}

// ResultWrapper 泛型结果包装器。
// Data 与 Meta 为平行数组（长度一致），保持模型结构体干净的同时携带溯源信息。
type ResultWrapper[T any] struct {
	Data []T    `json:"data"`
	Meta []Meta `json:"_meta"`
}

// ────────────────────────────────────────
// 企业基本信息
// ────────────────────────────────────────

// Company 企业搜索结果。
type Company struct {
	Name              string `json:"name"`               // 企业名称
	CreditCode        string `json:"credit_code"`        // 统一社会信用代码
	LegalPerson       string `json:"legal_person"`       // 法定代表人
	RegisteredCapital string `json:"registered_capital"` // 注册资本
	Status            string `json:"status"`             // 经营状态
	EstablishedDate   string `json:"established_date"`   // 成立日期
	CompanyID         string `json:"company_id"`         // 数据源内部ID（如爱企查PID）
	Source            string `json:"source"`             // 数据来源标识
}

// ────────────────────────────────────────
// ICP 备案
// ────────────────────────────────────────

// ICPRecord ICP 备案记录。
type ICPRecord struct {
	ICPNumber    string `json:"icp_number"`    // 备案号
	Domain       string `json:"domain"`        // 域名
	SiteName     string `json:"site_name"`     // 网站名称
	SiteType     string `json:"site_type"`     // 备案类型
	ApprovalDate string `json:"approval_date"` // 审核通过日期
	CompanyName  string `json:"company_name"`  // 所属企业名称
}

// ────────────────────────────────────────
// 股权投资
// ────────────────────────────────────────

// Investment 对外投资记录，股权穿透引擎的基础数据。
type Investment struct {
	CompanyName   string  `json:"company_name"`   // 被投资企业名称
	CreditCode    string  `json:"credit_code"`    // 被投资企业信用代码
	Ratio         float64 `json:"ratio"`          // 持股比例（0-100）
	Amount        string  `json:"amount"`         // 投资金额
	LegalPerson   string  `json:"legal_person"`   // 法定代表人
	Status        string  `json:"status"`         // 经营状态
	CompanyID     string  `json:"company_id"`     // 数据源内部ID
	Depth         int     `json:"depth"`          // 穿透层级（引擎填充）
	ParentCompany string  `json:"parent_company"` // 母公司名称（引擎填充）
}

// ────────────────────────────────────────
// 股权穿透树
// ────────────────────────────────────────

// EquityTree 股权穿透结果，以目标企业为根节点的树结构。
type EquityTree struct {
	Root     Company      `json:"root"`
	Children []EquityNode `json:"children"`
}

// EquityNode 股权树中的节点。
type EquityNode struct {
	Investment Investment   `json:"investment"`
	Children   []EquityNode `json:"children,omitempty"`
}

// ────────────────────────────────────────
// APP / 小程序 / 公众号
// ────────────────────────────────────────

// AppInfo 移动应用信息。
type AppInfo struct {
	Name        string `json:"name"`         // 应用名称
	BundleID    string `json:"bundle_id"`    // 包名 / Bundle ID
	Platform    string `json:"platform"`     // 平台：ios / android
	Description string `json:"description"`  // 应用简介
	DownloadURL string `json:"download_url"` // 下载链接
	Version     string `json:"version"`      // 版本号
	Developer   string `json:"developer"`    // 开发者名称
}

// MiniProgram 小程序信息（微信/百度/支付宝）。
type MiniProgram struct {
	Name        string `json:"name"`         // 小程序名称
	Platform    string `json:"platform"`     // 平台：wechat / baidu / alipay
	AppID       string `json:"app_id"`       // 小程序ID
	Description string `json:"description"`  // 简介
	CompanyName string `json:"company_name"` // 所属企业
}

// OfficialAccount 微信公众号信息。
type OfficialAccount struct {
	Name        string `json:"name"`         // 公众号名称
	WechatID    string `json:"wechat_id"`    // 微信号
	Description string `json:"description"`  // 简介
	QRCode      string `json:"qr_code"`      // 二维码链接
	CompanyName string `json:"company_name"` // 所属企业
}

// ────────────────────────────────────────
// 聚合查询结果
// ────────────────────────────────────────

// QueryResult 单个企业的完整查询结果，由引擎层组装。
type QueryResult struct {
	Company          Company                          `json:"company"`
	ICPRecords       *ResultWrapper[ICPRecord]        `json:"icp_records,omitempty"`
	Investments      *ResultWrapper[Investment]       `json:"investments,omitempty"`
	EquityTree       *EquityTree                      `json:"equity_tree,omitempty"`
	Apps             *ResultWrapper[AppInfo]           `json:"apps,omitempty"`
	MiniPrograms     *ResultWrapper[MiniProgram]      `json:"mini_programs,omitempty"`
	OfficialAccounts *ResultWrapper[OfficialAccount]  `json:"official_accounts,omitempty"`
}
