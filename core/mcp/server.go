// Package mcp provides read tools and explicitly registered confirmed writes.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"

	"go.uber.org/zap"
	"nexadmin/core/apidoc"
	"nexadmin/core/auth"
	"nexadmin/core/cache"
)

const (
	protocolVersion  = "2025-03-26"
	maxRequestBytes  = 1 << 20
	maxResponseBytes = 1 << 20
)

// bodyReadTimeout 是读取请求体的总时限；包级变量仅为了让测试缩短等待（见 export_test.go）。
var bodyReadTimeout = 10 * time.Second

// readBody 以 maxRequestBytes 上限和 bodyReadTimeout 读取请求体。返回非 0 状态码表示应中止：
// 413 超限，408 超时 / 读取失败。优先用连接读截止时间；底层不支持时退回 goroutine + 定时器。
func readBody(c *gin.Context) ([]byte, int) {
	rc := http.NewResponseController(c.Writer)
	deadlineSet := rc.SetReadDeadline(time.Now().Add(bodyReadTimeout)) == nil
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBytes)
	type out struct {
		b   []byte
		err error
	}
	done := make(chan out, 1)
	go func() {
		b, err := io.ReadAll(c.Request.Body)
		done <- out{b, err}
	}()
	timer := time.NewTimer(bodyReadTimeout + time.Second)
	defer timer.Stop()
	select {
	case r := <-done:
		var tooLarge *http.MaxBytesError
		switch {
		case errors.As(r.err, &tooLarge):
			return nil, http.StatusRequestEntityTooLarge
		case r.err != nil:
			return nil, http.StatusRequestTimeout
		}
		if deadlineSet {
			// 只在读完后还原；出错 / 超时时保留已过期的截止时间，这样 net/http 在写错误响应前
			// 丢弃剩余 body 的读取会立刻失败，响应能马上发出而不是卡在对端永远不发的 body 上。
			_ = rc.SetReadDeadline(time.Time{})
		}
		return r.b, 0
	case <-timer.C:
		_ = c.Request.Body.Close()
		return nil, http.StatusRequestTimeout
	}
}

type dispatchKey struct{}

// Preserve cancellation and deadlines, but rebuild identity and tenant through middleware.
type dispatchContext struct {
	context.Context
	expected *identity
}

func (d dispatchContext) Value(key any) any {
	if _, ok := key.(dispatchKey); ok {
		if d.expected != nil {
			return d.expected
		}
		return true
	}
	return nil
}

type Server struct {
	engine   *gin.Engine
	doc      *apidoc.Doc
	cache    cache.Cache
	auth     *auth.Service
	log      *zap.Logger
	mu       sync.Mutex
	sessions map[string]*session
	active   chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
	closed   bool

	confirmLimit  int
	confirmWindow time.Duration
}

func New(engine *gin.Engine, doc *apidoc.Doc) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{engine: engine, doc: doc, sessions: make(map[string]*session),
		active: make(chan struct{}, maxConcurrentRequests), ctx: ctx, cancel: cancel}
}

func (s *Server) WithConfirmation(ch cache.Cache, svc *auth.Service, log *zap.Logger) *Server {
	s.cache, s.auth, s.log = ch, svc, log
	return s
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func rpcError(c *gin.Context, id json.RawMessage, code int, message string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	encoded, _ := json.Marshal(gin.H{
		"jsonrpc": "2.0", "id": id,
		"error": gin.H{"code": code, "message": message},
	})
	sendRPC(c, encoded)
}

func result(c *gin.Context, id json.RawMessage, value any) {
	encoded, err := json.Marshal(gin.H{"jsonrpc": "2.0", "id": id, "result": value})
	if err != nil || len(encoded) > maxResponseBytes {
		rpcError(c, id, -32603, "encoded MCP response exceeds 1 MiB limit")
		return
	}
	sendRPC(c, encoded)
}

func decode(raw []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("expected one JSON value")
	}
	return nil
}

