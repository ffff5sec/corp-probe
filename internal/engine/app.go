package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/ffff5sec/corp-probe/internal/cache"
	"github.com/ffff5sec/corp-probe/internal/model"
	"github.com/ffff5sec/corp-probe/internal/source"
)

// AppEngine APP / 小程序 / 公众号发现引擎。
// 聚合多个数据源的查询结果并去重。
type AppEngine struct {
	sources   []source.Source
	cache     cache.Cache
	cacheMode CacheMode
}

// NewAppEngine 创建 APP 发现引擎。
func NewAppEngine(sources []source.Source, c cache.Cache, mode CacheMode) *AppEngine {
	return &AppEngine{sources: sources, cache: c, cacheMode: mode}
}

// QueryApps 查询企业名下的移动应用。
// 按 (BundleID, Platform) 去重。
func (e *AppEngine) QueryApps(ctx context.Context, companyName string) (*model.ResultWrapper[model.AppInfo], error) {
	wrapper := &model.ResultWrapper[model.AppInfo]{}
	var lastErr error

	for _, src := range e.sources {
		key := cache.CacheKey{
			CompanyName: companyName,
			QueryType:   cache.QueryTypeApp,
			Source:      src.Name(),
		}

		apps, meta, err := cacheAware(ctx, e.cache, key, e.cacheMode, func() ([]model.AppInfo, error) {
			return src.QueryApps(ctx, companyName)
		})
		if err != nil {
			if errors.Is(err, source.ErrNotSupported) {
				continue
			}
			lastErr = err
			continue
		}

		for _, a := range apps {
			wrapper.Data = append(wrapper.Data, a)
			wrapper.Meta = append(wrapper.Meta, *meta)
		}
	}

	// 按 (BundleID, Platform) 去重
	dedupApps(wrapper)

	if len(wrapper.Data) == 0 && lastErr != nil {
		return nil, fmt.Errorf("APP 查询失败: %w", lastErr)
	}
	return wrapper, nil
}

// QueryMiniPrograms 查询企业名下的小程序。
// 按 (Name, Platform) 去重。
func (e *AppEngine) QueryMiniPrograms(ctx context.Context, companyName string) (*model.ResultWrapper[model.MiniProgram], error) {
	wrapper := &model.ResultWrapper[model.MiniProgram]{}
	var lastErr error

	for _, src := range e.sources {
		key := cache.CacheKey{
			CompanyName: companyName,
			QueryType:   cache.QueryTypeMiniProgram,
			Source:      src.Name(),
		}

		programs, meta, err := cacheAware(ctx, e.cache, key, e.cacheMode, func() ([]model.MiniProgram, error) {
			return src.QueryMiniPrograms(ctx, companyName)
		})
		if err != nil {
			if errors.Is(err, source.ErrNotSupported) {
				continue
			}
			lastErr = err
			continue
		}

		for _, p := range programs {
			wrapper.Data = append(wrapper.Data, p)
			wrapper.Meta = append(wrapper.Meta, *meta)
		}
	}

	dedupMiniPrograms(wrapper)

	if len(wrapper.Data) == 0 && lastErr != nil {
		return nil, fmt.Errorf("小程序查询失败: %w", lastErr)
	}
	return wrapper, nil
}

// QueryOfficialAccounts 查询企业名下的公众号。
// 按 WechatID 去重。
func (e *AppEngine) QueryOfficialAccounts(ctx context.Context, companyName string) (*model.ResultWrapper[model.OfficialAccount], error) {
	wrapper := &model.ResultWrapper[model.OfficialAccount]{}
	var lastErr error

	for _, src := range e.sources {
		key := cache.CacheKey{
			CompanyName: companyName,
			QueryType:   cache.QueryTypeOfficialAccount,
			Source:      src.Name(),
		}

		accounts, meta, err := cacheAware(ctx, e.cache, key, e.cacheMode, func() ([]model.OfficialAccount, error) {
			return src.QueryOfficialAccounts(ctx, companyName)
		})
		if err != nil {
			if errors.Is(err, source.ErrNotSupported) {
				continue
			}
			lastErr = err
			continue
		}

		for _, a := range accounts {
			wrapper.Data = append(wrapper.Data, a)
			wrapper.Meta = append(wrapper.Meta, *meta)
		}
	}

	dedupOfficialAccounts(wrapper)

	if len(wrapper.Data) == 0 && lastErr != nil {
		return nil, fmt.Errorf("公众号查询失败: %w", lastErr)
	}
	return wrapper, nil
}

// ────────────────────────────────────────
// 去重函数
// ────────────────────────────────────────

func dedupApps(w *model.ResultWrapper[model.AppInfo]) {
	type key struct{ bundleID, platform string }
	seen := make(map[key]bool)
	var data []model.AppInfo
	var meta []model.Meta
	for i, a := range w.Data {
		k := key{a.BundleID, a.Platform}
		if seen[k] {
			continue
		}
		seen[k] = true
		data = append(data, a)
		meta = append(meta, w.Meta[i])
	}
	w.Data = data
	w.Meta = meta
}

func dedupMiniPrograms(w *model.ResultWrapper[model.MiniProgram]) {
	type key struct{ name, platform string }
	seen := make(map[key]bool)
	var data []model.MiniProgram
	var meta []model.Meta
	for i, p := range w.Data {
		k := key{p.Name, p.Platform}
		if seen[k] {
			continue
		}
		seen[k] = true
		data = append(data, p)
		meta = append(meta, w.Meta[i])
	}
	w.Data = data
	w.Meta = meta
}

func dedupOfficialAccounts(w *model.ResultWrapper[model.OfficialAccount]) {
	seen := make(map[string]bool)
	var data []model.OfficialAccount
	var meta []model.Meta
	for i, a := range w.Data {
		if seen[a.WechatID] {
			continue
		}
		seen[a.WechatID] = true
		data = append(data, a)
		meta = append(meta, w.Meta[i])
	}
	w.Data = data
	w.Meta = meta
}
