package dict

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/response"
	pi18n "nexadmin/pkg/i18n"
	"nexadmin/pkg/idgen"
)

// dictLabelFields 是字典项唯一可翻译的字段。
var dictLabelFields = pi18n.Fields{"label": {Max: 100}}

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
type Service struct{ db *gorm.DB }

// NewService 创建服务。
func NewService(db *gorm.DB) *Service { return &Service{db: db} }

func conflict(msg string) error { return response.Conflict("error.conflict").WithMsg(msg) }
func notFound() error           { return response.NotFound("error.not_found") }

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

// ListTypes 分页查询字典类型，按 sort 排序。
func (s *Service) ListTypes(ctx context.Context, q ListQuery, page, size int) ([]Type, int64, error) {
	db := filter(s.db.WithContext(ctx).Model(&Type{}), q.Keyword, q.Status, "name", "code")
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	list := []Type{}
	err := db.Order("sort, id").Offset((page - 1) * size).Limit(size).Find(&list).Error
	for i := range list {
		list[i].Builtin = isBuiltinType(list[i].Code)
	}
	return list, total, err
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
		return nil, conflict("字典编码已存在")
	}
	t := &Type{Name: in.Name, Code: in.Code, Sort: in.Sort, Status: statusOr(in.Status), Remark: in.Remark}
	t.Builtin = isBuiltinType(t.Code)
	return t, s.db.WithContext(ctx).Create(t).Error
}

// UpdateType 修改字典类型。
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
		return nil, conflict("字典编码已存在")
	}
	err = s.db.WithContext(ctx).Model(&Type{}).Where("id = ?", int64(id)).Updates(map[string]any{
		"name": in.Name, "code": in.Code, "sort": in.Sort, "status": statusOr(in.Status), "remark": in.Remark,
	}).Error
	if err != nil {
		return nil, err
	}
	return s.GetType(ctx, id)
}

// DeleteTypes 删除字典类型，并级联删除其下全部字典数据；编码在出厂清单里的内置类型拒绝删除（409 dict.builtin）。
func (s *Service) DeleteTypes(ctx context.Context, ids []idgen.ID) error {
	raw := toInt64(ids)
	var codes []string
	if err := s.db.WithContext(ctx).Model(&Type{}).Where("id IN ?", raw).Pluck("code", &codes).Error; err != nil {
		return err
	}
	for _, c := range codes {
		if isBuiltinType(c) {
			return builtinErr()
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("type_id IN ?", raw).Delete(&Data{}).Error; err != nil {
			return err
		}
		return tx.Where("id IN ?", raw).Delete(&Type{}).Error
	})
}

// SortTypes 按 ids 顺序重写 sort（10、20、30…）。
func (s *Service) SortTypes(ctx context.Context, ids []idgen.ID) error {
	return sortIDs(s.db.WithContext(ctx), &Type{}, ids)
}

// ---- 字典数据 ----

// ListData 分页查询字典数据，可按 typeId / status / 关键字过滤。
func (s *Service) ListData(ctx context.Context, q ListQuery, page, size int) ([]Data, int64, error) {
	db := filter(s.db.WithContext(ctx).Model(&Data{}), q.Keyword, q.Status, "dict_label", "dict_value")
	if q.TypeID != "" {
		tid, err := idgen.Parse(q.TypeID)
		if err != nil {
			return nil, 0, response.BadRequest("error.bad_request").Wrap(err)
		}
		db = db.Where("type_id = ?", int64(tid))
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	list := []Data{}
	err := db.Order("sort, id").Offset((page - 1) * size).Limit(size).Find(&list).Error
	return list, total, err
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
func (s *Service) checkData(ctx context.Context, in DataInput, exclude idgen.ID) error {
	if _, err := s.GetType(ctx, in.TypeID); err != nil {
		return response.BadRequest("error.bad_request").WithMsg("字典类型不存在")
	}
	var n int64
	err := s.db.WithContext(ctx).Model(&Data{}).
		Where("type_id = ? AND dict_value = ? AND id <> ?", int64(in.TypeID), in.Value, int64(exclude)).Count(&n).Error
	if err != nil {
		return err
	}
	if n > 0 {
		return conflict("字典值已存在")
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
	return d, s.db.WithContext(ctx).Create(d).Error
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
	return s.GetData(ctx, id)
}

// DeleteData 删除字典数据；内置类型下的出厂数据项（seed 里的 value）拒绝删除（409 dict.builtin）。
func (s *Service) DeleteData(ctx context.Context, ids []idgen.ID) error {
	raw := toInt64(ids)
	var rows []Data
	if err := s.db.WithContext(ctx).Select("id", "type_id", "dict_value").Where("id IN ?", raw).Find(&rows).Error; err != nil {
		return err
	}
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
		}
		for _, r := range rows {
			if isBuiltinItem(codeOf[r.TypeID], r.Value) {
				return builtinErr()
			}
		}
	}
	return s.db.WithContext(ctx).Where("id IN ?", raw).Delete(&Data{}).Error
}

// SortData 按 ids 顺序重写 sort。
func (s *Service) SortData(ctx context.Context, ids []idgen.ID) error {
	return sortIDs(s.db.WithContext(ctx), &Data{}, ids)
}

// Items 返回某字典下启用的数据（前端下拉/标签渲染），标签按请求语言取：
// 先查 i18n_json 里这门语言的译文，没有则回退默认语言的 dict_label；
// 老数据的 label_en 在没有 i18n_json 时当英文兜底（升级前写进去的译文不能丢）。
func (s *Service) Items(ctx context.Context, code, lang string) ([]Item, error) {
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