func validID(id json.RawMessage) bool {
	if len(id) == 0 {
		return true
	}
	var value any
	d := json.NewDecoder(bytes.NewReader(id))
	d.UseNumber()
	if d.Decode(&value) != nil {
		return false
	}
	switch value.(type) {
	case string, json.Number:
		return true
	}
	return false
}

// Handle 由原 API 鉴权链和 mcp:list 权限保护；成功响应不使用业务 response 信封。
func (s *Server) Handle(c *gin.Context) {
	if c.Request.Context().Value(dispatchKey{}) != nil {
		rpcError(c, nil, -32600, "recursive MCP invocation is forbidden")
		return
	}
	if origin := c.GetHeader("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") ||
			u.Host != c.Request.Host || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
	}
	if version := c.GetHeader("MCP-Protocol-Version"); version != "" && version != protocolVersion {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	if !s.transport(c) {
		return
	}
	if c.Request.Method != http.MethodPost {
		return
	}
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" {
		c.AbortWithStatus(http.StatusUnsupportedMediaType)
		return
	}
	// 先限长并带读超时地读完请求体，再占并发槽：慢速发送 body（slowloris）不会占住槽位。
	body, status := readBody(c)
	if status != 0 {
		c.AbortWithStatus(status)
		return
	}
	release, ok := s.acquire(c)
	if !ok {
		return
	}
	defer release()
	if c.Request.Context().Err() != nil {
		c.AbortWithStatus(http.StatusRequestTimeout)
		return
	}
	if !json.Valid(body) {
		rpcError(c, nil, -32700, "parse error")
		return
	}
	var req request
	if decode(body, &req) != nil || req.JSONRPC != "2.0" || req.Method == "" || !validID(req.ID) {
		rpcError(c, nil, -32600, "invalid request; batches are not supported")
		return
	}
	if len(req.Params) > 0 && (len(bytes.TrimSpace(req.Params)) == 0 || bytes.TrimSpace(req.Params)[0] != '{') {
		if len(req.ID) == 0 {
			c.Status(http.StatusAccepted)
			return
		}
		rpcError(c, req.ID, -32602, "params must be an object")
		return
	}
	// JSON-RPC 通知永不产生响应体，包括未知通知。
	if len(req.ID) == 0 {
		c.Status(http.StatusAccepted)
		return
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string          `json:"protocolVersion"`
			Capabilities    json.RawMessage `json:"capabilities"`
			ClientInfo      json.RawMessage `json:"clientInfo"`
			Meta            json.RawMessage `json:"_meta"`
		}
		if decode(req.Params, &p) != nil || p.ProtocolVersion == "" {
			rpcError(c, req.ID, -32602, "protocolVersion, capabilities and clientInfo are required")
			return
		}
		var capabilities map[string]json.RawMessage
		var client struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		if json.Unmarshal(p.Capabilities, &capabilities) != nil || capabilities == nil ||
			json.Unmarshal(p.ClientInfo, &client) != nil || client.Name == "" || client.Version == "" {
			rpcError(c, req.ID, -32602, "invalid clientInfo or capabilities")
			return
		}
		if !s.initializeSession(c) {
			return
		}
		result(c, req.ID, gin.H{
			"protocolVersion": protocolVersion,
			"capabilities":    gin.H{"tools": gin.H{"listChanged": false}},
			"serverInfo":      gin.H{"name": "nexadmin", "version": "1.0.0"},
			"instructions":    "GET tools execute directly. Explicit write tools first return confirmationId and a browser URL; a human must reauthenticate with a local password and approve. Repeat the same tool with confirmationId only to execute. Original route authentication, tenant and permissions always apply.",
		})
	case "ping":
		result(c, req.ID, gin.H{})
	case "tools/list":
		var p struct {
			Cursor string          `json:"cursor"`
			Meta   json.RawMessage `json:"_meta"`
		}
		if len(req.Params) != 0 && (decode(req.Params, &p) != nil || p.Cursor != "") {
			rpcError(c, req.ID, -32602, "pagination cursors are not supported")
			return
		}
		tools := make([]any, 0)
		for _, tool := range s.doc.Tools() {
			tools = append(tools, gin.H{
				"name": tool.Name, "description": strings.TrimSpace(tool.Description + " (" + tool.Method + " " + tool.Path + ")"),
				"inputSchema": toolSchema(tool),
				"annotations": gin.H{"readOnlyHint": !tool.Write, "destructiveHint": false},
			})
		}
		encoded, err := json.Marshal(gin.H{"tools": tools})
		if err != nil || len(encoded) > maxResponseBytes {
			rpcError(c, req.ID, -32603, "tool directory exceeds response limit")
			return
		}
		result(c, req.ID, json.RawMessage(encoded))
	case "tools/call":
		s.call(c, req)
	default:
		rpcError(c, req.ID, -32601, "method not found")
	}
}

