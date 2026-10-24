// Package openapi 是「开放平台」管理模块：个人访问令牌、开放应用（服务账号）、事件目录、Webhook 订阅与投递。
// 令牌/签名的鉴权中间件在 core/openapi；事件总线在 core/event。
package openapi

import (
	"encoding/json"
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 投递状态。
const (
	StatusPending = "pending" // 等待投递或等待重试
	StatusSuccess = "success"
	StatusFailed  = "failed" // 重试用尽或 Webhook 已失效
)

// Webhook 对应 sys_webhook。签名密钥用 core/secret 加密存储（签名需要原文）。
type Webhook struct {
	database.TenantModel
	Name      string `gorm:"column:name;size:100;not null"`
	URL       string `gorm:"column:url;size:500;not null"`
	Events    string `gorm:"column:events;type:text"` // JSON 数组
	SecretEnc string `gorm:"column:secret_enc;type:text;not null"`
	Enabled   bool   `gorm:"column:enabled"`
}

// TableName 实现 gorm.Tabler。
func (Webhook) TableName() string { return "sys_webhook" }

// EventList 返回订阅的事件名。
func (w *Webhook) EventList() []string { return decodeList(w.Events) }

// Delivery 对应 sys_webhook_delivery：一次事件对一个 Webhook 的投递记录（保留 30 天）。
// Payload 是发出去的 JSON 体（不含请求头和密钥）；ResponseBody 最多保存 2KB。
type Delivery struct {
	database.TenantModel
	WebhookID      idgen.ID   `gorm:"column:webhook_id;not null"`
	EventID        string     `gorm:"column:event_id;size:64;not null"`
	Event          string     `gorm:"column:event;size:64;not null"`
	Payload        string     `gorm:"column:payload;type:text"`
	Status         string     `gorm:"column:status;size:16;not null"`
	Attempts       int        `gorm:"column:attempts;not null"`
	NextAttemptAt  time.Time  `gorm:"column:next_attempt_at;not null"`
	ResponseStatus int        `gorm:"column:response_status;not null"`
	ResponseBody   string     `gorm:"column:response_body;type:text"`
	ErrorMsg       string     `gorm:"column:error_msg;size:500"`
	DurationMs     int        `gorm:"column:duration_ms;not null"`
	DeliveredAt    *time.Time `gorm:"column:delivered_at"`
}

// TableName 实现 gorm.Tabler。
func (Delivery) TableName() string { return "sys_webhook_delivery" }

func decodeList(s string) []string {
	var out []string
	if s != "" {
		_ = json.Unmarshal([]byte(s), &out)
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func encodeList(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// envelope 是投递的 JSON 体：{id,event,createdAt,tenantId,data}。
type envelope struct {
	ID        string          `json:"id"`
	Event     string          `json:"event"`
	CreatedAt time.Time       `json:"createdAt"`
	TenantID  idgen.ID        `json:"tenantId"`
	Data      json.RawMessage `json:"data"`
}

// ---- 管理接口的 DTO（字段名与前端契约一致） ----

type webhookDTO struct {
	ID        idgen.ID  `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	Secret    string    `json:"secret,omitempty"` // 仅创建/重置时返回一次
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
}

func toWebhookDTO(w *Webhook) webhookDTO {
	return webhookDTO{ID: w.ID, Name: w.Name, URL: w.URL, Events: w.EventList(), Enabled: w.Enabled, CreatedAt: w.CreatedAt}
}

type deliveryDTO struct {
	ID             idgen.ID   `json:"id"`
	WebhookID      idgen.ID   `json:"webhookId"`
	EventID        string     `json:"eventId"`
	Event          string     `json:"event"`
	Status         string     `json:"status"`
	Attempts       int        `json:"attempts"`
	ResponseStatus int        `json:"responseStatus"`
	ResponseBody   string     `json:"responseBody"`
	Error          string     `json:"error"`
	DurationMs     int        `json:"durationMs"`
	NextAttemptAt  *time.Time `json:"nextAttemptAt"`
	CreatedAt      time.Time  `json:"createdAt"`
	DeliveredAt    *time.Time `json:"deliveredAt"`
}

func toDeliveryDTO(d *Delivery) deliveryDTO {
	out := deliveryDTO{ID: d.ID, WebhookID: d.WebhookID, EventID: d.EventID, Event: d.Event, Status: d.Status, Attempts: d.Attempts,
		ResponseStatus: d.ResponseStatus, ResponseBody: d.ResponseBody, Error: d.ErrorMsg, DurationMs: d.DurationMs,
		CreatedAt: d.CreatedAt, DeliveredAt: d.DeliveredAt}
	if d.Status == StatusPending {
		t := d.NextAttemptAt
		out.NextAttemptAt = &t
	}
	return out
}
