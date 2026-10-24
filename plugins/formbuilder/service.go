package formbuilder

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"go.uber.org/zap"

	"nexadmin/pkg/idgen"
)

// Actor 是当前操作者（来自登录令牌）。
type Actor struct {
	ID       idgen.ID
	Username string
	Super    bool
}

// Service 是业务层：设计、版本、填写、收集。
type Service struct {
	store Store
	dir   Directory
	log   *zap.Logger
	now   func() time.Time
	newID func() (idgen.ID, error)
}

// NewService 创建服务。
func NewService(store Store, dir Directory, log *zap.Logger) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{store: store, dir: dir, log: log, now: time.Now, newID: idgen.Next}
}

// FormDTO 是表单对外的视图。Definition / Audience 只在详情里返回。
type FormDTO struct {
	ID               idgen.ID    `json:"id"`
	Name             string      `json:"name"`
	Description      string      `json:"description"`
	Status           string      `json:"status"`
	PublishedVersion int         `json:"publishedVersion"`
	PublishedAt      *time.Time  `json:"publishedAt"`
	HasUnpublished   bool        `json:"hasUnpublished"`
	AllowRepeat      bool        `json:"allowRepeat"`
	DeadlineAt       *time.Time  `json:"deadlineAt"`
	MaxSubmissions   int         `json:"maxSubmissions"`
	SubmissionCount  int64       `json:"submissionCount"`
	CreatedAt        time.Time   `json:"createdAt"`
	UpdatedAt        time.Time   `json:"updatedAt"`
	Definition       *Definition `json:"definition,omitempty"`
	Audience         *Audience   `json:"audience,omitempty"`
}

func toDTO(f *Form) FormDTO {
	return FormDTO{
		ID: f.ID, Name: f.Name, Description: f.Description, Status: f.Status,
		PublishedVersion: f.PublishedVersion, PublishedAt: f.PublishedAt,
		HasUnpublished: f.PublishedVersion == 0 || f.DraftRev > f.PublishedRev,
		AllowRepeat:    f.AllowRepeat, DeadlineAt: f.DeadlineAt, MaxSubmissions: f.MaxSubmissions,
		SubmissionCount: f.SubmissionCount, CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt,
	}
}

func (s *Service) detail(f *Form) (*FormDTO, error) {
	dto := toDTO(f)
	def, err := decodeDefinition([]byte(f.DraftDef))
	if err != nil {
		return nil, err
	}
	aud := parseAudience(f.Audience)
	dto.Definition, dto.Audience = def, &aud
	return &dto, nil
}

func parseAudience(raw string) Audience {
	a := Audience{Roles: []string{}, Users: []string{}}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &a) // 损坏的值按"未限制"处理会放宽权限，所以下面 allowed 里遇到解析失败会单独拒绝
	}
	if a.Roles == nil {
		a.Roles = []string{}
	}
	if a.Users == nil {
		a.Users = []string{}
	}
	return a
}

// normalizeAudience 校验并规范化可见范围：数量上限、ID 格式、去重。
func normalizeAudience(a Audience) (Audience, error) {
	clean := func(in []string, what string) ([]string, error) {
		if len(in) > MaxAudience {
			return nil, bad(fmt.Sprintf("%s最多 %d 个", what, MaxAudience))
		}
		seen := map[int64]struct{}{}
		out := make([]string, 0, len(in))
		for _, s := range in {
			id, err := idgen.Parse(s)
			if err != nil || id <= 0 {
				return nil, bad(what + "里有不合法的 ID")
			}
			if _, dup := seen[int64(id)]; dup {
				continue
			}
			seen[int64(id)] = struct{}{}
			out = append(out, strconv.FormatInt(int64(id), 10))
		}
		return out, nil
	}
	roles, err := clean(a.Roles, "角色")
	if err != nil {
		return a, err
	}
	users, err := clean(a.Users, "用户")
	if err != nil {
		return a, err
	}
	return Audience{Roles: roles, Users: users}, nil
}

// ---- 设计 ----

// CreateForm 新建一个空白草稿。
func (s *Service) CreateForm(ctx context.Context, actor Actor, name, desc string) (*FormDTO, error) {
	def := Definition{Name: name, Description: desc, Fields: []Field{}}
	if err := ValidateDefinition(&def); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(def)
	f := &Form{
		Name: def.Name, Description: def.Description, Status: StatusDraft, DraftDef: string(raw), DraftRev: 1,
		Audience: `{"roles":[],"users":[]}`, CreatedBy: actor.ID,
	}
	if err := s.store.CreateForm(ctx, f); err != nil {
		return nil, err
	}
	return s.detail(f)
}

