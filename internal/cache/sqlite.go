package cache

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动，无需 CGO
)

// sqliteCache 基于 SQLite 的缓存实现。
type sqliteCache struct {
	db *sql.DB
	mu sync.Mutex // 序列化写操作，避免 SQLite 并发写冲突
}

// NewSQLiteCache 创建并初始化 SQLite 缓存。
// 自动创建数据库文件所在目录和表结构。
func NewSQLiteCache(dbPath string) (Cache, error) {
	// 确保目录存在
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建缓存目录失败: %w", err)
	}

	// 使用 WAL 模式提升并发读性能，设置忙等待超时
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=5000", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开缓存数据库失败: %w", err)
	}

	c := &sqliteCache{db: db}
	if err := c.ensureSchema(); err != nil {
		db.Close()
		return nil, err
	}

	return c, nil
}

// ensureSchema 创建表和索引（如不存在）。
func (c *sqliteCache) ensureSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS cache (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		company    TEXT NOT NULL,
		query_type TEXT NOT NULL,
		source     TEXT NOT NULL,
		data       TEXT NOT NULL,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(company, query_type, source)
	);
	CREATE INDEX IF NOT EXISTS idx_cache_company ON cache(company);
	CREATE INDEX IF NOT EXISTS idx_cache_updated ON cache(updated_at);
	`
	_, err := c.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("初始化缓存表结构失败: %w", err)
	}
	return nil
}

// Put 写入或更新缓存条目。利用 UNIQUE 约束实现 UPSERT。
func (c *sqliteCache) Put(ctx context.Context, entry CacheEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	query := `
	INSERT INTO cache (company, query_type, source, data, updated_at)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(company, query_type, source)
	DO UPDATE SET data = excluded.data, updated_at = excluded.updated_at
	`
	_, err := c.db.ExecContext(ctx, query,
		entry.Key.CompanyName,
		entry.Key.QueryType,
		entry.Key.Source,
		string(entry.Data),
		entry.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("写入缓存失败: %w", err)
	}
	return nil
}

// Get 按 Key 精确查询缓存条目。未命中时返回 nil, nil。
func (c *sqliteCache) Get(ctx context.Context, key CacheKey) (*CacheEntry, error) {
	query := `SELECT data, updated_at FROM cache WHERE company = ? AND query_type = ? AND source = ?`
	row := c.db.QueryRowContext(ctx, query, key.CompanyName, key.QueryType, key.Source)

	var data string
	var updatedAt string
	if err := row.Scan(&data, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // 未命中
		}
		return nil, fmt.Errorf("查询缓存失败: %w", err)
	}

	t, _ := time.Parse(time.RFC3339, updatedAt)

	return &CacheEntry{
		Key:       key,
		Data:      []byte(data),
		UpdatedAt: t,
		FromCache: true,
	}, nil
}

// GetAll 查询某企业的所有缓存条目。
func (c *sqliteCache) GetAll(ctx context.Context, companyName string) ([]CacheEntry, error) {
	query := `SELECT company, query_type, source, data, updated_at FROM cache WHERE company = ?`
	rows, err := c.db.QueryContext(ctx, query, companyName)
	if err != nil {
		return nil, fmt.Errorf("查询缓存失败: %w", err)
	}
	defer rows.Close()

	var entries []CacheEntry
	for rows.Next() {
		var company, queryType, source, data, updatedAt string
		if err := rows.Scan(&company, &queryType, &source, &data, &updatedAt); err != nil {
			return nil, fmt.Errorf("扫描缓存行失败: %w", err)
		}
		t, _ := time.Parse(time.RFC3339, updatedAt)
		entries = append(entries, CacheEntry{
			Key:       CacheKey{CompanyName: company, QueryType: queryType, Source: source},
			Data:      []byte(data),
			UpdatedAt: t,
			FromCache: true,
		})
	}
	return entries, rows.Err()
}

// Purge 清除早于 olderThan 的缓存条目，返回删除行数。
func (c *sqliteCache) Purge(ctx context.Context, olderThan time.Duration) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := time.Now().Add(-olderThan).UTC().Format(time.RFC3339)
	result, err := c.db.ExecContext(ctx, `DELETE FROM cache WHERE updated_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("清理缓存失败: %w", err)
	}
	return result.RowsAffected()
}

// Stats 返回缓存统计信息。
func (c *sqliteCache) Stats(ctx context.Context) (*CacheStats, error) {
	stats := &CacheStats{}

	// 总条目数
	row := c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cache`)
	if err := row.Scan(&stats.TotalEntries); err != nil {
		return nil, fmt.Errorf("统计缓存条目失败: %w", err)
	}

	// 不重复企业数
	row = c.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT company) FROM cache`)
	if err := row.Scan(&stats.UniqueCompanies); err != nil {
		return nil, fmt.Errorf("统计企业数失败: %w", err)
	}

	// 最早条目时间
	var oldest sql.NullString
	row = c.db.QueryRowContext(ctx, `SELECT MIN(updated_at) FROM cache`)
	if err := row.Scan(&oldest); err != nil {
		return nil, fmt.Errorf("查询最早条目失败: %w", err)
	}
	if oldest.Valid {
		stats.OldestEntry, _ = time.Parse(time.RFC3339, oldest.String)
	}

	// 数据库文件大小（从 pragma 获取页数 × 页大小）
	var pageCount, pageSize int64
	row = c.db.QueryRowContext(ctx, `PRAGMA page_count`)
	_ = row.Scan(&pageCount)
	row = c.db.QueryRowContext(ctx, `PRAGMA page_size`)
	_ = row.Scan(&pageSize)
	stats.DBSizeBytes = pageCount * pageSize

	return stats, nil
}

// Close 关闭数据库连接。
func (c *sqliteCache) Close() error {
	return c.db.Close()
}
