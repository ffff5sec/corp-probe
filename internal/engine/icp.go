package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/ffff5sec/corp-probe/internal/cache"
	"github.com/ffff5sec/corp-probe/internal/model"
	"github.com/ffff5sec/corp-probe/internal/source"
)

// ICPEngine ICP 备案查询引擎。
// 聚合多个数据源的查询结果，自动去重合并，支持缓存降级。
type ICPEngine struct {
	sources   []source.Source
	cache     cache.Cache
	cacheMode CacheMode
}

// NewICPEngine 创建 ICP 查询引擎。
func NewICPEngine(sources []source.Source, c cache.Cache, mode CacheMode) *ICPEngine {
	return &ICPEngine{sources: sources, cache: c, cacheMode: mode}
}

// Sources 返回引擎使用的数据源列表。
func (e *ICPEngine) Sources() []source.Source {
	return e.sources
}

// Query 查询企业的 ICP 备案记录。
// 遍历所有数据源，合并结果并按 (域名, 备案号) 去重。
func (e *ICPEngine) Query(ctx context.Context, companyName string) (*model.ResultWrapper[model.ICPRecord], error) {
	wrapper := &model.ResultWrapper[model.ICPRecord]{}
	var lastErr error

	for _, src := range e.sources {
		key := cache.CacheKey{
			CompanyName: companyName,
			QueryType:   cache.QueryTypeICP,
			Source:      src.Name(),
		}

		records, meta, err := cacheAware(ctx, e.cache, key, e.cacheMode, func() ([]model.ICPRecord, error) {
			return src.QueryICP(ctx, companyName)
		})

		if err != nil {
			if errors.Is(err, source.ErrNotSupported) {
				continue // 该数据源不支持 ICP 查询，跳过
			}
			lastErr = err
			continue
		}

		// 追加结果和元数据
		for _, r := range records {
			wrapper.Data = append(wrapper.Data, r)
			wrapper.Meta = append(wrapper.Meta, *meta)
		}
	}

	// 去重：按 (域名, 备案号) 保留最新数据
	dedup(wrapper)

	if len(wrapper.Data) == 0 && lastErr != nil {
		return nil, fmt.Errorf("ICP 查询失败: %w", lastErr)
	}

	return wrapper, nil
}

// dedup 按 (域名, 备案号) 去重，重复时保留 UpdatedAt 最新的记录。
func dedup(wrapper *model.ResultWrapper[model.ICPRecord]) {
	type dedupKey struct{ domain, icp string }
	seen := make(map[dedupKey]int) // key → 在结果中的索引

	var data []model.ICPRecord
	var meta []model.Meta

	for i, r := range wrapper.Data {
		dk := dedupKey{domain: r.Domain, icp: r.ICPNumber}
		if idx, exists := seen[dk]; exists {
			// 保留更新时间更近的
			if wrapper.Meta[i].UpdatedAt.After(meta[idx].UpdatedAt) {
				data[idx] = r
				meta[idx] = wrapper.Meta[i]
			}
			continue
		}
		seen[dk] = len(data)
		data = append(data, r)
		meta = append(meta, wrapper.Meta[i])
	}

	wrapper.Data = data
	wrapper.Meta = meta
}
