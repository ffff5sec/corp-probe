// corp-probe 企业信息查询工具。
// 支持 ICP 备案查询、股权穿透、APP/小程序/公众号发现。
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/ffff5sec/corp-probe/internal/cache"
	"github.com/ffff5sec/corp-probe/internal/config"
	"github.com/ffff5sec/corp-probe/internal/engine"
	"github.com/ffff5sec/corp-probe/internal/model"
	"github.com/ffff5sec/corp-probe/internal/output"
	"github.com/ffff5sec/corp-probe/internal/source"
	"github.com/ffff5sec/corp-probe/internal/source/aiqicha"
	"github.com/ffff5sec/corp-probe/internal/source/yiqicha"
)

var version = "dev"

// ────────────────────────────────────────
// CLI 参数
// ────────────────────────────────────────

var (
	flagName       string
	flagFile       string
	flagModules    []string
	flagOutput     string
	flagSource     []string
	flagICPSource  string
	flagDepth      int
	flagRatio      int
	flagBranch     bool
	flagProxy      string
	flagConcurrency int
	flagDelay      time.Duration
	flagCacheOnly  bool
	flagNoCache    bool
	flagConfigFile string
)

// ────────────────────────────────────────
// 命令定义
// ────────────────────────────────────────

var rootCmd = &cobra.Command{
	Use:   "corp-probe",
	Short: "企业信息查询工具",
	Long:  "corp-probe - 企业信息查询工具：ICP 备案查询、股权穿透、APP/小程序/公众号发现",
	RunE:  runQuery,
}

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "缓存管理",
}

var cachePurgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "清理过期缓存",
	RunE:  runCachePurge,
}

var cacheStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "查看缓存统计",
	RunE:  runCacheStats,
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "生成默认配置文件",
	RunE:  runInit,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "显示版本信息",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("corp-probe %s\n", version)
	},
}