// ListForms 分页列出当前租户的表单。
func (s *Service) ListForms(ctx context.Context, q FormQuery) ([]FormDTO, int64, error) {
	list, total, err := s.store.ListForms(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	out := make([]FormDTO, len(list))
	for i := range list {
		out[i] = toDTO(&list[i])
	}
	return out, total, nil
}

// GetForm 返回表单详情（含草稿定义与可见范围）。
func (s *Service) GetForm(ctx context.Context, id idgen.ID) (*FormDTO, error) {
	f, err := s.store.GetForm(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.detail(f)
}

// SaveInput 是保存草稿的入参；Audience 为 nil 表示不改可见范围。
type SaveInput struct {
	Definition Definition `json:"definition"`
	Audience   *Audience  `json:"audience"`
}

// SaveDraft 保存草稿。草稿不影响已发布版本。
func (s *Service) SaveDraft(ctx context.Context, id idgen.ID, in SaveInput) (*FormDTO, error) {
	if err := ValidateDefinition(&in.Definition); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(in.Definition)
	if err != nil {
		return nil, errDefInvalid
	}
	if err := s.store.SaveDraft(ctx, id, in.Definition.Name, in.Definition.Description, string(raw)); err != nil {
		return nil, err
	}
	if in.Audience != nil {
		aud, err := normalizeAudience(*in.Audience)
		if err != nil {
			return nil, err
		}
		ab, _ := json.Marshal(aud)
		if err := s.store.SaveAudience(ctx, id, string(ab)); err != nil {
			return nil, err
		}
	}
	return s.GetForm(ctx, id)
}

// DeleteForm 删除表单及其全部版本与提交。
func (s *Service) DeleteForm(ctx context.Context, id idgen.ID) error {
	return s.store.DeleteForm(ctx, id)
}

// Publish 把当前草稿冻结成新版本。字典绑定的选项在此刻快照进版本，之后字典变化不影响该版本。
func (s *Service) Publish(ctx context.Context, actor Actor, id idgen.ID) (*FormDTO, error) {
	f, err := s.store.GetForm(ctx, id)
	if err != nil {
		return nil, err
	}
	if f.PublishedVersion >= MaxVersions {
		return nil, conflict(fmt.Sprintf("发布版本数已达上限 %d", MaxVersions))
	}
	def, err := decodeDefinition([]byte(f.DraftDef))
	if err != nil {
		return nil, err
	}
	if err := s.snapshotDicts(ctx, def); err != nil {
		return nil, err
	}
	if len(def.Fields) == 0 {
		return nil, bad("表单至少需要一个字段")
	}
	if err := ValidateDefinition(def); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(def)
	if err != nil || len(raw) > MaxDefBytes {
		return nil, errDefTooLarge
	}
	v := &Version{Version: f.PublishedVersion + 1, Definition: string(raw), CreatedBy: actor.ID}
	meta := PublishMeta{
		Name: def.Name, Description: def.Description, Rev: f.DraftRev, AllowRepeat: def.Settings.AllowRepeat,
		DeadlineAt: def.Settings.Deadline, MaxSubmissions: def.Settings.MaxSubmissions,
	}
	if err := s.store.Publish(ctx, id, v, meta); err != nil {
		if err == errStoreConflict {
			return nil, conflict("表单已被他人修改或发布，请刷新后重试")
		}
		return nil, err
	}
	return s.GetForm(ctx, id)
}

// snapshotDicts 把绑定了字典的字段的选项替换成字典当前的启用项。
func (s *Service) snapshotDicts(ctx context.Context, def *Definition) error {
	for i := range def.Fields {
		f := &def.Fields[i]
		if f.DictType == "" || !f.usesOptions() {
			continue
		}
		if !dictRe.MatchString(f.DictType) {
			return bad(fmt.Sprintf("字段 %s 的字典类型不合法", f.Key))
		}
		items, err := s.dir.DictItems(ctx, f.DictType)
		if err != nil {
			s.log.Warn("formbuilder: read dict", zap.String("dict", f.DictType), zap.Error(err))
			return bad(fmt.Sprintf("读取字典 %s 失败", f.DictType))
		}
		if len(items) == 0 || len(items) > MaxOptions {
			return bad(fmt.Sprintf("字典 %s 没有可用项，或超过 %d 项", f.DictType, MaxOptions))
		}
		f.Options = items
	}
	return nil
}

// SetOnline 下线/重新上线已发布过的表单；上线不会生成新版本。
func (s *Service) SetOnline(ctx context.Context, id idgen.ID, online bool) (*FormDTO, error) {
	if _, err := s.store.GetForm(ctx, id); err != nil {
		return nil, err
	}
	from, to := StatusPublished, StatusOffline
	if online {
		from, to = StatusOffline, StatusPublished
	}
	if err := s.store.SetStatus(ctx, id, from, to); err != nil {
		if err == errStoreConflict {
			return nil, conflict("当前状态不允许此操作")
		}
		return nil, err
	}
	return s.GetForm(ctx, id)
}

// VersionInfo 是版本列表项。
type VersionInfo struct {
	Version    int       `json:"version"`
	FieldCount int       `json:"fieldCount"`
	CreatedAt  time.Time `json:"createdAt"`
	CreatedBy  idgen.ID  `json:"createdBy"`
}

// ListVersions 列出表单的发布版本。
func (s *Service) ListVersions(ctx context.Context, id idgen.ID) ([]VersionInfo, error) {
	if _, err := s.store.GetForm(ctx, id); err != nil {
		return nil, err
	}
	vs, err := s.store.ListVersions(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]VersionInfo, 0, len(vs))
	for _, v := range vs {
		n := 0
		if d, err := decodeDefinition([]byte(v.Definition)); err == nil {
			n = len(d.Fields)
		}
		out = append(out, VersionInfo{Version: v.Version, FieldCount: n, CreatedAt: v.CreatedAt, CreatedBy: v.CreatedBy})
	}
	return out, nil
}

// GetVersionDefinition 返回某个版本冻结的定义。
func (s *Service) GetVersionDefinition(ctx context.Context, id idgen.ID, version int) (*Definition, error) {
	v, err := s.store.GetVersion(ctx, id, version)
	if err != nil {
		return nil, err
	}
	return decodeDefinition([]byte(v.Definition))
}
