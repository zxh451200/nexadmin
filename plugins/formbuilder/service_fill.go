package formbuilder

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/i18n"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 导入导出表单定义用的信封。
const (
	EnvelopeFormat        = "nexadmin.formbuilder"
	EnvelopeSchemaVersion = 1
)

// Envelope 是导出的表单定义文件。不含可见范围（角色/用户 ID 只在本租户有意义）。
type Envelope struct {
	Format        string     `json:"format"`
	SchemaVersion int        `json:"schemaVersion"`
	ExportedAt    time.Time  `json:"exportedAt"`
	Definition    Definition `json:"definition"`
}

// ExportDefinition 导出草稿（version = 0）或某个已发布版本的定义。
func (s *Service) ExportDefinition(ctx context.Context, id idgen.ID, version int) (*Envelope, error) {
	var def *Definition
	if version > 0 {
		d, err := s.GetVersionDefinition(ctx, id, version)
		if err != nil {
			return nil, err
		}
		def = d
	} else {
		f, err := s.store.GetForm(ctx, id)
		if err != nil {
			return nil, err
		}
		if def, err = decodeDefinition([]byte(f.DraftDef)); err != nil {
			return nil, err
		}
	}
	return &Envelope{Format: EnvelopeFormat, SchemaVersion: EnvelopeSchemaVersion, ExportedAt: s.now().UTC(), Definition: *def}, nil
}

// ImportDefinition 用导出的信封创建一个新的草稿表单。导入的内容与手工保存草稿走同一套校验。
func (s *Service) ImportDefinition(ctx context.Context, actor Actor, raw []byte) (*FormDTO, error) {
	if len(raw) > MaxDefBytes+1024 {
		return nil, errDefTooLarge
	}
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, errDefInvalid
	}
	if env.Format != EnvelopeFormat {
		return nil, bad("formbuilder.import_not_envelope")
	}
	if env.SchemaVersion != EnvelopeSchemaVersion {
		return nil, bad("formbuilder.import_version_unsupported", env.SchemaVersion)
	}
	if env.Definition.Fields == nil {
		env.Definition.Fields = []Field{}
	}
	if err := ValidateDefinition(&env.Definition); err != nil {
		return nil, err
	}
	def, _ := json.Marshal(env.Definition)
	f := &Form{
		Name: env.Definition.Name, Description: env.Definition.Description, Status: StatusDraft,
		DraftDef: string(def), DraftRev: 1, Audience: `{"roles":[],"users":[]}`, CreatedBy: actor.ID,
	}
	if err := s.store.CreateForm(ctx, f); err != nil {
		return nil, err
	}
	return s.detail(f)
}

// ---- 填写 ----

var idemRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// allowed 判断操作者能否填写该表单：超管放行；未限制范围的表单所有登录用户可填；
// 否则用户在名单里，或拥有名单里的任一角色。可见范围数据损坏或读取角色失败时一律拒绝（fail closed）。
func (s *Service) allowed(ctx context.Context, actor Actor, f *Form, roles *roleCache) (bool, error) {
	if actor.Super {
		return true, nil
	}
	var a Audience
	if f.Audience != "" {
		if err := json.Unmarshal([]byte(f.Audience), &a); err != nil {
			s.log.Warn("formbuilder: corrupt audience", zap.Int64("form", int64(f.ID)))
			return false, nil
		}
	}
	if a.Empty() {
		return true, nil
	}
	me := strconv.FormatInt(int64(actor.ID), 10)
	for _, u := range a.Users {
		if u == me {
			return true, nil
		}
	}
	if len(a.Roles) == 0 {
		return false, nil
	}
	mine, err := roles.get(ctx, s.dir, actor.ID)
	if err != nil {
		return false, err
	}
	for _, r := range a.Roles {
		if _, ok := mine[r]; ok {
			return true, nil
		}
	}
	return false, nil
}

// roleCache 在一次请求内只查一次用户角色。
type roleCache struct {
	done bool
	set  map[string]struct{}
	err  error
}

func (c *roleCache) get(ctx context.Context, dir Directory, uid idgen.ID) (map[string]struct{}, error) {
	if !c.done {
		c.done = true
		ids, err := dir.UserRoleIDs(ctx, uid)
		c.err = err
		c.set = make(map[string]struct{}, len(ids))
		for _, id := range ids {
			c.set[strconv.FormatInt(int64(id), 10)] = struct{}{}
		}
	}
	return c.set, c.err
}

