package dict

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/cache"
	"nexadmin/core/response"
	"nexadmin/modules/internal/recbin"
	pi18n "nexadmin/pkg/i18n"
	"nexadmin/pkg/idgen"
)

// dictLabelFields 是字典项唯一可翻译的字段。
var dictLabelFields = pi18n.Fields{"label": {Max: 100}}

// maxExportRows 是导出接口（不分页）的行数上限，防止异常数据把一次响应撑爆。
// 字典类型/数据都是平台级小表，正常远达不到；真需要更大规模时改走导出中心作业，别抬这个上限。
const maxExportRows = 10000

// platformLocales 把框架语言清单（sys_language）适配成 pkg/i18n 需要的形状：
// 契约层不认识任何具体表，语言清单归调用方所有。
type platformLocales struct{}

func (platformLocales) Resolve(code string) (pi18n.Language, bool) {
	l, ok := core.LanguageByCode(code)
	if !ok {
		return pi18n.Language{}, false
	}
	return pi18n.Language{Code: l.Code, Default: l.Default}, true
}

func (platformLocales) All() []pi18n.Language {
	ls := core.Languages()
	out := make([]pi18n.Language, 0, len(ls))
	for _, l := range ls {
		out = append(out, pi18n.Language{Code: l.Code, Default: l.Default})
	}
	return out
}

// Service 封装字典的数据访问与业务规则。
type Service struct {
	db    *gorm.DB
	cache cache.Cache
}

// NewService 创建服务；cache 可为 nil（直查，无缓存）。
func NewService(db *gorm.DB, c cache.Cache) *Service { return &Service{db: db, cache: c} }

func conflict(key string, args ...any) error { return response.Conflict(key, args...) }
func notFound() error                        { return response.NotFound("error.not_found") }

// builtinErr 内置字典保护：409 + i18n dict.builtin。
func builtinErr() error { return response.Conflict("dict.builtin") }

func statusOr(s string) string {
	if s == "" {
		return "1"
	}
	return s
}

func tagOr(s string) string {
	if s == "" {
		return "default"
	}
	return s
}

// filter 追加关键字（对给定列做不区分大小写的模糊匹配）与状态过滤。
func filter(q *gorm.DB, keyword, status string, cols ...string) *gorm.DB {
	if kw := strings.ToLower(strings.TrimSpace(keyword)); kw != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(kw) + "%"
		conds := make([]string, len(cols))
		args := make([]any, len(cols))
		for i, c := range cols {
			conds[i], args[i] = "LOWER("+c+") LIKE ?", like
		}
		q = q.Where(strings.Join(conds, " OR "), args...)
	}
	if status != "" {
		q = q.Where("status IN ?", strings.Split(status, ","))
	}
	return q
}

// ---- 字典类型 ----

// typesScope 是字典类型「列表」与「导出」共用的查询条件：同一套关键字/状态过滤，只写一遍。
func (s *Service) typesScope(ctx context.Context, q ListQuery) *gorm.DB {
	return filter(s.db.WithContext(ctx).Model(&Type{}), q.Keyword, q.Status, "name", "code")
}

// ListTypes 分页查询字典类型，按 sort 排序。
func (s *Service) ListTypes(ctx context.Context, q ListQuery, page, size int) ([]Type, int64, error) {
	db := s.typesScope(ctx, q)
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	list := []Type{}
	err := db.Order("sort, id").Offset((page - 1) * size).Limit(size).Find(&list).Error
	markBuiltinTypes(list)
	return list, total, err
}

// markBuiltinTypes 就地补上派生标记 builtin（不入库，读出时按出厂清单算）。
func markBuiltinTypes(list []Type) {
	for i := range list {
		list[i].Builtin = isBuiltinType(list[i].Code)
	}
}

// GetType 读取单个字典类型。
func (s *Service) GetType(ctx context.Context, id idgen.ID) (*Type, error) {
	var t Type
	if err := s.db.WithContext(ctx).Where("id = ?", int64(id)).First(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, notFound()
		}
		return nil, err
	}
	t.Builtin = isBuiltinType(t.Code)
	return &t, nil
}

