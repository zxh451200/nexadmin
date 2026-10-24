package migrate

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrAborted 表示操作者没有确认，没有改动数据库。
var ErrAborted = errors.New("已取消，未改动数据库")

// Confirm 向操作者展示 prompt 并返回是否确认；默认应为“否”。
type Confirm func(prompt string) (bool, error)

// ParseRef 把 "<scope>/<文件名>" 拆开；scope 为 framework / business / plugin:<id>。
func ParseRef(ref string) (scope, name string, err error) {
	i := strings.LastIndex(ref, "/")
	if i <= 0 || i == len(ref)-1 {
		return "", "", fmt.Errorf("参数格式应为 <scope>/<文件名>（scope: framework | business | plugin:<id>），收到 %q", ref)
	}
	scope, name = ref[:i], ref[i+1:]
	validScope := scope == ScopeFramework || scope == ScopeBusiness ||
		(strings.HasPrefix(scope, ScopePrefix) && len(scope) > len(ScopePrefix))
	if !validScope {
		return "", "", fmt.Errorf("未知 scope %q（应为 framework | business | plugin:<id>）", scope)
	}
	return scope, name, nil
}

// checkMark 只读检查：文件必须在磁盘上，且版本表里没有记录。
func (r *Runner) checkMark(scope, name string) (FileStatus, error) {
	st, err := r.Status()
	if err != nil {
		return FileStatus{}, err
	}
	for _, s := range st {
		if s.Scope != scope || s.Name != name {
			continue
		}
		switch s.State {
		case StateApplied:
			return s, fmt.Errorf("%s 已在版本表中记录，拒绝重复标记", key(scope, name))
		case StateMismatch:
			return s, fmt.Errorf("%s 已有记录且 checksum 异常（记录 %.12s，现在 %.12s）：文件在执行后被改过，拒绝标记。请先还原文件",
				key(scope, name), s.Recorded, s.Checksum)
		}
		return s, nil
	}
	return FileStatus{}, fmt.Errorf("%s 在磁盘上不存在（已按当前引擎的迁移目录查找）", key(scope, name))
}

// MarkApplied 不执行任何 SQL，把磁盘上尚未记录的迁移文件按当前 checksum 写入版本表，来源标为手工。
// 流程：只读检查 -> 确认 -> 拿数据库级锁 -> 锁内重新检查 -> 写入并记日志。
func (r *Runner) MarkApplied(ref, actor string, confirm Confirm) error {
	scope, name, err := ParseRef(ref)
	if err != nil {
		return err
	}
	if _, err := r.checkMark(scope, name); err != nil {
		return err
	}
	if err := ask(confirm, fmt.Sprintf("将把 %s 标记为已执行但不会运行其中的任何 SQL，请确认你已手工完成其中的全部变更", key(scope, name))); err != nil {
		return err
	}
	return r.Exclusive(func() error {
		if err := r.Store.EnsureTable(); err != nil {
			return fmt.Errorf("migrate: 创建 / 升级版本表 %s: %w", Table, err)
		}
		s, err := r.checkMark(scope, name) // 等锁期间别的实例可能已处理
		if err != nil {
			return err
		}
		now := time.Now()
		if err := r.Store.Insert(Record{Scope: scope, Version: name, Checksum: s.Checksum, AppliedAt: now, Source: SourceManual}); err != nil {
			return fmt.Errorf("migrate: 写入版本表: %w", err)
		}
		r.Log.Infof("迁移手工标记为已执行（未运行 SQL）: operator=%s time=%s file=%s checksum=%.12s",
			actor, now.Format(time.RFC3339), key(scope, name), s.Checksum)
		return nil
	})
}

// manualRecord 在版本表里找 scope/name 的记录，并确认它是手工标记的。
func (r *Runner) manualRecord(scope, name string) error {
	if ok, err := r.Store.HasTable(Table); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%s 没有记录（版本表不存在）", key(scope, name))
	}
	recs, err := r.Store.Applied()
	if err != nil {
		return err
	}
	for _, rec := range recs {
		if rec.Scope == scope && rec.Version == name {
			if rec.Source != SourceManual {
				return fmt.Errorf("%s 是真实执行的记录，只允许撤销手工标记的记录，拒绝", key(scope, name))
			}
			return nil
		}
	}
	return fmt.Errorf("%s 没有记录，无需撤销", key(scope, name))
}

// Unmark 撤销一条手工标记（不能撤销真实执行的记录）。流程同 MarkApplied。
func (r *Runner) Unmark(ref, actor string, confirm Confirm) error {
	scope, name, err := ParseRef(ref)
	if err != nil {
		return err
	}
	if err := r.manualRecord(scope, name); err != nil {
		return err
	}
	if err := ask(confirm, fmt.Sprintf("将撤销 %s 的手工标记，它会重新变为待执行（下次迁移会真正运行其中的 SQL）", key(scope, name))); err != nil {
		return err
	}
	return r.Exclusive(func() error {
		if err := r.manualRecord(scope, name); err != nil {
			return err
		}
		ok, err := r.Store.DeleteManual(scope, name)
		if err != nil {
			return fmt.Errorf("migrate: 删除版本表记录: %w", err)
		}
		if !ok {
			return fmt.Errorf("%s 未被撤销（不是手工标记的记录）", key(scope, name))
		}
		r.Log.Infof("迁移手工标记已撤销: operator=%s time=%s file=%s", actor, time.Now().Format(time.RFC3339), key(scope, name))
		return nil
	})
}

func ask(confirm Confirm, prompt string) error {
	if confirm == nil {
		return ErrAborted
	}
	ok, err := confirm(prompt)
	if err != nil {
		return err
	}
	if !ok {
		return ErrAborted
	}
	return nil
}