// FillItem 是"我的表单"列表项。Closed 非空表示当前不能提交：expired / full / submitted。
type FillItem struct {
	ID               idgen.ID   `json:"id"`
	Name             string     `json:"name"`
	Description      string     `json:"description"`
	PublishedVersion int        `json:"publishedVersion"`
	DeadlineAt       *time.Time `json:"deadlineAt"`
	MaxSubmissions   int        `json:"maxSubmissions"`
	SubmissionCount  int64      `json:"submissionCount"`
	AllowRepeat      bool       `json:"allowRepeat"`
	Submitted        bool       `json:"submitted"`
	Closed           string     `json:"closed"`
}

func (s *Service) closedReason(f *Form, submitted bool) string {
	switch {
	case f.DeadlineAt != nil && !s.now().Before(*f.DeadlineAt):
		return "expired"
	case f.MaxSubmissions > 0 && f.SubmissionCount >= int64(f.MaxSubmissions):
		return "full"
	case submitted && !f.AllowRepeat:
		return "submitted"
	}
	return ""
}

func itemOf(f *Form, submitted bool, closed string) FillItem {
	return FillItem{
		ID: f.ID, Name: f.Name, Description: f.Description, PublishedVersion: f.PublishedVersion,
		DeadlineAt: f.DeadlineAt, MaxSubmissions: f.MaxSubmissions, SubmissionCount: f.SubmissionCount,
		AllowRepeat: f.AllowRepeat, Submitted: submitted, Closed: closed,
	}
}

const maxFillList = 200

// FillList 列出操作者可见的已发布表单。
func (s *Service) FillList(ctx context.Context, actor Actor) ([]FillItem, error) {
	forms, err := s.store.ListPublished(ctx, maxFillList)
	if err != nil {
		return nil, err
	}
	var rc roleCache
	visible := make([]*Form, 0, len(forms))
	ids := make([]idgen.ID, 0, len(forms))
	for i := range forms {
		ok, err := s.allowed(ctx, actor, &forms[i], &rc)
		if err != nil {
			return nil, err
		}
		if ok {
			visible = append(visible, &forms[i])
			ids = append(ids, forms[i].ID)
		}
	}
	done, err := s.store.SubmittedForms(ctx, actor.ID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]FillItem, 0, len(visible))
	for _, f := range visible {
		out = append(out, itemOf(f, done[f.ID], s.closedReason(f, done[f.ID])))
	}
	return out, nil
}

// PendingCount 是"待填表单数"：可见、未关闭、且操作者还没提交过。
func (s *Service) PendingCount(ctx context.Context, actor Actor) (int, error) {
	items, err := s.FillList(ctx, actor)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, it := range items {
		if it.Closed == "" && !it.Submitted {
			n++
		}
	}
	return n, nil
}

// FillDetail 是填写页要渲染的内容：已发布版本的定义，绝不包含草稿。
type FillDetail struct {
	FillItem
	Definition *Definition `json:"definition"`
}

// FillGet 返回填写页数据。
func (s *Service) FillGet(ctx context.Context, actor Actor, id idgen.ID) (*FillDetail, error) {
	f, err := s.publishedForm(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	v, err := s.store.GetVersion(ctx, id, f.PublishedVersion)
	if err != nil {
		return nil, err
	}
	def, err := decodeDefinition([]byte(v.Definition))
	if err != nil {
		return nil, err
	}
	done, err := s.store.SubmittedForms(ctx, actor.ID, []idgen.ID{id})
	if err != nil {
		return nil, err
	}
	return &FillDetail{FillItem: itemOf(f, done[id], s.closedReason(f, done[id])), Definition: def}, nil
}

// publishedForm 取表单并要求：已发布（未下线）、操作者在可见范围内。
func (s *Service) publishedForm(ctx context.Context, actor Actor, id idgen.ID) (*Form, error) {
	f, err := s.store.GetForm(ctx, id)
	if err != nil {
		return nil, err
	}
	if f.Status != StatusPublished || f.PublishedVersion == 0 {
		return nil, errNotFound
	}
	ok, err := s.allowed(ctx, actor, f, &roleCache{})
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, forbidden("formbuilder.audience_denied")
	}
	return f, nil
}

// SubmitInput 是提交入参。IdemKey 是客户端生成的幂等 key（可选）。
type SubmitInput struct {
	Data    map[string]any `json:"data"`
	IdemKey string         `json:"idemKey"`
}

