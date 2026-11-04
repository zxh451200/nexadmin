package openapi

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/event"
	coreoa "nexadmin/core/openapi"
	"nexadmin/core/response"
	"nexadmin/core/secret"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

type hookForm struct {
	Name    string   `json:"name"`
	URL     string   `json:"url"`
	Events  []string `json:"events"`
	Enabled *bool    `json:"enabled"`
}

func (f *hookForm) normalize() *response.Error {
	f.Name = strings.TrimSpace(f.Name)
	if f.Name == "" || utf8.RuneCountInString(f.Name) > 100 {
		return response.BadRequest("openapi.name_required")
	}
	f.URL = strings.TrimSpace(f.URL)
	if utf8.RuneCountInString(f.URL) > 500 {
		return response.BadRequest("openapi.url_too_long")
	}
	if _, err := validateURLStatic(f.URL); err != nil {
		return response.BadRequest("openapi.url_invalid").Wrap(err)
	}
	seen := map[string]bool{}
	evs := []string{}
	for _, e := range f.Events {
		e = strings.TrimSpace(e)
		if !event.Known(e) {
			return response.BadRequest("openapi.unknown_event", e)
		}
		if !seen[e] {
			seen[e] = true
			evs = append(evs, e)
		}
	}
	if len(evs) == 0 {
		return response.BadRequest("openapi.event_required")
	}
	f.Events = evs
	return nil
}

func newWebhookSecret() string { return "whsec_" + coreoa.RandString(32) }

// GET /openapi/webhooks?keyword=&page=&pageSize=
func (h *handler) hookList(c *gin.Context) {
	page, size := response.ParsePage(c)
	if c.Query("pageSize") == "" {
		size = 200
	}
	q := h.db.WithContext(c.Request.Context()).Model(&Webhook{})
	if kw := strings.TrimSpace(c.Query("keyword")); kw != "" {
		q = q.Where("LOWER(name) LIKE LOWER(?) OR LOWER(url) LIKE LOWER(?)", "%"+kw+"%", "%"+kw+"%")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		response.Fail(c, err)
		return
	}
	var rows []Webhook
	if err := q.Order("created_at DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		response.Fail(c, err)
		return
	}
	out := make([]webhookDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toWebhookDTO(&rows[i]))
	}
	response.Page(c, out, total, page, size)
}

// POST /openapi/webhooks，响应里带一次性明文 secret。
func (h *handler) hookCreate(c *gin.Context) {
	var f hookForm
	if !response.BindJSON(c, &f) {
		return
	}
	if e := f.normalize(); e != nil {
		response.Fail(c, e)
		return
	}
	plain := newWebhookSecret()
	enc, err := secret.Encrypt(plain)
	if err != nil {
		response.Fail(c, err)
		return
	}
	w := Webhook{Name: f.Name, URL: f.URL, Events: encodeList(f.Events), SecretEnc: enc, Enabled: f.Enabled == nil || *f.Enabled}
	if err := h.db.WithContext(c.Request.Context()).Create(&w).Error; err != nil {
		response.Fail(c, err)
		return
	}
	d := toWebhookDTO(&w)
	d.Secret = plain
	response.OK(c, d)
}

func (h *handler) findHook(c *gin.Context, id idgen.ID) (*Webhook, bool) {
	var w Webhook
	err := h.db.WithContext(c.Request.Context()).Where("id = ?", int64(id)).Take(&w).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Fail(c, response.NotFound("error.not_found"))
		return nil, false
	}
	if err != nil {
		response.Fail(c, err)
		return nil, false
	}
	return &w, true
}

// PUT /openapi/webhooks/:id（不改密钥）
func (h *handler) hookUpdate(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var f hookForm
	if !response.BindJSON(c, &f) {
		return
	}
	if e := f.normalize(); e != nil {
		response.Fail(c, e)
		return
	}
	w, ok := h.findHook(c, id)
	if !ok {
		return
	}
	w.Name, w.URL, w.Events = f.Name, f.URL, encodeList(f.Events)
	if f.Enabled != nil {
		w.Enabled = *f.Enabled
	}
	if err := h.db.WithContext(c.Request.Context()).Save(w).Error; err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, toWebhookDTO(w))
}

