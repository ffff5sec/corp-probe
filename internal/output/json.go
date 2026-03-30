// Package output 提供查询结果的格式化输出。
// 支持 JSON、CLI 表格等格式，自动标注缓存数据的来源和时间。
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ffff5sec/corp-probe/internal/model"
)

// WriteJSON 将查询结果以缩进 JSON 格式写入 writer。
func WriteJSON(w io.Writer, results []model.QueryResult) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(results)
}

// WriteJSONFile 将查询结果写入 JSON 文件。
// 返回生成的文件路径。
func WriteJSONFile(dir string, companyName string, results []model.QueryResult) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建输出目录失败: %w", err)
	}

	filename := fmt.Sprintf("%s_%s.json", companyName, time.Now().Format("20060102_150405"))
	filePath := filepath.Join(dir, filename)

	f, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("创建输出文件失败: %w", err)
	}
	defer f.Close()

	if err := WriteJSON(f, results); err != nil {
		return "", fmt.Errorf("写入 JSON 失败: %w", err)
	}

	return filePath, nil
}

// ────────────────────────────────────────
// JSONL 输出（每行一条资产记录，供下游模块消费）
// ────────────────────────────────────────

// AssetLine JSONL 单行资产记录，用于模块间管道传递。
type AssetLine struct {
	Type    string `json:"type"`              // domain / app / wechat / miniprogram
	Value   string `json:"value"`             // 资产值（域名/APP名等）
	Company string `json:"company"`           // 所属企业
	Source  string `json:"source,omitempty"`   // 数据来源
	Extra   map[string]string `json:"extra,omitempty"` // 扩展字段
}

// WriteJSONL 将查询结果以 JSONL 格式输出，每行一条资产记录。
func WriteJSONL(w io.Writer, results []model.QueryResult) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)

	for _, r := range results {
		company := r.Company.Name

		// ICP 域名
		if r.ICPRecords != nil {
			for _, icp := range r.ICPRecords.Data {
				_ = encoder.Encode(AssetLine{
					Type:    "domain",
					Value:   icp.Domain,
					Company: icp.CompanyName,
					Source:  "icp",
					Extra: map[string]string{
						"icp_number": icp.ICPNumber,
						"site_name":  icp.SiteName,
					},
				})
			}
		}

		// APP
		if r.Apps != nil {
			for _, app := range r.Apps.Data {
				_ = encoder.Encode(AssetLine{
					Type:    "app",
					Value:   app.Name,
					Company: company,
					Source:  "app",
					Extra: map[string]string{
						"bundle_id": app.BundleID,
						"platform":  app.Platform,
					},
				})
			}
		}

		// 公众号
		if r.OfficialAccounts != nil {
			for _, acc := range r.OfficialAccounts.Data {
				_ = encoder.Encode(AssetLine{
					Type:    "wechat",
					Value:   acc.Name,
					Company: company,
					Source:  "wechat",
					Extra: map[string]string{
						"wechat_id": acc.WechatID,
					},
				})
			}
		}

		// 小程序
		if r.MiniPrograms != nil {
			for _, mp := range r.MiniPrograms.Data {
				_ = encoder.Encode(AssetLine{
					Type:    "miniprogram",
					Value:   mp.Name,
					Company: company,
					Source:  mp.Platform,
					Extra: map[string]string{
						"app_id": mp.AppID,
					},
				})
			}
		}
	}

	return nil
}
