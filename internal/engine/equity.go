package engine

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/ffff5sec/corp-probe/internal/cache"
	"github.com/ffff5sec/corp-probe/internal/model"
	"github.com/ffff5sec/corp-probe/internal/source"
)

// EquityEngine 股权穿透引擎。
// 递归发现子公司并自动触发 ICP 查询，构建股权关系树。
type EquityEngine struct {
	sources       []source.Source
	cache         cache.Cache
	cacheMode     CacheMode
	maxDepth      int        // 最大穿透层数
	minRatio      float64    // 最低控股比例
	includeBranch bool       // 是否包含分支机构
	icpEngine     *ICPEngine // 用于自动查询子公司 ICP
}

// NewEquityEngine 创建股权穿透引擎。
func NewEquityEngine(
	sources []source.Source,
	c cache.Cache,
	mode CacheMode,
	depth int,
	ratio float64,
	branch bool,
	icpEngine *ICPEngine,
) *EquityEngine {
	return &EquityEngine{
		sources:       sources,
		cache:         c,
		cacheMode:     mode,
		maxDepth:      depth,
		minRatio:      ratio,
		includeBranch: branch,
		icpEngine:     icpEngine,
	}
}

// Traverse 从目标企业开始递归穿透股权关系。
// 返回股权树和所有发现的子公司 ICP 记录。
func (e *EquityEngine) Traverse(ctx context.Context, companyName string) (*model.EquityTree, *model.ResultWrapper[model.ICPRecord], error) {
	// 先搜索目标企业获取基本信息和 CompanyID
	var rootCompany model.Company
	var companyID string

	for _, src := range e.sources {
		companies, err := src.SearchCompany(ctx, companyName)
		if err != nil || len(companies) == 0 {
			continue
		}
		rootCompany = companies[0]
		companyID = rootCompany.CompanyID
		break
	}

	if companyID == "" {
		return nil, nil, fmt.Errorf("未找到企业: %s", companyName)
	}

	// 收集所有子公司的 ICP 记录
	allICP := &model.ResultWrapper[model.ICPRecord]{}

	// visited 防止环路（A→B→A）
	visited := make(map[string]bool)

	// 递归穿透
	children := e.traverse(ctx, companyID, companyName, 1, visited, allICP)

	tree := &model.EquityTree{
		Root:     rootCompany,
		Children: children,
	}

	return tree, allICP, nil
}

// traverse 递归穿透股权关系。
func (e *EquityEngine) traverse(
	ctx context.Context,
	companyID string,
	companyName string,
	depth int,
	visited map[string]bool,
	allICP *model.ResultWrapper[model.ICPRecord],
) []model.EquityNode {

	// 超过最大深度或已访问：终止递归
	if depth > e.maxDepth || visited[companyID] {
		return nil
	}
	visited[companyID] = true

	// 查询控股企业
	investments, err := e.queryInvestments(ctx, companyID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[equity] %s 查询控股企业失败: %v\n", companyName, err)
		return nil
	}

	var nodes []model.EquityNode
	for _, inv := range investments {
		// 过滤控股比例
		if inv.Ratio < e.minRatio {
			continue
		}

		// 填充穿透层级信息
		inv.Depth = depth
		inv.ParentCompany = companyName

		// 自动触发子公司 ICP 查询（结果写入缓存）
		if e.icpEngine != nil {
			icpResult, err := e.icpEngine.Query(ctx, inv.CompanyName)
			if err == nil && icpResult != nil {
				allICP.Data = append(allICP.Data, icpResult.Data...)
				allICP.Meta = append(allICP.Meta, icpResult.Meta...)
			}
		}

		// 递归穿透子公司
		children := e.traverse(ctx, inv.CompanyID, inv.CompanyName, depth+1, visited, allICP)

		nodes = append(nodes, model.EquityNode{
			Investment: inv,
			Children:   children,
		})
	}

	return nodes
}

// queryInvestments 从多个数据源查询控股企业，取第一个成功的结果。
func (e *EquityEngine) queryInvestments(ctx context.Context, companyID string) ([]model.Investment, error) {
	for _, src := range e.sources {
		key := cache.CacheKey{
			CompanyName: companyID,
			QueryType:   cache.QueryTypeEquity,
			Source:      src.Name(),
		}

		investments, _, err := cacheAware(ctx, e.cache, key, e.cacheMode, func() ([]model.Investment, error) {
			return src.QueryInvestments(ctx, companyID)
		})

		if err != nil {
			if errors.Is(err, source.ErrNotSupported) {
				continue
			}
			// 配额耗尽等严重错误直接上报
			return nil, err
		}

		if len(investments) > 0 {
			return investments, nil
		}
	}
	return nil, nil
}