func init() {
	// 查询参数
	rootCmd.Flags().StringVarP(&flagName, "name", "n", "", "企业名称")
	rootCmd.Flags().StringVarP(&flagFile, "file", "f", "", "批量查询文件（每行一个企业名称）")
	rootCmd.Flags().StringSliceVarP(&flagModules, "module", "m", nil, "查询模块: icp, equity, app（默认全部）")
	rootCmd.Flags().StringVarP(&flagOutput, "output", "o", "table", "输出格式: table, json")
	rootCmd.Flags().StringSliceVar(&flagSource, "source", nil, "数据源: aiqicha, qichacha（默认全部已配置）")
	rootCmd.Flags().StringVar(&flagICPSource, "icp-source", "all", "ICP 数据源: miit, aiqicha, qichacha, all")

	// 股权穿透参数
	rootCmd.Flags().IntVar(&flagDepth, "depth", 0, "股权穿透层数（覆盖配置文件）")
	rootCmd.Flags().IntVar(&flagRatio, "ratio", 0, "最低控股比例（覆盖配置文件）")
	rootCmd.Flags().BoolVar(&flagBranch, "branch", false, "包含分支机构")

	// 运行参数
	rootCmd.Flags().StringVar(&flagProxy, "proxy", "", "代理地址")
	rootCmd.Flags().IntVar(&flagConcurrency, "concurrency", 0, "并发数（覆盖配置文件）")
	rootCmd.Flags().DurationVar(&flagDelay, "delay", 0, "请求间隔（覆盖配置文件）")

	// 缓存控制
	rootCmd.Flags().BoolVar(&flagCacheOnly, "cache-only", false, "仅使用缓存数据")
	rootCmd.Flags().BoolVar(&flagNoCache, "no-cache", false, "不使用缓存")

	// 配置文件
	rootCmd.PersistentFlags().StringVar(&flagConfigFile, "config", "", "配置文件路径")

	// 缓存清理参数
	var purgeBefore string
	cachePurgeCmd.Flags().StringVar(&purgeBefore, "before", "720h", "清理早于此时间的缓存（如 720h, 30d）")

	// 注册子命令
	cacheCmd.AddCommand(cachePurgeCmd, cacheStatsCmd)
	rootCmd.AddCommand(initCmd, cacheCmd, versionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// ────────────────────────────────────────
// 查询逻辑
// ────────────────────────────────────────

func runQuery(cmd *cobra.Command, args []string) error {
	if flagName == "" && flagFile == "" {
		return fmt.Errorf("请通过 -n 指定企业名称或 -f 指定批量文件")
	}

	// 加载配置
	if flagConfigFile != "" {
		viper.SetConfigFile(flagConfigFile)
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// CLI 参数覆盖配置
	applyFlags(cfg)

	// 初始化缓存
	var c cache.Cache
	if cfg.Cache.Enabled && !flagNoCache {
		c, err = cache.NewSQLiteCache(cfg.Cache.DBPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "警告: 初始化缓存失败: %v\n", err)
		} else {
			defer c.Close()
		}
	}

	// 确定缓存模式
	cacheMode := engine.CacheModeNormal
	if flagCacheOnly {
		cacheMode = engine.CacheModeCacheOnly
	} else if flagNoCache {
		cacheMode = engine.CacheModeNoCache
	}

	// 构建数据源
	sources := buildSources(cfg)
	if len(sources) == 0 && !flagCacheOnly {
		return fmt.Errorf("没有可用的数据源，请在 config.yaml 中配置认证信息（运行 corp-probe init 生成默认配置）")
	}

	// 构建引擎
	icpEngine := engine.NewICPEngine(sources, c, cacheMode)
	equityEngine := engine.NewEquityEngine(
		sources, c, cacheMode,
		cfg.Equity.DefaultDepth, float64(cfg.Equity.DefaultRatio),
		cfg.Equity.IncludeBranch, icpEngine,
	)
	appEngine := engine.NewAppEngine(sources, c, cacheMode)

	ctx := context.Background()

	// 批量查询
	if flagFile != "" {
		runner := engine.NewRunner(icpEngine, equityEngine, appEngine, cfg.Runtime.Concurrency, cfg.Runtime.Delay)
		runner.SetProgress(func(completed, total int, current string) {
			fmt.Fprintf(os.Stderr, "[%d/%d] %s\n", completed, total, current)
		})

		results, err := runner.RunFromFile(ctx, flagFile, flagModules)
		if err != nil {
			return err
		}

		return outputResults(cfg, results)
	}

	// 单企业查询
	task := engine.Task{CompanyName: flagName, Modules: flagModules}
	runner := engine.NewRunner(icpEngine, equityEngine, appEngine, cfg.Runtime.Concurrency, cfg.Runtime.Delay)
	taskResults := runner.Run(ctx, []engine.Task{task})

	return outputResults(cfg, taskResults)
}

// ────────────────────────────────────────
// 辅助函数
// ────────────────────────────────────────

// applyFlags 将 CLI 参数覆盖到配置中。
func applyFlags(cfg *config.Config) {
	if flagDepth > 0 {
		cfg.Equity.DefaultDepth = flagDepth
	}
	if flagRatio > 0 {
		cfg.Equity.DefaultRatio = flagRatio
	}
	if flagBranch {
		cfg.Equity.IncludeBranch = true
	}
	if flagProxy != "" {
		cfg.Runtime.Proxy = flagProxy
	}
	if flagConcurrency > 0 {
		cfg.Runtime.Concurrency = flagConcurrency
	}
	if flagDelay > 0 {
		cfg.Runtime.Delay = flagDelay
	}
}

// buildSources 根据配置构建可用的数据源列表。
// 只有配置了认证信息的数据源才会被启用。
func buildSources(cfg *config.Config) []source.Source {
	var sources []source.Source

	// 如果指定了 --source，只构建指定的数据源
	allowed := make(map[string]bool)
	if len(flagSource) > 0 {
		for _, s := range flagSource {
			allowed[s] = true
		}
	}

	shouldAdd := func(name string) bool {
		if len(allowed) == 0 {
			return true // 未指定则全部添加
		}
		return allowed[name]
	}

	// 爱企查
	if shouldAdd("aiqicha") && cfg.Sources.AiQiCha.Cookie != "" {
		sources = append(sources, aiqicha.New(
			cfg.Sources.AiQiCha.Cookie,
			cfg.Runtime.Proxy,
			cfg.Runtime.Timeout,
			cfg.Runtime.Delay,
			cfg.Runtime.Retry,
		))
	}

	// 亿企查
	if shouldAdd("yiqicha") && cfg.Sources.YiQiCha.Token != "" {
		sources = append(sources, yiqicha.New(
			cfg.Sources.YiQiCha.Token,
			cfg.Sources.YiQiCha.CID,
			cfg.Runtime.Proxy,
			cfg.Runtime.Timeout,
			cfg.Runtime.Delay,
			cfg.Runtime.Retry,
		))
	}

	return sources
}

// outputResults 格式化输出查询结果。
func outputResults(cfg *config.Config, taskResults []engine.TaskResult) error {
	var queryResults []model.QueryResult
	for _, tr := range taskResults {
		if tr.Err != nil {
			fmt.Fprintf(os.Stderr, "查询 %s 失败: %v\n", tr.Task.CompanyName, tr.Err)
			continue
		}
		if tr.Result != nil {
			queryResults = append(queryResults, *tr.Result)
		}
	}

	switch flagOutput {
	case "json":
		if flagFile != "" {
			// 批量查询输出到文件
			path, err := output.WriteJSONFile(cfg.Runtime.OutputDir, "batch", queryResults)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "结果已保存: %s\n", path)
		} else {
			return output.WriteJSON(os.Stdout, queryResults)
		}
	case "table":
		for _, r := range queryResults {
			output.WriteTable(os.Stdout, &r)
		}
	default:
		return fmt.Errorf("不支持的输出格式: %s", flagOutput)
	}

	return nil
}

// ────────────────────────────────────────
// 缓存管理命令
// ────────────────────────────────────────

func runCachePurge(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	c, err := cache.NewSQLiteCache(cfg.Cache.DBPath)
	if err != nil {
		return fmt.Errorf("打开缓存失败: %w", err)
	}
	defer c.Close()

	beforeStr, _ := cmd.Flags().GetString("before")
	before, err := time.ParseDuration(beforeStr)
	if err != nil {
		return fmt.Errorf("无效的时间格式: %s", beforeStr)
	}

	deleted, err := c.Purge(context.Background(), before)
	if err != nil {
		return err
	}

	fmt.Printf("已清理 %d 条过期缓存\n", deleted)
	return nil
}

func runCacheStats(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	c, err := cache.NewSQLiteCache(cfg.Cache.DBPath)
	if err != nil {
		return fmt.Errorf("打开缓存失败: %w", err)
	}
	defer c.Close()

	stats, err := c.Stats(context.Background())
	if err != nil {
		return err
	}

	fmt.Printf("缓存统计:\n")
	fmt.Printf("  总条目数:     %d\n", stats.TotalEntries)
	fmt.Printf("  企业数:       %d\n", stats.UniqueCompanies)
	if !stats.OldestEntry.IsZero() {
		fmt.Printf("  最早条目:     %s\n", stats.OldestEntry.Format("2006-01-02 15:04:05"))
	}
	fmt.Printf("  数据库大小:   %.2f KB\n", float64(stats.DBSizeBytes)/1024)
	return nil
}

// ────────────────────────────────────────
// 初始化命令
// ────────────────────────────────────────

func runInit(cmd *cobra.Command, args []string) error {
	path := "config.yaml"
	created, err := config.GenerateDefaultConfig(path)
	if err != nil {
		return err
	}
	if created {
		fmt.Printf("已生成默认配置文件: %s\n", path)
		fmt.Println("请编辑配置文件，填入数据源的认证信息后使用。")
		fmt.Println()
		fmt.Println("亿企查配置方法:")
		fmt.Println("  1. 浏览器登录 https://www.yiqicha.com")
		fmt.Println("  2. 按 F12 打开 DevTools -> Network")
		fmt.Println("  3. 搜索任意企业，找到 dropDownSearch 请求")
		fmt.Println("  4. 从 Request Headers 中复制 token 和 cid 的值")
		fmt.Println("  5. 填入 config.yaml 的 sources.yiqicha 下")
	} else {
		fmt.Printf("配置文件已存在: %s\n", path)
	}
	return nil
}