// typeCodeTaken 判断编码是否已被别的字典类型占用。
// uk_sys_dict_type_code 接软删除时已降级成普通索引（三引擎都拿不到「只对未删行唯一」的写法），
// 全局唯一性从这里保证：Model 查询自动带 deleted_at IS NULL，所以回收站里的类型不占码，
// 删掉可以重建同码；还原撞码由回收站在还原前显式检查并回 409 recycle.restore_conflict。
func (s *Service) typeCodeTaken(ctx context.Context, code string, exclude idgen.ID) (bool, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&Type{}).Where("code = ? AND id <> ?", code, int64(exclude)).Count(&n).Error
	return n > 0, err
}

// CreateType 新增字典类型。
func (s *Service) CreateType(ctx context.Context, in TypeInput) (*Type, error) {
	if taken, err := s.typeCodeTaken(ctx, in.Code, 0); err != nil {
		return nil, err
	} else if taken {
		return nil, conflict("dict.code_exists")
	}
	t := &Type{Name: in.Name, Code: in.Code, Sort: in.Sort, Status: statusOr(in.Status), Remark: in.Remark}
	t.Builtin = isBuiltinType(t.Code)
	return t, s.db.WithContext(ctx).Create(t).Error
}

// UpdateType 修改字典类型。状态/编码变更会影响 Items，旧编码与新编码都 bump。
func (s *Service) UpdateType(ctx context.Context, id idgen.ID, in TypeInput) (*Type, error) {
	cur, err := s.GetType(ctx, id)
	if err != nil {
		return nil, err
	}
	if cur.Builtin && in.Code != cur.Code { // 内置类型：名称/备注/状态可改，编码不可改
		return nil, builtinErr()
	}
	if taken, err := s.typeCodeTaken(ctx, in.Code, id); err != nil {
		return nil, err
	} else if taken {
		return nil, conflict("dict.code_exists")
	}
	err = s.db.WithContext(ctx).Model(&Type{}).Where("id = ?", int64(id)).Updates(map[string]any{
		"name": in.Name, "code": in.Code, "sort": in.Sort, "status": statusOr(in.Status), "remark": in.Remark,
	}).Error
	if err != nil {
		return nil, err
	}
	s.bumpItems(ctx, cur.Code, in.Code)
	return s.GetType(ctx, id)
}

