package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	coreai "nexadmin/core/ai"
	"nexadmin/core/auth"
	"nexadmin/core/tenant"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const maxConversations = 30

type conversation struct {
	ID        string    `json:"id" gorm:"primaryKey"`
	TenantID  int64     `json:"-"`
	UserID    int64     `json:"-"`
	Slot      int       `json:"-"`
	Name      string    `json:"name"`
	Messages  string    `json:"-"`
	Revision  uint64    `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (conversation) TableName() string { return "plg_assistant_conversation" }

func validConversationID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func conversationKeyID(key string) string {
	_, id, _ := strings.Cut(key, ":conversation:")
	return id
}

func (s *service) conversationScope(ctx context.Context) *gorm.DB {
	cl := auth.ClaimsFromContext(ctx)
	tid, ok := tenant.From(ctx)
	if s.db == nil || cl == nil || cl.UserID <= 0 || !ok || tid <= 0 {
		return nil
	}
	return s.db.WithContext(ctx).Model(&conversation{}).
		Where("tenant_id = ? AND user_id = ?", int64(tid), int64(cl.UserID))
}

func (s *service) ownedConversation(ctx context.Context, id string) (conversation, error) {
	var row conversation
	q := s.conversationScope(ctx)
	if q == nil {
		return row, errors.New("storage unavailable")
	}
	err := q.Where("id = ?", id).First(&row).Error
	return row, err
}

// Query selection is explicit and canonical; omitting it preserves legacy cache data.
func (s *service) requestIdentity(c *gin.Context) (coreai.Caller, string, error) {
	who, key, err := identity(c.Request.Context())
	if err != nil {
		return who, key, err
	}
	values, present := c.Request.URL.Query()["conversationId"]
	if !present {
		return who, key, nil
	}
	if len(values) != 1 || !validConversationID(values[0]) {
		return who, "", errors.New("invalid conversation")
	}
	if _, err := s.ownedConversation(c.Request.Context(), values[0]); err != nil {
		return who, "", errors.New("conversation unavailable")
	}
	return who, key + ":conversation:" + values[0], nil
}

func plainHistory(messages []coreai.Message) bool {
	if len(messages)%2 != 0 || len(messages) > 12 || historyBytes(messages) > maxHistory {
		return false
	}
	for i, m := range messages {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		if m.Role != role || len(m.ToolCalls) != 0 || m.ToolCallID != "" || !utf8.ValidString(m.Content) {
			return false
		}
	}
	return true
}

func (s *service) saveConversation(ctx context.Context, key string, revision uint64, messages []coreai.Message) error {
	if !plainHistory(messages) {
		return errors.New("invalid history")
	}
	q := s.conversationScope(ctx)
	if q == nil {
		return errors.New("storage unavailable")
	}
	data, err := json.Marshal(messages)
	if err != nil {
		return err
	}
	r := q.Where("id = ? AND revision = ?", conversationKeyID(key), revision).
		Updates(map[string]any{"messages": string(data), "revision": revision + 1, "updated_at": time.Now().UTC()})
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return errors.New("conversation changed")
	}
	return nil
}

func conversationName(c *gin.Context) (string, bool) {
	var in struct {
		Name string `json:"name"`
	}
	if !decode(c, &in) {
		return "", false
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 240 || utf8.RuneCountInString(name) > 80 || !utf8.ValidString(name) {
		return "", false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return name, true
}

func (s *service) listConversations(c *gin.Context) {
	if _, _, err := identity(c.Request.Context()); err != nil {
		reply(c, 403, "forbidden")
		return
	}
	q := s.conversationScope(c.Request.Context())
	if q == nil {
		reply(c, 503, "conversation storage unavailable")
		return
	}
	rows := []conversation{}
	if q.Select("id", "name", "created_at", "updated_at").Order("updated_at DESC, id ASC").Limit(maxConversations).Find(&rows).Error != nil {
		reply(c, 503, "conversation storage unavailable")
		return
	}
	reply(c, 200, rows)
}

func (s *service) createConversation(c *gin.Context) {
	_, key, err := identity(c.Request.Context())
	if err != nil {
		reply(c, 403, "forbidden")
		return
	}
	name, ok := conversationName(c)
	if !ok {
		reply(c, 400, "name must contain 1–80 characters (maximum 240 UTF-8 bytes)")
		return
	}
	// Serialize the bounded personal list across application nodes.
	lockKey := key + ":conversations"
	lockID, l, err := s.acquire(c.Request.Context(), lockKey)
	if err != nil {
		reply(c, 409, "conversation list busy")
		return
	}
	defer release(l, lockKey, lockID)
	q := s.conversationScope(c.Request.Context())
	var rows []conversation
	if q == nil || q.Select("slot").Limit(maxConversations).Find(&rows).Error != nil {
		reply(c, 503, "conversation storage unavailable")
		return
	}
	if len(rows) >= maxConversations {
		reply(c, 409, "maximum 30 conversations")
		return
	}
	used := [maxConversations]bool{}
	for _, row := range rows {
		if row.Slot >= 0 && row.Slot < maxConversations {
			used[row.Slot] = true
		}
	}
	slot := 0
	for used[slot] {
		slot++
	}
	id, err := newID()
	if err != nil {
		reply(c, 503, "conversation unavailable")
		return
	}
	cl := auth.ClaimsFromContext(c.Request.Context())
	tid, _ := tenant.From(c.Request.Context())
	row := conversation{ID: id, TenantID: int64(tid), UserID: int64(cl.UserID), Slot: slot, Name: name, Messages: "[]"}
	if s.db.WithContext(c.Request.Context()).Create(&row).Error != nil {
		reply(c, 503, "conversation storage unavailable")
		return
	}
	reply(c, 200, row)
}

func (s *service) renameConversation(c *gin.Context) { s.editConversation(c, false) }
func (s *service) deleteConversation(c *gin.Context) { s.editConversation(c, true) }

func (s *service) editConversation(c *gin.Context, deleting bool) {
	_, key, err := identity(c.Request.Context())
	if err != nil {
		reply(c, 403, "forbidden")
		return
	}
	id := c.Param("id")
	if !validConversationID(id) {
		reply(c, 400, "invalid conversation")
		return
	}
	if _, err := s.ownedConversation(c.Request.Context(), id); err != nil {
		reply(c, 404, "conversation unavailable")
		return
	}
	var name string
	if !deleting {
		var ok bool
		name, ok = conversationName(c)
		if !ok {
			reply(c, 400, "invalid conversation name")
			return
		}
	}
	key += ":conversation:" + id
	lockID, l, err := s.acquire(c.Request.Context(), key)
	if err != nil {
		reply(c, 409, "conversation busy")
		return
	}
	defer release(l, key, lockID)
	q := s.conversationScope(c.Request.Context()).Where("id = ?", id)
	var result *gorm.DB
	if deleting {
		// Delete transient resumable state before removing its durable owner.
		if s.cache.Del(c.Request.Context(), key+":resume") != nil || s.cache.Del(c.Request.Context(), key) != nil {
			reply(c, 503, "conversation unavailable")
			return
		}
		result = q.Delete(&conversation{})
	} else {
		result = q.Updates(map[string]any{"name": name, "updated_at": time.Now().UTC()})
	}
	if result.Error != nil || result.RowsAffected != 1 {
		reply(c, 503, "conversation unavailable")
		return
	}
	reply(c, 200, gin.H{"updated": true})
}
