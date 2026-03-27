package engine

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ffff5sec/corp-probe/internal/model"
	"github.com/ffff5sec/corp-probe/internal/source"
)

// sourceAccessor 允许 runner 访问引擎内部的数据源（用于搜索企业信息）。
func init() {
	_ = source.ErrNotSupported // 确保 source 包被导入
}

// Task 单个查询任务。
type Task struct {
	CompanyName string   // 企业名称
	Modules     []string // 要执行的模块：icp / equity / app（空 = 全部）
}

// TaskResult 任务执行结果。
type TaskResult struct {
	Task   Task
	Result *model.QueryResult
	Err    error
}

// ProgressFunc 进度回调函数。
type ProgressFunc func(completed, total int, current string)

// Runner 批量任务执行器。
// 使用信号量控制并发，支持请求间隔和进度回调。
type Runner struct {
	icpEngine    *ICPEngine
	equityEngine *EquityEngine
	appEngine    *AppEngine
	concurrency  int
	delay        time.Duration
	progress     ProgressFunc
}

// NewRunner 创建批量任务执行器。
func NewRunner(icp *ICPEngine, equity *EquityEngine, app *AppEngine, concurrency int, delay time.Duration) *Runner {
	return &Runner{
		icpEngine:    icp,
		equityEngine: equity,
		appEngine:    app,
		concurrency:  concurrency,
		delay:        delay,
	}
}

// SetProgress 设置进度回调。
func (r *Runner) SetProgress(fn ProgressFunc) {
	r.progress = fn
}

// Run 执行批量任务，返回与输入顺序一致的结果。
func (r *Runner) Run(ctx context.Context, tasks []Task) []TaskResult {
	results := make([]TaskResult, len(tasks))
	sem := make(chan struct{}, r.concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	completed := 0

	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, t Task) {
			defer wg.Done()

			sem <- struct{}{}        // 获取信号量
			defer func() { <-sem }() // 释放信号量

			result, err := r.executeTask(ctx, t)
			results[idx] = TaskResult{Task: t, Result: result, Err: err}

			mu.Lock()
			completed++
			if r.progress != nil {
				r.progress(completed, len(tasks), t.CompanyName)
			}
			mu.Unlock()

			// 请求间隔
			if r.delay > 0 {
				time.Sleep(r.delay)
			}
		}(i, task)
	}

	wg.Wait()
	return results
}

// RunFromFile 从文件读取企业名称（每行一个），批量执行。
func (r *Runner) RunFromFile(ctx context.Context, filePath string, modules []string) ([]TaskResult, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("打开文件失败: %w", err)
	}
	defer f.Close()

	var tasks []Task
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if name == "" || strings.HasPrefix(name, "#") {
			continue // 跳过空行和注释
		}
		tasks = append(tasks, Task{CompanyName: name, Modules: modules})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	if len(tasks) == 0 {
		return nil, fmt.Errorf("文件中未找到有效的企业名称")
	}

	return r.Run(ctx, tasks), nil
}

// executeTask 执行单个查询任务。
func (r *Runner) executeTask(ctx context.Context, task Task) (*model.QueryResult, error) {
	result := &model.QueryResult{}
	modules := task.Modules
	runAll := len(modules) == 0
	var errors []string

	// 判断是否需要执行某模块
	shouldRun := func(name string) bool {
		if runAll {
			return true
		}
		for _, m := range modules {
			if m == name {
				return true
			}
		}
		return false
	}

	// 先搜索企业获取基本信息
	if r.icpEngine != nil && len(r.icpEngine.Sources()) > 0 {
		for _, src := range r.icpEngine.Sources() {
			companies, err := src.SearchCompany(ctx, task.CompanyName)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[%s] 搜索企业失败: %v\n", src.Name(), err)
				errors = append(errors, err.Error())
				continue
			}
			if len(companies) > 0 {
				result.Company = companies[0]
				break
			}
		}
	}

	// ICP 查询
	if shouldRun("icp") && r.icpEngine != nil {
		icpResult, err := r.icpEngine.Query(ctx, task.CompanyName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[icp] 查询失败: %v\n", err)
			errors = append(errors, err.Error())
		} else {
			result.ICPRecords = icpResult
		}
	}

	// 股权穿透
	if shouldRun("equity") && r.equityEngine != nil {
		tree, icpRecords, err := r.equityEngine.Traverse(ctx, task.CompanyName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[equity] 查询失败: %v\n", err)
			errors = append(errors, err.Error())
		} else {
			result.EquityTree = tree
			if icpRecords != nil && len(icpRecords.Data) > 0 {
				if result.ICPRecords == nil {
					result.ICPRecords = icpRecords
				} else {
					result.ICPRecords.Data = append(result.ICPRecords.Data, icpRecords.Data...)
					result.ICPRecords.Meta = append(result.ICPRecords.Meta, icpRecords.Meta...)
				}
			}
		}
	}

	// APP / 小程序 / 公众号
	if shouldRun("app") && r.appEngine != nil {
		if apps, err := r.appEngine.QueryApps(ctx, task.CompanyName); err != nil {
			fmt.Fprintf(os.Stderr, "[app] APP查询失败: %v\n", err)
		} else {
			result.Apps = apps
		}
		if programs, err := r.appEngine.QueryMiniPrograms(ctx, task.CompanyName); err != nil {
			fmt.Fprintf(os.Stderr, "[app] 小程序查询失败: %v\n", err)
		} else {
			result.MiniPrograms = programs
		}
		if accounts, err := r.appEngine.QueryOfficialAccounts(ctx, task.CompanyName); err != nil {
			fmt.Fprintf(os.Stderr, "[app] 公众号查询失败: %v\n", err)
		} else {
			result.OfficialAccounts = accounts
		}
	}

	// 所有模块都失败时返回错误
	if result.Company.CompanyID == "" && len(errors) > 0 {
		return result, fmt.Errorf("%s", strings.Join(errors, "; "))
	}

	return result, nil
}