func inputSchema(route string) gin.H {
	properties := gin.H{}
	required := []string{}
	for _, segment := range strings.Split(route, "/") {
		if strings.HasPrefix(segment, ":") {
			name := strings.TrimPrefix(segment, ":")
			properties[name] = gin.H{"type": "string", "minLength": 1}
			required = append(required, name)
		}
	}
	schema := gin.H{
		"type": "object", "additionalProperties": false,
		"properties": gin.H{
			"path": gin.H{"type": "object", "properties": properties, "required": required, "additionalProperties": false},
			"query": gin.H{"type": "object", "additionalProperties": gin.H{"oneOf": []any{
				gin.H{"type": "string"}, gin.H{"type": "array", "items": gin.H{"type": "string"}},
			}}},
		},
	}
	if len(required) > 0 {
		schema["required"] = []string{"path"}
	}
	return schema
}

func (s *Server) call(c *gin.Context, req request) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
		Meta      json.RawMessage `json:"_meta"`
	}
	if decode(req.Params, &p) != nil || p.Name == "" {
		rpcError(c, req.ID, -32602, "name and valid arguments are required")
		return
	}
	var tool *apidoc.Tool
	for _, candidate := range s.doc.Tools() {
		if candidate.Name == p.Name {
			copy := candidate
			tool = &copy
			break
		}
	}
	if tool == nil {
		rpcError(c, req.ID, -32602, "unknown tool")
		return
	}
	if tool.Write {
		s.write(c, req, *tool, p.Arguments)
		return
	}
	var args struct {
		Path  map[string]string          `json:"path"`
		Query map[string]json.RawMessage `json:"query"`
	}
	if len(p.Arguments) == 0 {
		p.Arguments = json.RawMessage("{}")
	}
	if bytes.Equal(bytes.TrimSpace(p.Arguments), []byte("null")) || decode(p.Arguments, &args) != nil {
		rpcError(c, req.ID, -32602, "arguments only allow path and query; confirmation overrides are forbidden")
		return
	}
	target, err := targetURL(*tool, args.Path, args.Query)
	if err != nil {
		rpcError(c, req.ID, -32602, err.Error())
		return
	}
	// 外层 POST 的签名不能复用于 GET，也不能把已鉴权的 claims 注入子请求。
	if c.GetHeader("X-App-Key") != "" {
		toolError(c, req.ID, "application signatures cannot be reused for a different route; tools/call requires a bearer credential")
		return
	}
	s.dispatch(c, req, *tool, target, nil, nil)
}

