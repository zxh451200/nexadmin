package core

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/database"
	"nexadmin/core/storage"
)

const (
	healthCacheProbeTTL = 10 * time.Second
	healthStorageKey    = "healthcheck/probe"
	healthStorageS3Wait = 1 * time.Second // S3 只做一次 HEAD，超时比总超时更短
)

// dbHealthProbe 探数据库连通性。
func dbHealthProbe(db *gorm.DB) *healthProbe {
	return &healthProbe{name: "db", fn: func(ctx context.Context) error {
		sqlDB, err := database.Platform(db).DB()
		if err != nil {
			return err
		}
		if err := sqlDB.PingContext(ctx); err != nil {
			return err
		}
		for _, td := range database.TenantDatabases(db) {
			pool, err := td.DB()
			if err != nil {
				return err
			}
			if err := pool.PingContext(ctx); err != nil {
				return err
			}
		}
		return nil
	}}
}

// cacheHealthProbe 读写一个带 TTL 的探针 key，并核对读回的值。
func cacheHealthProbe(c cache.Cache) *healthProbe {
	return &healthProbe{name: "cache", fn: func(ctx context.Context) error {
		key := cache.GlobalKey("healthz", "probe")
		val := strconv.FormatInt(time.Now().UnixNano(), 36)
		if err := c.Set(ctx, key, val, healthCacheProbeTTL); err != nil {
			return err
		}
		got, err := c.Get(ctx, key)
		if err != nil {
			return err
		}
		if got != val {
			return errors.New("cache probe value mismatch")
		}
		return nil
	}}
}

// storageHealthProbe：本地存储写入并删除一个探针对象（目录可写）；S3 只做一次 HEAD（对象不存在也算连通）。
func storageHealthProbe(cfg *config.Config) *healthProbe {
	var (
		mu sync.Mutex
		st storage.Storage
	)
	get := func() (storage.Storage, error) {
		mu.Lock()
		defer mu.Unlock()
		if st != nil {
			return st, nil
		}
		s, err := storage.New(cfg.Storage, filepath.Join(cfg.DataDir, "uploads", "files"))
		if err != nil {
			return nil, err
		}
		st = s
		return st, nil
	}
	return &healthProbe{name: "storage", fn: func(ctx context.Context) error {
		s, err := get()
		if err != nil {
			return err
		}
		if strings.EqualFold(cfg.Storage.Driver, "s3") {
			c, cancel := context.WithTimeout(ctx, healthStorageS3Wait)
			defer cancel()
			_, err = s.Exists(c, healthStorageKey)
			return err
		}
		body := strings.NewReader("ok")
		if err := s.Put(ctx, healthStorageKey, body, int64(body.Len()), storage.PutOptions{}); err != nil {
			return err
		}
		return s.Delete(ctx, healthStorageKey)
	}}
}