// SubmitResult 是提交结果。Replayed = 这是对同一幂等 key 的重放，没有产生新记录。
type SubmitResult struct {
	ID       idgen.ID `json:"id"`
	Version  int      `json:"version"`
	Message  string   `json:"message"`
	Replayed bool     `json:"replayed"`
}

// Submit 提交一份填写。校验依据永远是"已发布版本"的定义，而不是草稿，也不信任客户端传来的任何版本号。
func (s *Service) Submit(ctx context.Context, actor Actor, id idgen.ID, in SubmitInput) (*SubmitResult, error) {
	if in.IdemKey != "" && !idemRe.MatchString(in.IdemKey) {
		return nil, bad("formbuilder.idem_key_invalid")
	}
	f, err := s.publishedForm(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	ver, err := s.store.GetVersion(ctx, id, f.PublishedVersion)
	if err != nil {
		return nil, err
	}
	def, err := decodeDefinition([]byte(ver.Definition))
	if err != nil {
		return nil, err
	}
	if dl := def.Settings.Deadline; dl != nil && !s.now().Before(*dl) {
		return nil, conflict("formbuilder.closed_deadline")
	}
	clean, ferrs := ValidateSubmission(def, in.Data)
	if len(ferrs) > 0 {
		return nil, fieldErrors(ctx, def, ferrs)
	}
	raw, err := json.Marshal(clean)
	if err != nil || len(raw) > MaxDataBytes {
		return nil, bad("formbuilder.submit_too_large", MaxDataBytes)
	}
	sid, err := s.newID()
	if err != nil {
		return nil, err
	}
	dedupe := "r:" + strconv.FormatInt(int64(sid), 10)
	switch {
	case !def.Settings.AllowRepeat:
		dedupe = "u:" + strconv.FormatInt(int64(actor.ID), 10)
	case in.IdemKey != "":
		dedupe = "k:" + strconv.FormatInt(int64(actor.ID), 10) + ":" + in.IdemKey
	}
	sub := &Submission{
		FormID: id, Version: ver.Version, UserID: actor.ID, Username: clip(actor.Username, 64),
		Data: string(raw), DedupeKey: dedupe, IdemKey: in.IdemKey,
	}
	sub.ID = sid
	err = s.store.InsertSubmission(ctx, sub, def.Settings.MaxSubmissions)
	switch {
	case err == nil:
		return &SubmitResult{ID: sub.ID, Version: ver.Version, Message: def.Settings.SuccessMessage}, nil
	case errors.Is(err, errStoreFull):
		return nil, conflict("formbuilder.quota_full")
	case errors.Is(err, errStoreNotPublished):
		return nil, conflict("formbuilder.not_open")
	case errors.Is(err, errStoreDuplicate):
		old, ferr := s.store.FindByDedupe(ctx, id, dedupe)
		if ferr == nil && in.IdemKey != "" && old.IdemKey == in.IdemKey && old.UserID == actor.ID {
			return &SubmitResult{ID: old.ID, Version: old.Version, Message: def.Settings.SuccessMessage, Replayed: true}, nil
		}
		return nil, conflict("formbuilder.duplicate")
	}
	return nil, err
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// fieldErrors 把字段错误合成一条可读的 400（最多列出 5 条，以字段标题而不是 key 展示）。
// 字段标题是用户数据，原样拼接；外层前缀、分隔符、截断提示、字段级原因（按请求语言翻译）与
// 字段名/错误的连接方式走字典。
func fieldErrors(ctx context.Context, def *Definition, errs []FieldError) error {
	titles := make(map[string]string, len(def.Fields))
	for _, f := range def.Fields {
		titles[f.Key] = f.Title
	}
	msg := i18n.T(ctx, "formbuilder.errors_prefix")
	for i, e := range errs {
		if i == 5 {
			msg += i18n.T(ctx, "formbuilder.errors_truncated", len(errs))
			break
		}
		name := titles[e.Key]
		if name == "" {
			name = e.Key
		}
		if i > 0 {
			msg += i18n.T(ctx, "formbuilder.errors_separator")
		}
		reason := i18n.T(ctx, e.MsgKey, e.Args...)
		if name == "" {
			msg += reason
			continue
		}
		msg += i18n.T(ctx, "formbuilder.field_error_join", name, reason)
	}
	return response.BadRequest("error.bad_request").WithMsg(msg)
}
