// Package cache 提供查询结果的本地缓存机制。
// 网络查询成功时自动写入缓存；网络失败时降级读取缓存数据，并标注数据更新时间。
package cache

import (
	"context"
	"time"
)

// 查询类型常量，用作缓存键的一部分。
const (
	QueryTypeICP             = "icp"
	QueryTypeEquity          = "equity"
	QueryTypeApp             = "app"
	QueryTypeMiniProgram     = "miniprogram"
	QueryTypeOfficialAccount = "official_account"
)

// CacheKey 缓存键，由企业名称、查询类型和数据源三元组唯一确定。
type CacheKey struct {
	CompanyName string // 企业名称
	QueryType   string // 查询类型（见 QueryType* 常量）
	Source      string // 数据源标识，如 "aiqicha"
}

// CacheEntry 缓存条目。
type CacheEntry struct {
	Key       CacheKey  // 缓存键
	Data      []byte    // JSON 序列化的结果数据
	UpdatedAt time.Time // 数据写入/更新时间
	FromCache bool      // 运行时标记（读取时设置，不持久化）
}

// CacheStats 缓存统计信息。
type CacheStats struct {
	TotalEntries    int64     // 总条目数
	UniqueCompanies int64    // 不重复企业数
	OldestEntry     time.Time // 最早的缓存条目时间
	DBSizeBytes     int64     // 数据库文件大小（字节）
}

// Cache 缓存接口，支持多种后端实现。
type Cache interface {
	// Put 写入或更新缓存条目（相同 Key 会覆盖）。
	Put(ctx context.Context, entry CacheEntry) error

	// Get 按 Key 精确查询缓存条目。未命中时返回 nil, nil。
	Get(ctx context.Context, key CacheKey) (*CacheEntry, error)

	// GetAll 查询某企业的所有缓存条目。
	GetAll(ctx context.Context, companyName string) ([]CacheEntry, error)

	// Purge 清除早于 olderThan 的缓存条目，返回删除行数。
	Purge(ctx context.Context, olderThan time.Duration) (int64, error)

	// Stats 返回缓存统计信息。
	Stats(ctx context.Context) (*CacheStats, error)

	// Close 关闭缓存连接。
	Close() error
}