// DeleteTypes 软删除字典类型并登记回收站；编码在出厂清单里的内置类型拒绝删除（409 dict.builtin）。
//
// 不再级联删除 sys_dict_data：条目原样留着，还原类型时整套字典才是完整的（软删除的类型
// 已经查不出来，Items 按 code 取不到它，不会泄漏给业务）。只有「彻底删除」才按
// recbin.Children[TypeDictType] 级联物理清掉条目（含已软删的条目）。
func (s *Service) DeleteTypes(ctx context.Context, ids []idgen.ID) error {
	raw := toInt64(ids)
	var types []Type
	if err := s.db.WithContext(ctx).Where("id IN ?", raw).Find(&types).Error; err != nil {
		return err
	}
	for _, t := range types {
		if isBuiltinType(t.Code) {
			return builtinErr()
		}
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, t := range types {
			if err := tx.Delete(&Type{}, "id = ?", int64(t.ID)).Error; err != nil {
				return err
			}
			// 字典是平台级表（源行没有 tenant_id），回收记录的 tenant_id 由 GORM 从 ctx 的当前租户填入。
			if err := recbin.Add(ctx, tx, recbin.TypeDictType, t.ID, t.Name, recbin.Operator(ctx)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	// 软删的类型 Items 查不到，缓存跟着失效
	codes := make([]string, 0, len(types))
	for _, t := range types {
		codes = append(codes, t.Code)
	}
	s.bumpItems(ctx, codes...)
	return nil
}

// SortTypes 按 ids 顺序重写 sort（10、20、30…）。
func (s *Service) SortTypes(ctx context.Context, ids []idgen.ID) error {
	return sortIDs(s.db.WithContext(ctx), &Type{}, ids)
}

// ExportTypes 按与 ListTypes 完全相同的筛选条件返回全部字典类型（不分页），供前端生成 CSV。
// 上限 maxExportRows 只是兜底：字典是平台级小表（几百行量级），不会走到异步导出作业。
func (s *Service) ExportTypes(ctx context.Context, q ListQuery) ([]Type, error) {
	list := []Type{}
	err := s.typesScope(ctx, q).Order("sort, id").Limit(maxExportRows).Find(&list).Error
	markBuiltinTypes(list)
	return list, err
}

// ---- 字典数据 ----

// dataScope 是字典数据「列表」与「导出」共用的查询条件：同一套关键字/状态/类型过滤，只写一遍。
func (s *Service) dataScope(ctx context.Context, q ListQuery) (*gorm.DB, error) {
	db := filter(s.db.WithContext(ctx).Model(&Data{}), q.Keyword, q.Status, "dict_label", "dict_value")
	if q.TypeID != "" {
		tid, err := idgen.Parse(q.TypeID)
		if err != nil {
			return nil, response.BadRequest("error.bad_request").Wrap(err)
		}
		db = db.Where("type_id = ?", int64(tid))
	}
	return db, nil
}

// ListData 分页查询字典数据，可按 typeId / status / 关键字过滤。
func (s *Service) ListData(ctx context.Context, q ListQuery, page, size int) ([]Data, int64, error) {
	db, err := s.dataScope(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	list := []Data{}
	err = db.Order("sort, id").Offset((page - 1) * size).Limit(size).Find(&list).Error
	return list, total, err
}

// ExportData 按与 ListData 完全相同的筛选条件返回全部字典数据（不分页），供前端生成 CSV。
func (s *Service) ExportData(ctx context.Context, q ListQuery) ([]Data, error) {
	db, err := s.dataScope(ctx, q)
	if err != nil {
		return nil, err
	}
	list := []Data{}
	return list, db.Order("sort, id").Limit(maxExportRows).Find(&list).Error
}

// GetData 读取单条字典数据。
func (s *Service) GetData(ctx context.Context, id idgen.ID) (*Data, error) {
	var d Data
	if err := s.db.WithContext(ctx).Where("id = ?", int64(id)).First(&d).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, notFound()
		}
		return nil, err
	}
	return &d, nil
}

// checkData 校验所属类型存在、同类型下值唯一。
// 唯一性同 typeCodeTaken：uk_sys_dict_data_type_value 已降级为普通索引，
// 靠这里的 deleted_at IS NULL（Model 查询自动追加）只比对未删条目。
func (s *Service) checkData(ctx context.Context, in DataInput, exclude idgen.ID) error {
	if _, err := s.GetType(ctx, in.TypeID); err != nil {
		return response.BadRequest("dict.type_not_found")
	}
	var n int64
	err := s.db.WithContext(ctx).Model(&Data{}).
		Where("type_id = ? AND dict_value = ? AND id <> ?", int64(in.TypeID), in.Value, int64(exclude)).Count(&n).Error
	if err != nil {
		return err
	}
	if n > 0 {
		return conflict("dict.value_exists")
	}
	return nil
}

// CreateData 新增字典数据。
func (s *Service) CreateData(ctx context.Context, in DataInput) (*Data, error) {
	if err := s.checkData(ctx, in, 0); err != nil {
		return nil, err
	}
	i18nJSON, err := pi18n.Normalize(in.I18nJSON, platformLocales{}, dictLabelFields)
	if err != nil {
		return nil, err
	}
	d := &Data{TypeID: in.TypeID, Label: in.Label, I18nJSON: i18nJSON, Value: in.Value, Tag: tagOr(in.Tag),
		Sort: in.Sort, Status: statusOr(in.Status), Remark: in.Remark}
	if err := s.db.WithContext(ctx).Create(d).Error; err != nil {
		return nil, err
	}
	s.bumpItems(ctx, s.codeOfTypeID(ctx, in.TypeID))
	return d, nil
}

// UpdateData 修改字典数据。
func (s *Service) UpdateData(ctx context.Context, id idgen.ID, in DataInput) (*Data, error) {
	cur, err := s.GetData(ctx, id)
	if err != nil {
		return nil, err
	}
	// 内置数据项：label/排序/样式/状态可改，value 不可改，也不可移到别的类型。
	if in.Value != cur.Value || in.TypeID != cur.TypeID {
		ct, err := s.GetType(ctx, cur.TypeID)
		if err != nil {
			return nil, err
		}
		if isBuiltinItem(ct.Code, cur.Value) {
			return nil, builtinErr()
		}
	}
	if err := s.checkData(ctx, in, id); err != nil {
		return nil, err
	}
	i18nJSON, err := pi18n.Normalize(in.I18nJSON, platformLocales{}, dictLabelFields)
	if err != nil {
		return nil, err
	}
	err = s.db.WithContext(ctx).Model(&Data{}).Where("id = ?", int64(id)).Updates(map[string]any{
		"type_id": int64(in.TypeID), "dict_label": in.Label, "i18n_json": i18nJSON, "dict_value": in.Value, "tag": tagOr(in.Tag),
		"sort": in.Sort, "status": statusOr(in.Status), "remark": in.Remark,
	}).Error
	if err != nil {
		return nil, err
	}
	// 条目可能被挪到别的类型：新旧两个编码都失效
	s.bumpItems(ctx, s.codeOfTypeID(ctx, cur.TypeID), s.codeOfTypeID(ctx, in.TypeID))
	return s.GetData(ctx, id)
}

// DeleteData 软删除字典数据并登记回收站；内置类型下的出厂数据项（seed 里的 value）拒绝删除（409 dict.builtin）。
func (s *Service) DeleteData(ctx context.Context, ids []idgen.ID) error {
	raw := toInt64(ids)
	var rows []Data
	if err := s.db.WithContext(ctx).Select("id", "type_id", "dict_label", "dict_value").Where("id IN ?", raw).Find(&rows).Error; err != nil {
		return err
	}
	codes := make([]string, 0, 2)
	if len(rows) > 0 {
		tids := make([]int64, 0, len(rows))
		for _, r := range rows {
			tids = append(tids, int64(r.TypeID))
		}
		var types []Type
		if err := s.db.WithContext(ctx).Select("id", "code").Where("id IN ?", tids).Find(&types).Error; err != nil {
			return err
		}
		codeOf := make(map[idgen.ID]string, len(types))
		for _, t := range types {
			codeOf[t.ID] = t.Code
			codes = append(codes, t.Code)
		}
		for _, r := range rows {
			if isBuiltinItem(codeOf[r.TypeID], r.Value) {
				return builtinErr()
			}
		}
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, r := range rows {
			if err := tx.Delete(&Data{}, "id = ?", int64(r.ID)).Error; err != nil {
				return err
			}
			if err := recbin.Add(ctx, tx, recbin.TypeDictData, r.ID, r.Label, recbin.Operator(ctx)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	s.bumpItems(ctx, codes...)
	return nil
}

// SortData 按 ids 顺序重写 sort（Items 按 sort 排序，排序变更同样失效缓存）。
func (s *Service) SortData(ctx context.Context, ids []idgen.ID) error {
	if err := sortIDs(s.db.WithContext(ctx), &Data{}, ids); err != nil {
		return err
	}
	codes := make([]string, 0, len(ids))
	for _, id := range ids {
		var typeID int64
		if err := s.db.WithContext(ctx).Model(&Data{}).Select("type_id").Where("id = ?", int64(id)).Scan(&typeID).Error; err != nil {
			return err
		}
		codes = append(codes, s.codeOfTypeID(ctx, idgen.ID(typeID)))
	}
	s.bumpItems(ctx, codes...)
	return nil
}

// Items 返回某字典下启用的数据（前端下拉/标签渲染），标签按请求语言取：
// 先查 i18n_json 里这门语言的译文，没有则回退默认语言的 dict_label；
// 老数据的 label_en 在没有 i18n_json 时当英文兜底（升级前写进去的译文不能丢）。
//
// 结果走缓存（这是全站下拉/标签的高频接口，原来每次请求两条 DB 查询）：
// 缓存键带类型版本号，任何数据/类型变更 bump 版本即全语言失效；TTL 兜底自愈。
func (s *Service) Items(ctx context.Context, code, lang string) ([]Item, error) {
	if s.cache == nil {
		return s.itemsFromDB(ctx, code, lang)
	}
	ver, err := s.cache.Get(ctx, dictVerKey(code))
	if err != nil {
		ver = "0" // 没有版本键（从没变更过）视作 0
	}
	key := cache.Key(ctx, "dict", "items", code, ver, lang)
	if v, gerr := s.cache.Get(ctx, key); gerr == nil {
		var out []Item
		if json.Unmarshal([]byte(v), &out) == nil {
			return out, nil
		}
	}
	out, err := s.itemsFromDB(ctx, code, lang)
	if err != nil {
		return nil, err
	}
	if b, jerr := json.Marshal(out); jerr == nil {
		_ = s.cache.Set(ctx, key, string(b), dictItemsTTL)
	}
	return out, nil
}

func (s *Service) itemsFromDB(ctx context.Context, code, lang string) ([]Item, error) {
	var t Type
	err := s.db.WithContext(ctx).Where("code = ? AND status = ?", code, "1").First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return []Item{}, nil
	}
	if err != nil {
		return nil, err
	}
	var rows []Data
	if err := s.db.WithContext(ctx).Where("type_id = ? AND status = ?", int64(t.ID), "1").Order("sort, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(rows))
	for _, r := range rows {
		label := dictLabelOf(r, lang)
		out = append(out, Item{Value: r.Value, Label: label, Tag: r.Tag})
	}
	return out, nil
}

// dictItemsTTL 是条目缓存的兜底过期：失效漏了（如手工改库）最多 5 分钟后自愈。
const dictItemsTTL = 5 * time.Minute

func dictVerKey(code string) string { return cache.GlobalKey("dict", "ver", code) }

// bumpItems 把受影响字典的版本号 +1，Items 的全部语言缓存随即失效。cache 不可用或
// 不支持计数时静默跳过（TTL 兜底）。
func (s *Service) bumpItems(ctx context.Context, codes ...string) {
	if s.cache == nil {
		return
	}
	ctr, ok := cache.As[cache.Counter](s.cache)
	if !ok {
		return
	}
	for _, code := range codes {
		if code = strings.TrimSpace(code); code != "" {
			_, _, _ = ctr.Incr(ctx, dictVerKey(code), 30*24*time.Hour)
		}
	}
}

// codeOfTypeID 查类型编码（bump 用）；查不到返回空串，bump 会跳过。
func (s *Service) codeOfTypeID(ctx context.Context, typeID idgen.ID) string {
	var code string
	_ = s.db.WithContext(ctx).Model(&Type{}).Select("code").Where("id = ?", int64(typeID)).Scan(&code).Error
	return code
}

// dictLabelOf 按语言取一条字典数据的标签。
// 顺序：i18n_json 里这门语言的 label → 老列 label_en（仅英文）→ 默认语言的 dict_label。
func dictLabelOf(r Data, lang string) string {
	if v, ok := pi18n.ParseDoc(r.I18nJSON).For(lang)["label"]; ok && v != "" {
		return v
	}
	if r.LabelEn != "" && lang != "" {
		base, _, _ := strings.Cut(strings.ToLower(lang), "-")
		if base == "en" {
			return r.LabelEn
		}
	}
	return r.Label
}

func toInt64(ids []idgen.ID) []int64 {
	out := make([]int64, len(ids))
	for i, id := range ids {
		out[i] = int64(id)
	}
	return out
}

// sortIDs 在事务里把 ids 依次改为 sort=10、20、30…
func sortIDs(db *gorm.DB, model any, ids []idgen.ID) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for i, id := range ids {
			if err := tx.Model(model).Where("id = ?", int64(id)).Update("sort", (i+1)*10).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