// DELETE /openapi/webhooks/:id（连同它的投递记录）
func (h *handler) hookRemove(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	w, ok := h.findHook(c, id)
	if !ok {
		return
	}
	err := h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("webhook_id = ?", int64(w.ID)).Delete(&Delivery{}).Error; err != nil {
			return err
		}
		return tx.Delete(w).Error
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// DELETE /openapi/webhooks?ids=1,2,3 —— 批量删除 Webhook 订阅，权限码复用单条删除的 openapi:edit。
//
// 清理动作原样照搬 hookRemove，并且同样整体事务化：先删这些订阅的投递记录（否则 sys_webhook_delivery
// 里留下一堆指向已删订阅的悬挂行，worker 也只会把它们当成失效投递），再删订阅本身；任一步失败整批回滚，
// 不出现「投递记录没了、订阅还在」的半截状态。
// 两条语句都显式带 tenant_id（同 modules/sso 的批量删除）：越权 id 既不删订阅、也不清别租户的投递记录，
// 实删条数按 Webhook 的 RowsAffected 如实返回，越权/不存在的 id 不计入。
func (h *handler) hookRemoveMany(c *gin.Context) {
	ids, err := response.ParseIDsQuery(c)
	if err != nil {
		response.Fail(c, err) // 空 ids / 任一项非法一律 400：绝不退化成「不带条件动整张表」
		return
	}
	tid, ok := tenant.From(c.Request.Context())
	if !ok {
		response.Fail(c, database.ErrNoTenant) // 认不出租户就一条都不动
		return
	}
	raw := idValues(ids)
	var deleted int64
	err = h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("tenant_id = ? AND webhook_id IN ?", int64(tid), raw).Delete(&Delivery{}).Error; err != nil {
			return err
		}
		res := tx.Where("tenant_id = ? AND id IN ?", int64(tid), raw).Delete(&Webhook{})
		deleted = res.RowsAffected
		return res.Error
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	if deleted == 0 {
		response.Fail(c, response.NotFound("error.not_found")) // 与单条删除同口径
		return
	}
	response.OK(c, map[string]int64{"deleted": deleted})
}

// PUT /openapi/webhooks/:id/enabled {enabled}
func (h *handler) hookSetEnabled(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var b struct {
		Enabled *bool `json:"enabled"`
	}
	if !response.BindJSON(c, &b) {
		return
	}
	if b.Enabled == nil {
		response.Fail(c, response.BadRequest("openapi.enabled_required"))
		return
	}
	w, ok := h.findHook(c, id)
	if !ok {
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Model(w).Update("enabled", *b.Enabled).Error; err != nil {
		response.Fail(c, err)
		return
	}
	w.Enabled = *b.Enabled
	response.OK(c, toWebhookDTO(w))
}

// POST /openapi/webhooks/:id/reset-secret：新密钥明文只返回这一次。
func (h *handler) hookResetSecret(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	w, ok := h.findHook(c, id)
	if !ok {
		return
	}
	plain := newWebhookSecret()
	enc, err := secret.Encrypt(plain)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Model(w).Update("secret_enc", enc).Error; err != nil {
		response.Fail(c, err)
		return
	}
	d := toWebhookDTO(w)
	d.Secret = plain
	response.OK(c, d)
}

type testResult struct {
	Success    bool   `json:"success"`
	StatusCode int    `json:"statusCode"`
	Body       string `json:"body"`
	Error      string `json:"error"`
	DurationMs int    `json:"durationMs"`
}

// POST /openapi/webhooks/:id/test：同步发送一个 ping 事件并返回接收端的结果（不写投递记录，也不受 enabled 限制）。
func (h *handler) hookTest(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	w, ok := h.findHook(c, id)
	if !ok {
		return
	}
	plain, err := secret.Decrypt(w.SecretEnc)
	if err != nil {
		response.Fail(c, err)
		return
	}
	body, err := json.Marshal(envelope{ID: "evt_ping_" + coreoa.RandString(12), Event: "ping", CreatedAt: time.Now().UTC(), TenantID: w.TenantID,
		Data: json.RawMessage(`{"message":"ping from NexAdmin"}`)})
	if err != nil {
		response.Fail(c, err)
		return
	}
	res := h.snd.send(c.Request.Context(), w.URL, plain, "ping", "dlv_ping_"+coreoa.RandString(12), body)
	response.OK(c, testResult{Success: res.ok(), StatusCode: res.Status, Body: res.Body, Error: res.Err, DurationMs: int(res.Duration / time.Millisecond)})
}

// GET /openapi/deliveries?webhookId=&status=success|failed|pending&page=&pageSize=
func (h *handler) deliveryList(c *gin.Context) {
	page, size := response.ParsePage(c)
	q := h.db.WithContext(c.Request.Context()).Model(&Delivery{})
	if v := strings.TrimSpace(c.Query("webhookId")); v != "" {
		id, err := idgen.Parse(v)
		if err != nil {
			response.Fail(c, response.BadRequest("openapi.webhook_id_invalid"))
			return
		}
		q = q.Where("webhook_id = ?", int64(id))
	}
	if st := strings.TrimSpace(c.Query("status")); st != "" {
		if st != StatusSuccess && st != StatusFailed && st != StatusPending {
			response.Fail(c, response.BadRequest("openapi.delivery_status_invalid"))
			return
		}
		q = q.Where("status = ?", st)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		response.Fail(c, err)
		return
	}
	var rows []Delivery
	if err := q.Order("created_at DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		response.Fail(c, err)
		return
	}
	out := make([]deliveryDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toDeliveryDTO(&rows[i]))
	}
	response.Page(c, out, total, page, size)
}

// POST /openapi/deliveries/:id/retry：把失败的投递重新排队（重置尝试次数，立即投递）。
func (h *handler) deliveryRetry(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	var d Delivery
	if err := h.db.WithContext(ctx).Where("id = ?", int64(id)).Take(&d).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Fail(c, response.NotFound("error.not_found"))
			return
		}
		response.Fail(c, err)
		return
	}
	if d.Status != StatusFailed {
		response.Fail(c, response.Conflict("openapi.retry_only_failed"))
		return
	}
	now := time.Now().UTC()
	res := h.db.WithContext(ctx).Model(&Delivery{}).Where("id = ? AND status = ?", int64(id), StatusFailed).
		Updates(map[string]any{"status": StatusPending, "attempts": 0, "next_attempt_at": now, "error_msg": ""})
	if res.Error != nil {
		response.Fail(c, res.Error)
		return
	}
	if res.RowsAffected == 0 {
		response.Fail(c, response.Conflict("openapi.delivery_state_changed"))
		return
	}
	d.Status, d.Attempts, d.NextAttemptAt, d.ErrorMsg = StatusPending, 0, now, ""
	h.wk.wake()
	response.OK(c, toDeliveryDTO(&d))
}
