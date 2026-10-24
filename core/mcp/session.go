package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/cache"
)

const (
	sessionHeader         = "Mcp-Session-Id"
	sessionTTL            = 15 * time.Minute
	maxSessions           = 1024
	maxConcurrentRequests = 64
	maxSessionRequests    = 4
	sseResponseKey        = "mcp.sseResponse"
	sessionContextKey     = "mcp.session"
	sessionPollInterval   = 250 * time.Millisecond
	sessionCacheTimeout   = time.Second
)

// Metadata and four request slots live in the configured cache. maxSessions
// bounds tracked sessions per instance; maxConcurrentRequests is also per instance.
// Neither activity nor credential refresh extends the original fixed deadline.
type session struct {
	id      string
	raw     string
	owner   identity
	expires time.Time
	ctx     context.Context
	cancel  context.CancelFunc
	timer   *time.Timer
}

type sessionRecord struct {
	ID        string    `json:"id"`
	Owner     identity  `json:"owner"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func sessionKey(id string) string { return cache.GlobalKey("mcp", "session", id) }

func (s *Server) sessionLocker() (cache.Locker, error) {
	locker, ok := cache.As[cache.Locker](s.cache)
	if !ok {
		return nil, errors.New("MCP sessions require an atomic cache")
	}
	return locker, nil
}

func (s *Server) readSession(ctx context.Context, id string) (sessionRecord, string, error) {
	var record sessionRecord
	if _, err := s.sessionLocker(); err != nil {
		return record, "", err
	}
	ctx, cancel := context.WithTimeout(ctx, sessionCacheTimeout)
	defer cancel()
	raw, err := s.cache.Get(ctx, sessionKey(id))
	if err != nil {
		return record, "", err
	}
	if json.Unmarshal([]byte(raw), &record) != nil || record.ID != id ||
		!time.Now().Before(record.ExpiresAt) {
		return record, "", cache.ErrNotFound
	}
	return record, raw, nil
}

func (s *Server) checkSessionSlot(ctx context.Context, v *session, key, token string) error {
	ctx, cancel := context.WithTimeout(ctx, sessionCacheTimeout)
	defer cancel()
	_, raw, err := s.readSession(ctx, v.id)
	if err != nil {
		return err
	}
	if raw != v.raw || v.ctx.Err() != nil {
		return cache.ErrNotFound
	}
	held, err := s.cache.Get(ctx, key)
	if err != nil {
		return err
	}
	if held != token {
		return cache.ErrNotFound
	}
	return nil
}

// Caller holds mu. Idle entries have only a fixed expiry timer, never a poller.
func (s *Server) trackSessionLocked(record sessionRecord, raw string) *session {
	if s.closed {
		return nil
	}
	now := time.Now()
	for id, v := range s.sessions {
		if !now.Before(v.expires) {
			s.removeSessionLocked(id, v)
		}
	}
	if v := s.sessions[record.ID]; v != nil {
		if v.raw == raw {
			return v
		}
		s.removeSessionLocked(record.ID, v)
	}
	if len(s.sessions) >= maxSessions || !now.Before(record.ExpiresAt) {
		return nil
	}
	ctx, cancel := context.WithDeadline(s.ctx, record.ExpiresAt)
	v := &session{id: record.ID, raw: raw, owner: record.Owner,
		expires: record.ExpiresAt, ctx: ctx, cancel: cancel}
	s.sessions[v.id] = v
	v.timer = time.AfterFunc(time.Until(v.expires), func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.removeSessionLocked(v.id, v)
	})
	return v
}

func (s *Server) invalidateSession(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v := s.sessions[id]; v != nil {
		s.removeSessionLocked(id, v)
	}
}

func (s *Server) removeSessionLocked(id string, v *session) {
	if s.sessions[id] == v {
		delete(s.sessions, id)
		v.cancel()
		if v.timer != nil {
			v.timer.Stop()
		}
	}
}

// Close also cancels in-flight requests without waiting for route handlers that
// ignore context cancellation.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.cancel()
	for id, v := range s.sessions {
		s.removeSessionLocked(id, v)
	}
}

func credentialDeadline(c *gin.Context) time.Time {
	cl := auth.ClaimsFrom(c)
	if cl == nil || cl.ExpiresAt == nil {
		return time.Time{}
	}
	return cl.ExpiresAt.Time
}

func (s *Server) initializeSession(c *gin.Context) bool {
	if c.GetHeader(sessionHeader) != "" {
		c.AbortWithStatus(http.StatusBadRequest)
		return false
	}
	owner, err := currentIdentity(c)
	expiry := credentialDeadline(c)
	now := time.Now()
	if err != nil || !expiry.After(now) {
		c.AbortWithStatus(http.StatusForbidden)
		return false
	}
	if limit := now.Add(sessionTTL); expiry.After(limit) {
		expiry = limit
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return false
	}
	id := hex.EncodeToString(random)
	locker, err := s.sessionLocker()
	if err != nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, v := range s.sessions {
		if !now.Before(v.expires) {
			s.removeSessionLocked(key, v)
		}
	}
	if s.closed || len(s.sessions) >= maxSessions {
		c.Header("Retry-After", "1")
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return false
	}
	if _, exists := s.sessions[id]; exists {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return false
	}
	record := sessionRecord{ID: id, Owner: owner, ExpiresAt: expiry}
	encoded, err := json.Marshal(record)
	if err != nil || time.Until(expiry) <= 0 {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return false
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), sessionCacheTimeout)
	ok, err := locker.SetNX(ctx, sessionKey(id), string(encoded), time.Until(expiry))
	cancel()
	if err != nil || !ok {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return false
	}
	if s.trackSessionLocked(record, string(encoded)) == nil {
		// An unreturned session is inaccessible and expires at its original deadline.
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return false
	}
	c.Header(sessionHeader, id)
	return true
}

// transport runs behind the original authentication and permission middleware.
// A session is never an authentication credential.
func (s *Server) transport(c *gin.Context) bool {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Writer.Header().Add("Vary", "Accept")
	values, present := c.Request.Header[http.CanonicalHeaderKey(sessionHeader)]
	if present {
		if len(values) != 1 || len(values[0]) != 64 {
			c.AbortWithStatus(http.StatusNotFound)
			return false
		}
		if _, err := hex.DecodeString(values[0]); err != nil {
			c.AbortWithStatus(http.StatusNotFound)
			return false
		}
		me, err := currentIdentity(c)
		if err != nil {
			c.AbortWithStatus(http.StatusNotFound)
			return false
		}
		record, raw, err := s.readSession(c.Request.Context(), values[0])
		if err != nil {
			s.invalidateSession(values[0])
			status := http.StatusServiceUnavailable
			if errors.Is(err, cache.ErrNotFound) {
				status = http.StatusNotFound
			}
			c.AbortWithStatus(status)
			return false
		}
		if me != record.Owner {
			c.AbortWithStatus(http.StatusNotFound)
			return false
		}
		if c.Request.Method == http.MethodDelete {
			locker, _ := s.sessionLocker()
			ctx, cancel := context.WithTimeout(c.Request.Context(), sessionCacheTimeout)
			deleted, err := locker.DelIfEqual(ctx, sessionKey(values[0]), raw)
			cancel()
			// Even an ambiguous deletion must cancel local work; never acknowledge
			// successful cancellation when the shared store did not confirm it.
			s.invalidateSession(values[0])
			if err != nil {
				c.AbortWithStatus(http.StatusServiceUnavailable)
				return false
			}
			if !deleted {
				c.AbortWithStatus(http.StatusNotFound)
				return false
			}
			c.Status(http.StatusNoContent)
			return false
		}
		s.mu.Lock()
		v := s.trackSessionLocked(record, raw)
		s.mu.Unlock()
		if v == nil {
			c.Header("Retry-After", "1")
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return false
		}
		c.Set(sessionContextKey, v)
	} else if c.Request.Method == http.MethodDelete {
		c.AbortWithStatus(http.StatusBadRequest)
		return false
	}
	if c.Request.Method != http.MethodPost {
		// No unsolicited notifications or replay store are implemented.
		c.Header("Allow", "POST, DELETE")
		c.AbortWithStatus(http.StatusMethodNotAllowed)
		return false
	}
	accept := c.GetHeader("Accept")
	jsonOK, sseOK := accept == "", false
	for _, item := range strings.Split(accept, ",") {
		media, params, err := mime.ParseMediaType(strings.TrimSpace(item))
		if err != nil {
			continue
		}
		if q, exists := params["q"]; exists {
			quality, err := strconv.ParseFloat(q, 64)
			if err != nil || !(quality > 0 && quality <= 1) {
				continue
			}
		}
		switch media {
		case "application/json", "application/*", "*/*":
			jsonOK = true
		case "text/event-stream":
			sseOK = true
		}
	}
	if !jsonOK && !sseOK {
		c.AbortWithStatus(http.StatusNotAcceptable)
		return false
	}
	c.Set(sseResponseKey, sseOK)
	return true
}

func (s *Server) acquire(c *gin.Context) (func(), bool) {
	if s.ctx.Err() != nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return nil, false
	}
	select {
	case s.active <- struct{}{}:
	default:
		c.Header("Retry-After", "1")
		c.AbortWithStatus(http.StatusTooManyRequests)
		return nil, false
	}
	var v *session
	if value, ok := c.Get(sessionContextKey); ok {
		v = value.(*session)
	}
	deadline := time.Now().Add(30 * time.Second)
	if expiry := credentialDeadline(c); !expiry.IsZero() && expiry.Before(deadline) {
		deadline = expiry
	}
	if v != nil && v.expires.Before(deadline) {
		deadline = v.expires
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), deadline)
	stopServer := context.AfterFunc(s.ctx, cancel)
	stopSession := func() bool { return false }
	var slotKey, token string
	var locker cache.Locker
	var monitorDone chan struct{}
	cleanup := func() {
		stopServer()
		stopSession()
		cancel()
		if monitorDone != nil {
			<-monitorDone
		}
		if slotKey != "" {
			// Keep a failed release occupied until the fixed session expiry.
			// Do not release on request cancellation: a handler may still run.
			releaseCtx, releaseCancel := context.WithTimeout(context.Background(), sessionCacheTimeout)
			_, _ = locker.DelIfEqual(releaseCtx, slotKey, token)
			releaseCancel()
		}
		<-s.active
	}
	if v != nil {
		stopSession = context.AfterFunc(v.ctx, cancel)
		if v.ctx.Err() != nil {
			cancel()
		}
		var err error
		locker, err = s.sessionLocker()
		if err != nil {
			cleanup()
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return nil, false
		}
		random := make([]byte, 32)
		if _, err = rand.Read(random); err != nil {
			cleanup()
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return nil, false
		}
		token = hex.EncodeToString(random)
		for i := 0; i < maxSessionRequests; i++ {
			ttl := time.Until(v.expires)
			if ttl <= 0 || ctx.Err() != nil {
				cleanup()
				c.AbortWithStatus(http.StatusNotFound)
				return nil, false
			}
			key := cache.GlobalKey("mcp", "session", v.id, "slot", strconv.Itoa(i))
			lockCtx, lockCancel := context.WithTimeout(ctx, sessionCacheTimeout)
			ok, lockErr := locker.SetNX(lockCtx, key, token, ttl)
			lockCancel()
			if lockErr != nil {
				// SetNX may have committed despite its error. Leave that slot
				// to expire rather than risk admitting work without a lease.
				s.invalidateSession(v.id)
				cleanup()
				c.AbortWithStatus(http.StatusServiceUnavailable)
				return nil, false
			}
			if ok {
				slotKey = key
				break
			}
		}
		if slotKey == "" {
			cleanup()
			c.Header("Retry-After", "1")
			c.AbortWithStatus(http.StatusTooManyRequests)
			return nil, false
		}
		// Recheck after acquiring the slot to close the lookup/DELETE race.
		err = s.checkSessionSlot(ctx, v, slotKey, token)
		if err != nil {
			s.invalidateSession(v.id)
			cleanup()
			status := http.StatusNotFound
			if !errors.Is(err, cache.ErrNotFound) {
				status = http.StatusServiceUnavailable
			}
			c.AbortWithStatus(status)
			return nil, false
		}
		// At most maxConcurrentRequests pollers exist per instance, and only
		// for admitted requests. Missing metadata or ANY read failure cancels
		// all local work for this session. Polling never updates its TTL.
		monitorDone = make(chan struct{})
		go func() {
			defer close(monitorDone)
			ticker := time.NewTicker(sessionPollInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := s.checkSessionSlot(ctx, v, slotKey, token); err != nil {
						if ctx.Err() != nil {
							return
						}
						s.invalidateSession(v.id)
						cancel()
						return
					}
				}
			}
		}()
	}
	c.Request = c.Request.WithContext(ctx)
	if ctx.Err() != nil {
		cleanup()
		c.AbortWithStatus(http.StatusNotFound)
		return nil, false
	}
	return cleanup, true
}

func sendRPC(c *gin.Context, encoded []byte) {
	if c.Request.Context().Err() != nil {
		return
	}
	if value, ok := c.Get(sseResponseKey); ok && value == true {
		// Only the final JSON-RPC message is available. No fabricated progress,
		// resumable event IDs, or forwarding of upstream tool SSE.
		c.Header("X-Accel-Buffering", "no")
		c.Data(http.StatusOK, "text/event-stream", append(append([]byte("event: message\ndata: "), encoded...), '\n', '\n'))
		c.Writer.Flush()
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", encoded)
}
