// Package config 提供 corp-probe 的配置加载与管理。
// 优先级：CLI参数 > 环境变量 > 配置文件 > 默认值。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
)

// ────────────────────────────────────────
// 配置结构体
// ────────────────────────────────────────

// Config 顶层配置。
type Config struct {
	Sources SourcesConfig `mapstructure:"sources"`
	Equity  EquityConfig  `mapstructure:"equity"`
	Cache   CacheConfig   `mapstructure:"cache"`
	Runtime RuntimeConfig `mapstructure:"runtime"`
}

// SourcesConfig 各数据源的认证信息。
type SourcesConfig struct {
	AiQiCha  SourceAuth  `mapstructure:"aiqicha"`
	YiQiCha  YiQiChaAuth `mapstructure:"yiqicha"`
	QiChaCha SourceAuth  `mapstructure:"qichacha"`
	GSXT     SourceAuth  `mapstructure:"gsxt"`
	MIIT     MIITAuth    `mapstructure:"miit"`
	QiMai    QiMaiAuth   `mapstructure:"qimai"`
	CoolAPK  SourceAuth  `mapstructure:"coolapk"`
}

// YiQiChaAuth 亿企查认证配置（JWT Token + 客户端ID）。
type YiQiChaAuth struct {
	Token string `mapstructure:"token"` // JWT 令牌
	CID   string `mapstructure:"cid"`   // 客户端标识
}

// SourceAuth 基于 Cookie 认证的数据源。
type SourceAuth struct {
	Cookie string `mapstructure:"cookie"`
}

// MIITAuth 工信部接口配置。
type MIITAuth struct {
	Proxy string `mapstructure:"proxy"`
}

// QiMaiAuth 七麦接口配置。
type QiMaiAuth struct {
	APIKey string `mapstructure:"api_key"`
}

// EquityConfig 股权穿透默认参数。
type EquityConfig struct {
	DefaultDepth  int  `mapstructure:"default_depth"`  // 穿透层数
	DefaultRatio  int  `mapstructure:"default_ratio"`  // 最低控股比例
	IncludeBranch bool `mapstructure:"include_branch"` // 是否包含分支机构
}

// CacheConfig 本地缓存配置。
type CacheConfig struct {
	Enabled bool          `mapstructure:"enabled"` // 是否启用缓存
	DBPath  string        `mapstructure:"db_path"` // SQLite 数据库路径
	TTL     time.Duration `mapstructure:"ttl"`     // 缓存过期时间
}

// RuntimeConfig 运行时参数。
type RuntimeConfig struct {
	Concurrency int           `mapstructure:"concurrency"` // 并发数
	Delay       time.Duration `mapstructure:"delay"`       // 请求间隔
	Timeout     time.Duration `mapstructure:"timeout"`     // 单次请求超时
	Retry       int           `mapstructure:"retry"`       // 重试次数
	Proxy       string        `mapstructure:"proxy"`       // 全局代理
	OutputDir   string        `mapstructure:"output_dir"`  // 输出目录
}

// ────────────────────────────────────────
// 默认值
// ────────────────────────────────────────

// SetDefaults 注册所有配置项的默认值。
func SetDefaults() {
	// 股权穿透
	viper.SetDefault("equity.default_depth", 2)
	viper.SetDefault("equity.default_ratio", 51)
	viper.SetDefault("equity.include_branch", false)

	// 缓存
	viper.SetDefault("cache.enabled", true)
	viper.SetDefault("cache.db_path", "./data/cache.db")
	viper.SetDefault("cache.ttl", "720h") // 30天

	// 运行时
	viper.SetDefault("runtime.concurrency", 5)
	viper.SetDefault("runtime.delay", "1s")
	viper.SetDefault("runtime.timeout", "30s")
	viper.SetDefault("runtime.retry", 3)
	viper.SetDefault("runtime.proxy", "")
	viper.SetDefault("runtime.output_dir", "./results")
}

