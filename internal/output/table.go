package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/ffff5sec/corp-probe/internal/model"
	"github.com/rodaine/table"
)

// WriteTable 将查询结果以 CLI 表格形式输出。
// 缓存数据会在备注列标注 [缓存: 日期]。
func WriteTable(w io.Writer, result *model.QueryResult) {
	// 企业基本信息
	fmt.Fprintf(w, "\n=== 企业信息 ===\n")
	tbl := table.New("名称", "信用代码", "法人", "注册资本", "状态").WithWriter(w)
	tbl.AddRow(
		result.Company.Name,
		result.Company.CreditCode,
		result.Company.LegalPerson,
		result.Company.RegisteredCapital,
		result.Company.Status,
	)
	tbl.Print()

	// ICP 备案
	if result.ICPRecords != nil && len(result.ICPRecords.Data) > 0 {
		fmt.Fprintf(w, "\n=== ICP 备案 (%d 条) ===\n", len(result.ICPRecords.Data))
		tbl = table.New("备案号", "域名", "网站名称", "类型", "审核日期", "所属企业", "备注").WithWriter(w)
		for i, r := range result.ICPRecords.Data {
			tbl.AddRow(
				r.ICPNumber, r.Domain, r.SiteName, r.SiteType,
				r.ApprovalDate, r.CompanyName, cacheNote(result.ICPRecords.Meta[i]),
			)
		}
		tbl.Print()
	}

	// 股权穿透树
	if result.EquityTree != nil && len(result.EquityTree.Children) > 0 {
		fmt.Fprintf(w, "\n=== 股权穿透 ===\n")
		tbl = table.New("层级", "关系", "企业名称", "持股比例", "法人", "状态").WithWriter(w)
		renderEquityTree(tbl, result.EquityTree.Children, 0)
		tbl.Print()
	}

	// APP
	if result.Apps != nil && len(result.Apps.Data) > 0 {
		fmt.Fprintf(w, "\n=== 移动应用 (%d 个) ===\n", len(result.Apps.Data))
		tbl = table.New("名称", "包名", "平台", "版本", "开发者", "备注").WithWriter(w)
		for i, a := range result.Apps.Data {
			tbl.AddRow(
				a.Name, a.BundleID, a.Platform, a.Version,
				a.Developer, cacheNote(result.Apps.Meta[i]),
			)
		}
		tbl.Print()
	}

	// 小程序
	if result.MiniPrograms != nil && len(result.MiniPrograms.Data) > 0 {
		fmt.Fprintf(w, "\n=== 小程序 (%d 个) ===\n", len(result.MiniPrograms.Data))
		tbl = table.New("名称", "平台", "AppID", "简介", "备注").WithWriter(w)
		for i, p := range result.MiniPrograms.Data {
			tbl.AddRow(
				p.Name, p.Platform, p.AppID,
				truncate(p.Description, 30), cacheNote(result.MiniPrograms.Meta[i]),
			)
		}
		tbl.Print()
	}

	// 公众号
	if result.OfficialAccounts != nil && len(result.OfficialAccounts.Data) > 0 {
		fmt.Fprintf(w, "\n=== 公众号 (%d 个) ===\n", len(result.OfficialAccounts.Data))
		tbl = table.New("名称", "微信号", "简介", "备注").WithWriter(w)
		for i, a := range result.OfficialAccounts.Data {
			tbl.AddRow(
				a.Name, a.WechatID,
				truncate(a.Description, 40), cacheNote(result.OfficialAccounts.Meta[i]),
			)
		}
		tbl.Print()
	}
}

// renderEquityTree 递归渲染股权树到表格。
func renderEquityTree(tbl table.Table, nodes []model.EquityNode, indent int) {
	for _, node := range nodes {
		prefix := strings.Repeat("  ", indent) + "├── "
		tbl.AddRow(
			fmt.Sprintf("%d", node.Investment.Depth),
			prefix,
			node.Investment.CompanyName,
			fmt.Sprintf("%.2f%%", node.Investment.Ratio),
			node.Investment.LegalPerson,
			node.Investment.Status,
		)
		if len(node.Children) > 0 {
			renderEquityTree(tbl, node.Children, indent+1)
		}
	}
}

// cacheNote 根据元数据生成缓存标注。
func cacheNote(meta model.Meta) string {
	if meta.FromCache {
		return fmt.Sprintf("[缓存: %s]", meta.UpdatedAt.Format("2006-01-02"))
	}
	return ""
}

// truncate 截断过长的字符串。
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
