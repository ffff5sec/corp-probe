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