// ────────────────────────────────────────
// 环境变量绑定
// ────────────────────────────────────────

// BindEnvVars 将 CORP_PROBE_* 环境变量映射到配置键。
func BindEnvVars() {
	viper.SetEnvPrefix("CORP_PROBE")

	// 数据源凭证
	_ = viper.BindEnv("sources.aiqicha.cookie", "CORP_PROBE_AIQICHA_COOKIE")
	_ = viper.BindEnv("sources.yiqicha.token", "CORP_PROBE_YIQICHA_TOKEN")
	_ = viper.BindEnv("sources.yiqicha.cid", "CORP_PROBE_YIQICHA_CID")
	_ = viper.BindEnv("sources.qichacha.cookie", "CORP_PROBE_QICHACHA_COOKIE")
	_ = viper.BindEnv("sources.gsxt.cookie", "CORP_PROBE_GSXT_COOKIE")
	_ = viper.BindEnv("sources.miit.proxy", "CORP_PROBE_MIIT_PROXY")
	_ = viper.BindEnv("sources.qimai.api_key", "CORP_PROBE_QIMAI_API_KEY")
	_ = viper.BindEnv("sources.coolapk.cookie", "CORP_PROBE_COOLAPK_COOKIE")

	// 运行时
	_ = viper.BindEnv("runtime.proxy", "CORP_PROBE_PROXY")
}

// ────────────────────────────────────────
// 加载
// ────────────────────────────────────────

// DefaultConfigTemplate 默认配置文件模板。
const DefaultConfigTemplate = `# corp-probe 配置文件
# 至少配置一个数据源的认证信息才能使用

sources:
  # 亿企查（推荐）— 登录 www.yiqicha.com 后从浏览器 DevTools 获取 token 和 cid
  yiqicha:
    token: ""   # JWT 令牌（从请求头 token 字段获取）
    cid: ""     # 客户端标识（从请求头 cid 字段获取）

  # 爱企查 — 登录 aiqicha.baidu.com 后从浏览器获取 Cookie
  # aiqicha:
  #   cookie: ""

equity:
  default_depth: 2      # 股权穿透层数
  default_ratio: 51     # 最低控股比例
  include_branch: false  # 是否包含分支机构

cache:
  enabled: true
  db_path: "./data/cache.db"
  ttl: "720h"            # 缓存有效期（30天）

runtime:
  concurrency: 5         # 并发数
  delay: "1s"            # 请求间隔
  timeout: "30s"         # 单次请求超时
  retry: 3               # 重试次数
  proxy: ""              # 代理地址（如 http://127.0.0.1:7890）
  output_dir: "./results"
`

// GenerateDefaultConfig 在指定路径生成默认配置文件。
// 如果文件已存在则不覆盖，返回 false。
func GenerateDefaultConfig(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil // 文件已存在
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Errorf("创建配置目录失败: %w", err)
	}

	if err := os.WriteFile(path, []byte(DefaultConfigTemplate), 0o644); err != nil {
		return false, fmt.Errorf("写入配置文件失败: %w", err)
	}
	return true, nil
}

// ConfigFileUsed 返回当前使用的配置文件路径。
func ConfigFileUsed() string {
	return viper.ConfigFileUsed()
}

// Load 从配置文件、环境变量加载并合并配置。
// 调用前可通过 viper.Set() 注入 CLI 参数覆盖。
func Load() (*Config, error) {
	SetDefaults()
	BindEnvVars()

	// 配置文件搜索路径
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")                                           // 当前目录
	viper.AddConfigPath(filepath.Join(homeDir(), ".corp-probe"))       // 用户主目录

	// 读取配置文件（不存在时忽略）
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("读取配置文件失败: %w", err)
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}

	return &cfg, nil
}

// homeDir 返回用户主目录，获取失败时返回当前目录。
func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return "."
}
