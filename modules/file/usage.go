package file

import (
	"context"
	"fmt"
	"math"
	"time"

	"nexadmin/core/cache"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// usedBytes 返回租户已存储的字节数（仅本地/对象存储里的文件，外链不计）。
// 汇总结果由 core/tenant.StorageMeter 缓存约 30 秒，上传完成/删除时增量修正，热路径不会每次 SUM 全表。
func (s *Service) usedBytes(ctx context.Context, tid idgen.ID) (int64, error) {
	return tenant.StorageMeter.Get(tid, func() (int64, error) {
		var n int64
		err := s.db.WithContext(ctx).Model(&File{}).Select("COALESCE(SUM(size), 0)").Where("storage_path <> ''").Scan(&n).Error
		return n, err
	})
}

// ensureStorage 在新增 add 字节前检查套餐存储额度（0 = 不限）；没有额度限制时不做任何查询。
// 此函数仅供用量检查；上传必须调用 reserveStorage 原子预占。
func (s *Service) ensureStorage(ctx context.Context, add int64) error {
	if add < 0 {
		return fmt.Errorf("file: negative storage bytes")
	}
	info, err := tenant.QuotaInfo(ctx)
	if err != nil || info == nil || info.MaxStorageMB == 0 {
		return err
	}
	if info.MaxStorageMB < 0 || info.MaxStorageMB > math.MaxInt64/(1024*1024) {
		return fmt.Errorf("file: invalid storage quota")
	}
	tid, _ := tenant.From(ctx)
	used, err := s.usedBytes(ctx, tid)
	if err != nil {
		return err
	}
	return tenant.CheckStorageQuota(ctx, used, add)
}

func reservationKey(tid idgen.ID) string {
	return cache.GlobalKey("t"+tid.String(), "upload", "reservations")
}

// reserveStorage 的数据库读取发生在原子更新内：Redis WATCH 冲突会重新读取。
// 入库后尚未释放的 ID 不计入实际用量，其空间仍由预占计入，避免转换窗口重复计算。
func (s *Service) reserveStorage(ctx context.Context, upload *session) error {
	if upload.Length <= 0 {
		return fmt.Errorf("file: invalid upload length")
	}
	tid, ok := tenant.From(ctx)
	if !ok || tid.String() != upload.TenantID || tenant.Skipped(ctx) {
		return fmt.Errorf("file: invalid reservation tenant")
	}
	info, err := tenant.QuotaInfo(ctx)
	if err != nil {
		return err
	}
	if info == nil || info.MaxStorageMB == 0 {
		return nil
	}
	if info.MaxStorageMB < 0 || info.MaxStorageMB > math.MaxInt64/(1024*1024) {
		return fmt.Errorf("file: invalid storage quota")
	}
	leases, ok := cache.As[cache.Lease](s.cache)
	if !ok {
		return response.Unavailable("file.cache_lease_unsupported")
	}
	until := upload.CreatedAt.Add(sessionMaxAge)
	if !time.Now().Before(until) {
		return fmt.Errorf("file: upload expired")
	}
	err = leases.UpdateLeases(ctx, reservationKey(tid), sessionMaxAge, func(entries map[string]cache.LeaseEntry) error {
		var reserved int64
		ids := make([]string, 0, len(entries))
		for id, entry := range entries {
			if entry.Bytes < 0 || reserved > math.MaxInt64-entry.Bytes {
				return fmt.Errorf("file: reservation overflow")
			}
			reserved += entry.Bytes
			ids = append(ids, id)
		}
		var used int64
		q := s.db.WithContext(ctx).Model(&File{}).Where("tenant_id = ? AND storage_path <> ''", tid)
		if len(ids) > 0 {
			q = q.Where("id NOT IN ?", ids)
		}
		if err := q.Select("COALESCE(SUM(size), 0)").Scan(&used).Error; err != nil {
			return err
		}
		add := upload.Length
		if entry, exists := entries[upload.ID]; exists {
			if entry.Bytes != upload.Length {
				return fmt.Errorf("file: reservation length mismatch")
			}
			add = 0
		}
		if used < 0 || reserved > math.MaxInt64-used {
			return fmt.Errorf("file: storage overflow")
		}
		if err := tenant.CheckStorageQuota(ctx, used+reserved, add); err != nil {
			return err
		}
		entries[upload.ID] = cache.LeaseEntry{Bytes: upload.Length, Until: until}
		return nil
	})
	if err == nil {
		upload.Reserved = true
	}
	return err
}

func (s *Service) releaseStorage(upload *session) error {
	if !upload.Reserved {
		return nil
	}
	tid, err := idgen.Parse(upload.TenantID)
	if err != nil {
		return err
	}
	leases, ok := cache.As[cache.Lease](s.cache)
	if !ok {
		return response.Unavailable("file.cache_lease_release_unsupported")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = leases.UpdateLeases(ctx, reservationKey(tid), sessionMaxAge, func(entries map[string]cache.LeaseEntry) error {
		delete(entries, upload.ID)
		return nil
	})
	if err == nil {
		upload.Reserved = false
	}
	return err
}
