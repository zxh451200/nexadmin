package audit

import (
	"fmt"
	"reflect"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

const (
	maxSnapshotRows = 500 // 单条语句最多记录的行数（批量更新/删除超出部分不记录）
	settingKeyOld   = "audit:old_rows"
)

// Register 在 GORM 上登记回调。必须在启动期（Start）调用一次，不要在并发请求中调用。
//
//	create: after  -> 直接从模型字段取新值
//	update: before -> 按同样的 WHERE 查出旧行快照；after -> 按主键再查新行，算 diff
//	delete: before -> 查旧行快照；after -> 记录被删的行
//
// update/delete 的"before"是必须的：after 钩子里旧值已经没了。只有被审计的表才会多做这次查询。
func (r *Recorder) Register(db *gorm.DB) error {
	cb := db.Callback()
	if err := cb.Create().After("gorm:create").Register("audit:after_create", r.afterCreate); err != nil {
		return err
	}
	// 排在租户 scope 之后，快照的 WHERE 才带上 tenant_id。
	if err := cb.Update().Before("gorm:update").After("nexadmin:tenant_update").Register("audit:before_update", r.before); err != nil {
		return err
	}
	if err := cb.Update().After("gorm:update").Register("audit:after_update", r.afterUpdate); err != nil {
		return err
	}
	if err := cb.Delete().Before("gorm:delete").After("nexadmin:tenant_delete").Register("audit:before_delete", r.before); err != nil {
		return err
	}
	return cb.Delete().After("gorm:delete").Register("audit:after_delete", r.afterDelete)
}

// active 判断这条语句是否需要审计。
func (r *Recorder) active(db *gorm.DB) bool {
	st := db.Statement
	if db.Error != nil || st == nil || st.Schema == nil || st.Context == nil {
		return false
	}
	if skipped(st.Context) {
		return false
	}
	return r.scope.Audited(tenantOf(st.Context), st.Schema.Table)
}

func (r *Recorder) afterCreate(db *gorm.DB) {
	if !r.active(db) {
		return
	}
	st := db.Statement
	rv := st.ReflectValue
	var elems []reflect.Value
	switch rv.Kind() {
	case reflect.Struct:
		elems = []reflect.Value{rv}
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len() && i < maxSnapshotRows; i++ {
			elems = append(elems, reflect.Indirect(rv.Index(i)))
		}
	}
	for _, el := range elems {
		if !el.IsValid() || el.Kind() != reflect.Struct {
			continue
		}
		row := make(map[string]any, len(st.Schema.Fields))
		for _, f := range st.Schema.Fields {
			if f.DBName == "" {
				continue
			}
			v, _ := f.ValueOf(st.Context, el)
			row[f.DBName] = normalizeValue(v)
		}
		r.enqueue(st.Context, st.Schema.Table, pkString(row, st.Schema.PrimaryFieldDBNames), ActionCreate, Diff(nil, row))
	}
}

// before 在 update/delete 执行前，用同样的条件把将被影响的旧行查出来。
func (r *Recorder) before(db *gorm.DB) {
	if !r.active(db) {
		return
	}
	rows, err := r.snapshot(db)
	if err != nil {
		r.log.Warn("audit: snapshot failed, change not recorded", zap.String("table", db.Statement.Schema.Table), zap.Error(err))
		return
	}
	db.InstanceSet(settingKeyOld, rows)
}

func (r *Recorder) snapshot(db *gorm.DB) ([]map[string]any, error) {
	st := db.Statement
	q := db.Session(&gorm.Session{NewDB: true, Context: markSkip(st.Context)}).Table(st.Schema.Table)
	hasCond := false
	if c, ok := st.Clauses["WHERE"]; ok {
		q.Statement.Clauses["WHERE"] = c
		hasCond = true
	}
	// GORM 在 gorm:update / gorm:delete 里才会把模型主键并进 WHERE，这里照做。
	if st.ReflectValue.IsValid() {
		_, vals := schema.GetIdentityFieldValuesMap(st.Context, st.ReflectValue, st.Schema.PrimaryFields)
		col, values := schema.ToQueryValues(st.Table, st.Schema.PrimaryFieldDBNames, vals)
		if len(values) > 0 {
			q = q.Where(clause.IN{Column: col, Values: values})
			hasCond = true
		}
	}
	if !hasCond {
		return nil, nil // 没有任何条件，GORM 自己会拒绝执行
	}
	var rows []map[string]any
	if err := q.Limit(maxSnapshotRows).Find(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i] = normalizeRow(rows[i])
	}
	return rows, nil
}

func oldRows(db *gorm.DB) []map[string]any {
	v, ok := db.InstanceGet(settingKeyOld)
	if !ok {
		return nil
	}
	rows, _ := v.([]map[string]any)
	return rows
}

func (r *Recorder) afterUpdate(db *gorm.DB) {
	old := oldRows(db)
	if len(old) == 0 || db.Error != nil || db.RowsAffected == 0 {
		return
	}
	st := db.Statement
	pks := st.Schema.PrimaryFieldDBNames
	// 同一连接/事务内按主键重查新值：兼容 Save、Updates(struct/map)、gorm.Expr 等所有写法。
	q := db.Session(&gorm.Session{NewDB: true, Context: markSkip(st.Context)}).Table(st.Schema.Table)
	if len(pks) == 1 {
		ids := make([]any, 0, len(old))
		for _, o := range old {
			ids = append(ids, o[pks[0]])
		}
		q = q.Where(clause.IN{Column: clause.Column{Name: pks[0]}, Values: ids})
	} else {
		for _, o := range old {
			cond := make(map[string]any, len(pks))
			for _, k := range pks {
				cond[k] = o[k]
			}
			q = q.Or(cond)
		}
	}
	var fresh []map[string]any
	if err := q.Limit(len(old)).Find(&fresh).Error; err != nil {
		r.log.Warn("audit: reload rows failed, change not recorded", zap.String("table", st.Schema.Table), zap.Error(err))
		return
	}
	byPK := make(map[string]map[string]any, len(fresh))
	for _, f := range fresh {
		f = normalizeRow(f)
		byPK[pkKey(f, pks)] = f
	}
	for _, o := range old {
		n, ok := byPK[pkKey(o, pks)]
		if !ok {
			continue
		}
		r.enqueue(st.Context, st.Schema.Table, pkString(o, pks), ActionUpdate, Diff(o, n))
	}
}

func (r *Recorder) afterDelete(db *gorm.DB) {
	old := oldRows(db)
	if len(old) == 0 || db.Error != nil || db.RowsAffected == 0 {
		return
	}
	st := db.Statement
	for _, o := range old {
		r.enqueue(st.Context, st.Schema.Table, pkString(o, st.Schema.PrimaryFieldDBNames), ActionDelete, Diff(o, nil))
	}
}

func pkKey(row map[string]any, pks []string) string {
	parts := make([]string, len(pks))
	for i, k := range pks {
		parts[i] = fmt.Sprint(row[k])
	}
	return strings.Join(parts, "\x1f")
}

// pkString 是展示用主键：单主键就是值本身，联合主键是 a=1,b=2。
func pkString(row map[string]any, pks []string) string {
	if len(pks) == 1 {
		return fmt.Sprint(row[pks[0]])
	}
	parts := make([]string, len(pks))
	for i, k := range pks {
		parts[i] = fmt.Sprintf("%s=%v", k, row[k])
	}
	return strings.Join(parts, ",")
}