func (s *Server) dispatch(c *gin.Context, req request, tool apidoc.Tool, target *url.URL, body []byte, expected *identity) {
	if c.Request.Context().Err() != nil {
		return
	}
	if expected == nil && c.GetHeader(sessionHeader) != "" {
		me, err := currentIdentity(c)
		if err != nil {
			toolError(c, req.ID, "session identity unavailable")
			return
		}
		expected = &me
	}
	ctx, cancel := context.WithTimeout(dispatchContext{Context: c.Request.Context(), expected: expected}, 15*time.Second)
	defer cancel()
	sub, err := http.NewRequestWithContext(ctx, tool.Method, target.String(), bytes.NewReader(body))
	if err != nil {
		rpcError(c, req.ID, -32602, "invalid route arguments")
		return
	}
	sub.Host = c.Request.Host
	sub.RemoteAddr = c.Request.RemoteAddr
	sub.TLS = c.Request.TLS
	// 仅继承外层请求头白名单。客户端不能通过 arguments 注入头、URL 或身份。
	for _, name := range []string{"Authorization", "X-Tenant-ID", "Accept-Language", "X-Forwarded-For", "X-Real-IP"} {
		if values := c.Request.Header.Values(name); len(values) > 0 {
			sub.Header[http.CanonicalHeaderKey(name)] = append([]string(nil), values...)
		}
	}
	sub.Header.Set("Accept", "application/json")
	if tool.Write {
		sub.Header.Set("Content-Type", "application/json")
	}
	w := &limitedWriter{header: make(http.Header), cancel: cancel}
	s.engine.ServeHTTP(w, sub)
	if w.streaming {
		toolError(c, req.ID, "streaming tool responses are not supported")
		return
	}
	if w.overflow {
		toolError(c, req.ID, "tool response exceeds 1 MiB limit")
		return
	}
	if ctx.Err() != nil {
		toolError(c, req.ID, "tool request deadline exceeded")
		return
	}
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	result(c, req.ID, gin.H{
		"content": []any{gin.H{"type": "text", "text": fmt.Sprintf("HTTP %d\n%s", status, strings.ToValidUTF8(w.body.String(), "\uFFFD"))}},
		"isError": status < 200 || status >= 300,
	})
}

func toolError(c *gin.Context, id json.RawMessage, message string) {
	result(c, id, gin.H{"content": []any{gin.H{"type": "text", "text": message}}, "isError": true})
}

func targetURL(tool apidoc.Tool, params map[string]string, query map[string]json.RawMessage) (*url.URL, error) {
	segments := strings.Split(tool.Path, "/")
	used := map[string]bool{}
	for i, segment := range segments {
		if !strings.HasPrefix(segment, ":") {
			continue
		}
		name := strings.TrimPrefix(segment, ":")
		value := params[name]
		if value == "" || value == "." || value == ".." ||
			strings.ContainsAny(value, "/\\%?#;") || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return nil, fmt.Errorf("invalid path parameter %s", name)
		}
		used[name] = true
		segments[i] = value
	}
	if len(used) != len(params) {
		return nil, errors.New("unknown path parameter")
	}
	path := strings.Join(segments, "/")
	if !strings.HasPrefix(path, apidoc.APIPrefix+"/") ||
		path == apidoc.APIPrefix+"/mcp" || strings.HasPrefix(path, apidoc.APIPrefix+"/mcp/") {
		return nil, errors.New("recursive or non-API target forbidden")
	}
	values := url.Values{}
	for key, raw := range query {
		var value string
		if json.Unmarshal(raw, &value) == nil && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			values.Add(key, value)
			continue
		}
		var list []string
		if json.Unmarshal(raw, &list) != nil || list == nil {
			return nil, fmt.Errorf("query %s must be a string or string array", key)
		}
		for _, value := range list {
			values.Add(key, value)
		}
	}
	return &url.URL{Path: path, RawQuery: values.Encode()}, nil
}

// 不缓存上限之外的响应，不转发 Set-Cookie、Location 或其他目标响应头，也不跟随重定向。
type limitedWriter struct {
	header    http.Header
	body      bytes.Buffer
	status    int
	overflow  bool
	streaming bool
	cancel    context.CancelFunc
}

func (w *limitedWriter) Header() http.Header { return w.header }
func (w *limitedWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		media, _, _ := mime.ParseMediaType(w.header.Get("Content-Type"))
		if media == "text/event-stream" {
			w.streaming = true
			w.cancel()
		}
	}
}
func (w *limitedWriter) Write(p []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	remaining := maxResponseBytes - w.body.Len()
	if len(p) > remaining {
		w.overflow = true
		w.cancel()
		_, _ = w.body.Write(p[:remaining])
		return remaining, errors.New("MCP response limit exceeded")
	}
	return w.body.Write(p)
}

func (w *limitedWriter) Flush() { w.WriteHeader(http.StatusOK) }
