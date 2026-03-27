// Package engine 实现 corp-probe 的核心业务引擎。
// 编排多数据源查询、缓存中间件、结果去重与合并。
package engine

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ffff5sec/corp-probe/internal/cache"
	"github.com/ffff5sec/corp-probe/internal/model"
)

// CacheMode 缓存策略。
type CacheMode int

const (
	CacheModeNormal    CacheMode = iota // 网络优先，失败时降级读缓存
	CacheModeCacheOnly                  // 仅从缓存读取，不发起网络请求
	CacheModeNoCache                    // 仅网络请求，不读写缓存
)

// cacheAware 缓存感知的通用查询包装器。
// 根据 CacheMode 决定是否使用缓存，并自动处理缓存读写和降级逻辑。
func cacheAware[T any](
	ctx context.Context,
	c cache.Cache,
	key cache.CacheKey,
	mode CacheMode,
	fetch func() ([]T, error),
) ([]T, *model.Meta, error) {

	// 仅缓存模式：直接读缓存
	if mode == CacheModeCacheOnly {
		return readFromCache[T](ctx, c, key)
	}

	// 尝试网络查询
	results, err := fetch()

	if err == nil && c != nil && mode == CacheModeNormal {
		// 网络成功：写入缓存
		writeToCache(ctx, c, key, results)
		return results, &model.Meta{
			Source:    key.Source,
			FromCache: false,
			UpdatedAt: time.Now(),
		}, nil
	}

	if err == nil {
		// NoCache 模式或无缓存实例：直接返回
		return results, &model.Meta{
			Source:    key.Source,
			FromCache: false,
			UpdatedAt: time.Now(),
		}, nil
	}

	// 网络失败：尝试缓存降级（仅 Normal 模式）
	if c != nil && mode == CacheModeNormal {
		cached, meta, cacheErr := readFromCache[T](ctx, c, key)
		if cacheErr == nil && len(cached) > 0 {
			return cached, meta, nil
		}
	}

	// 缓存也未命中，返回原始网络错误
	return nil, nil, err
}

// readFromCache 从缓存读取数据并反序列化。
func readFromCache[T any](ctx context.Context, c cache.Cache, key cache.CacheKey) ([]T, *model.Meta, error) {
	if c == nil {
		return nil, nil, nil
	}

	entry, err := c.Get(ctx, key)
	if err != nil || entry == nil {
		return nil, nil, err
	}

	var results []T
	if err := json.Unmarshal(entry.Data, &results); err != nil {
		return nil, nil, err
	}

	return results, &model.Meta{
		Source:    key.Source,
		FromCache: true,
		UpdatedAt: entry.UpdatedAt,
	}, nil
}

// writeToCache 将数据序列化后写入缓存。
func writeToCache[T any](ctx context.Context, c cache.Cache, key cache.CacheKey, data []T) {
	if c == nil {
		return
	}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return // 序列化失败不影响主流程
	}
	_ = c.Put(ctx, cache.CacheEntry{
		Key:       key,
		Data:      jsonData,
		UpdatedAt: time.Now(),
	})
}
