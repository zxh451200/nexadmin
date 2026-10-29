package file

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/core/storage"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 这里是 tus 1.0.0 协议的最小服务端实现（creation + termination 扩展），
// 因为 tusd 不在离线依赖缓存里，所以自己实现。前端 Uppy tus 插件按标准协议即可续传。
//
//	POST   /files/upload       创建上传（Upload-Length + Upload-Metadata）→ 201 + Location
//	HEAD   /files/upload/:id   查询已收字节数（断点续传先问这个）
//	PATCH  /files/upload/:id   追加数据（Upload-Offset 必须等于已收字节数）
//	DELETE /files/upload/:id   取消上传
const (
	tusVersion    = "1.0.0"
	sniffLen      = 512
	sessionMaxAge = 24 * time.Hour
)

// ourKey 匹配本模块生成的对象键：<租户>/<yyyymmdd>/<雪花ID>[.ext]。
var ourKey = regexp.MustCompile(`^\d+/\d{8}/\d+(\.[a-z0-9]{1,9})?$`)

// session 是一次上传的状态，持久化在 data/uploads/tmp/<id>.json。
type session struct {
	ID         string `json:"id"`
	TenantID   string `json:"tenantId"`
	UserID     string `json:"userId"`
	Username   string `json:"username"`
	Length     int64  `json:"length"`
	Offset     int64  `json:"offset"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Kind       string `json:"kind"`
	Visibility string `json:"visibility"`
	// VariantOf 非 0 表示这是原文件 id 的派生副本（如 WebP），可见性继承原文件。
	VariantOf idgen.ID  `json:"variantOf"`
	Verified  bool      `json:"verified"`
	Reserved  bool      `json:"reserved"`
	Completed bool      `json:"completed"`
	CreatedAt time.Time `json:"createdAt"`
}

type tusHandler struct {
	svc         *Service
	locks       map[string]*uploadLock
	locksMu     sync.Mutex
	lastCleanup time.Time
	cleanupMu   sync.Mutex
}

type uploadLock struct {
	sync.Mutex
	refs int
}

func (h *tusHandler) lock(id string) func() {
	h.locksMu.Lock()
	if h.locks == nil {
		h.locks = map[string]*uploadLock{}
	}
	m := h.locks[id]
	if m == nil {
		m = &uploadLock{}
		h.locks[id] = m
	}
	m.refs++
	h.locksMu.Unlock()
	m.Lock()
	return func() {
		m.Unlock()
		h.locksMu.Lock()
		m.refs--
		if m.refs == 0 {
			delete(h.locks, id)
		}
		h.locksMu.Unlock()
	}
}

func (h *tusHandler) saveSession(s *session) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(h.svc.store.infoPath(s.ID), b, 0o600)
}

// loadSession 读取上传状态，并校验归属（租户 + 用户），不属于当前用户一律当作不存在。
func (h *tusHandler) loadSession(c *gin.Context, id string) (*session, error) {
	if _, err := idgen.Parse(id); err != nil { // 同时避免路径穿越
		return nil, response.NotFound("error.not_found")
	}
	b, err := os.ReadFile(h.svc.store.infoPath(id))
	if err != nil {
		return nil, response.NotFound("error.not_found")
	}
	var s session
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, response.NotFound("error.not_found")
	}
	cl := auth.ClaimsFrom(c)
	tid, _ := tenant.From(c.Request.Context())
	if cl == nil || s.UserID != cl.UserID.String() || s.TenantID != tid.String() {
		return nil, response.NotFound("error.not_found")
	}
	if s.Length <= 0 || s.Offset < 0 || s.Offset > s.Length || time.Since(s.CreatedAt) >= sessionMaxAge {
		return nil, response.NotFound("error.not_found")
	}
	return &s, nil
}

func (h *tusHandler) drop(id string) error {
	b, err := os.ReadFile(h.svc.store.infoPath(id))
	if err == nil {
		var s session
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if err := h.svc.releaseStorage(&s); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	_ = os.Remove(h.svc.store.tmpPath(id))
	_ = os.Remove(h.svc.store.infoPath(id))
	return nil
}

// cleanup 每小时顺带清理一次超过 24 小时未完成的上传。
func (h *tusHandler) cleanup() {
	h.cleanupMu.Lock()
	if time.Since(h.lastCleanup) < time.Hour {
		h.cleanupMu.Unlock()
		return
	}
	h.lastCleanup = time.Now()
	h.cleanupMu.Unlock()
	// S3：中止本模块遗留的未完成分片上传（只动符合 <租户>/<yyyymmdd>/<ID>.<ext> 的键，不碰 bucket 里别的东西）
	if sa, ok := h.svc.store.blobs.(storage.StaleAborter); ok {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		_, _ = sa.AbortStale(ctx, sessionMaxAge, func(key string) bool { return ourKey.MatchString(key) })
		cancel()
	}
	dir := filepath.Join(h.svc.store.root, "tmp")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > sessionMaxAge {
			id := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			if _, err := idgen.Parse(id); err != nil {
				continue
			}
			unlock := h.lock(id)
			_ = h.drop(id)
			unlock()
		}
	}
}

// tusHeaders 在所有 tus 响应上带版本头；请求必须带 Tus-Resumable。
func tusHeaders(c *gin.Context) bool {
	c.Header("Tus-Resumable", tusVersion)
	if c.GetHeader("Tus-Resumable") != tusVersion {
		c.Header("Tus-Version", tusVersion)
		response.Fail(c, response.New(http.StatusPreconditionFailed, "error.bad_request").WithMsg("不支持的 Tus-Resumable 版本"))
		return false
	}
	return true
}

// parseMetadata 解析 Upload-Metadata："key base64value,key2 base64value2"。
func parseMetadata(h string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(h, ",") {
		kv := strings.SplitN(strings.TrimSpace(pair), " ", 2)
		if kv[0] == "" {
			continue
		}
		if len(kv) == 1 {
			out[kv[0]] = ""
			continue
		}
		v, err := base64.StdEncoding.DecodeString(kv[1])
		if err != nil {
			v, _ = base64.RawStdEncoding.DecodeString(kv[1])
		}
		out[kv[0]] = string(v)
	}
	return out
}

func limitFor(l Limits, kind string) float64 {
	switch kind {
	case "image":
		return l.MaxImageMB
	case "video":
		return l.MaxVideoMB
	}
	return l.MaxFileMB
}

// Create 处理 POST：创建前校验大小上限和类型白名单，超限直接拒绝，不浪费流量。
func (h *tusHandler) Create(c *gin.Context) {
	if !tusHeaders(c) {
		return
	}
	ctx := c.Request.Context()
	length, err := strconv.ParseInt(c.GetHeader("Upload-Length"), 10, 64)
	if err != nil || length <= 0 {
		response.Fail(c, badRequest("缺少有效的 Upload-Length"))
		return
	}
	meta := parseMetadata(c.GetHeader("Upload-Metadata"))
	name := cleanName(meta["filename"])
	declared := meta["filetype"]
	kind := meta["kind"]
	if kind != "image" && kind != "video" && kind != "file" {
		kind = map[string]string{"image": "image", "video": "video"}[classify(declared, name)]
		if kind == "" {
			kind = "file"
		}
	}
	lim := h.svc.currentLimits(ctx)
	if mb := limitFor(lim, kind); mb > 0 && float64(length) > mb*1024*1024 {
		response.Fail(c, response.New(http.StatusRequestEntityTooLarge, "error.bad_request").WithMsg(fmt.Sprintf("文件超过大小上限 %g MB", mb)))
		return
	}
	if !typeAllowed(lim.AllowedTypes, declared, name) {
		response.Fail(c, response.New(http.StatusUnsupportedMediaType, "error.bad_request").WithMsg("不允许上传该类型的文件"))
		return
	}
	cl := auth.ClaimsFrom(c)
	tid, ok := tenant.From(ctx)
	if cl == nil || !ok {
		response.Fail(c, response.Unauthorized("error.unauthorized"))
		return
	}
	// 派生文件（WebP 副本）：只能挂在当前租户自己的图片下，且本身必须是图片。
	var variantOf idgen.ID
	if raw := strings.TrimSpace(meta["variantof"]); raw != "" {
		var parent File
		if err := h.svc.db.WithContext(ctx).First(&parent, "id = ?", raw).Error; err != nil {
			response.Fail(c, response.NotFound("error.not_found"))
			return
		}
		if parent.Category != "image" || parent.VariantOf != 0 || kind != "image" {
			response.Fail(c, badRequest("只有图片可以生成派生副本"))
			return
		}
		variantOf = parent.ID
	}
	id, err := idgen.Next()
	if err != nil {
		response.Fail(c, err)
		return
	}
	s := &session{
		ID: id.String(), TenantID: tid.String(), UserID: cl.UserID.String(), Username: cl.Username,
		Length: length, Name: name, Type: declared, Kind: kind, Visibility: normVisibility(meta["visibility"]),
		VariantOf: variantOf, CreatedAt: time.Now().UTC(),
	}
	if err := h.svc.reserveStorage(ctx, s); err != nil {
		response.Fail(c, err)
		return
	}
	created := false
	defer func() {
		if !created {
			_ = h.svc.releaseStorage(s)
		}
	}()
	f, err := os.OpenFile(h.svc.store.tmpPath(s.ID), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		response.Fail(c, err)
		return
	}
	_ = f.Close()
	if err := h.saveSession(s); err != nil {
		h.drop(s.ID)
		response.Fail(c, err)
		return
	}
	created = true
	go h.cleanup()
	c.Header("Location", apiBase+"/files/upload/"+s.ID)
	c.Status(http.StatusCreated)
}

// Head 返回已收字节数，断点续传时客户端先调用它。
func (h *tusHandler) Head(c *gin.Context) {
	if !tusHeaders(c) {
		return
	}
	s, err := h.loadSession(c, c.Param("id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.Header("Upload-Offset", strconv.FormatInt(s.Offset, 10))
	c.Header("Upload-Length", strconv.FormatInt(s.Length, 10))
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
}

// Terminate 取消上传并删除临时文件。
func (h *tusHandler) Terminate(c *gin.Context) {
	if !tusHeaders(c) {
		return
	}
	s, err := h.loadSession(c, c.Param("id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	unlock := h.lock(s.ID)
	defer unlock()
	if _, err := h.loadSession(c, s.ID); err != nil {
		response.Fail(c, err)
		return
	}
	if err := h.drop(s.ID); err != nil {
		response.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Patch 追加数据；写满后做真实文件头校验、计算哈希、入库。
func (h *tusHandler) Patch(c *gin.Context) {
	if !tusHeaders(c) {
		return
	}
	if c.GetHeader("Content-Type") != "application/offset+octet-stream" {
		response.Fail(c, response.New(http.StatusUnsupportedMediaType, "error.bad_request").WithMsg("Content-Type 必须是 application/offset+octet-stream"))
		return
	}
	s, err := h.loadSession(c, c.Param("id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	unlock := h.lock(s.ID)
	defer unlock()
	if s, err = h.loadSession(c, s.ID); err != nil { // 拿到锁后重读，拿到最新偏移
		response.Fail(c, err)
		return
	}
	if s.Completed {
		if err := h.drop(s.ID); err != nil {
			response.Fail(c, err)
			return
		}
		c.Header("Upload-Offset", strconv.FormatInt(s.Length, 10))
		c.Status(http.StatusNoContent)
		return
	}
	if err := h.svc.reserveStorage(c.Request.Context(), s); err != nil {
		response.Fail(c, err)
		return
	}
	if err := h.saveSession(s); err != nil {
		_ = h.svc.releaseStorage(s)
		response.Fail(c, err)
		return
	}
	offset, perr := strconv.ParseInt(c.GetHeader("Upload-Offset"), 10, 64)
	if perr != nil || offset != s.Offset {
		response.Fail(c, response.Conflict("error.bad_request").WithMsg("Upload-Offset 与服务端已收字节数不一致"))
		return
	}
	f, err := os.OpenFile(h.svc.store.tmpPath(s.ID), os.O_RDWR, 0o600)
	if err != nil {
		response.Fail(c, response.NotFound("error.not_found").Wrap(err))
		return
	}
	defer f.Close()
	if _, err := f.Seek(s.Offset, io.SeekStart); err != nil {
		response.Fail(c, err)
		return
	}
	remaining := s.Length - s.Offset
	written, copyErr := io.Copy(f, io.LimitReader(c.Request.Body, remaining))
	s.Offset += written
	tooMuch := false
	if copyErr == nil && s.Offset == s.Length {
		var one [1]byte
		if n, _ := c.Request.Body.Read(one[:]); n > 0 {
			tooMuch = true
		}
	}
	if err := h.saveSession(s); err != nil {
		response.Fail(c, err)
		return
	}
	if copyErr != nil { // 客户端中途断开：已写入的部分保留，下次 HEAD 后续传
		return
	}
	if tooMuch {
		_ = h.drop(s.ID)
		response.Fail(c, response.New(http.StatusRequestEntityTooLarge, "error.bad_request").WithMsg("上传数据超过声明的长度"))
		return
	}
	// 够 512 字节就先验头，类型不对尽早拒绝
	if !s.Verified && s.Offset >= min(sniffLen, s.Length) {
		if err := h.verify(s, f); err != nil {
			h.drop(s.ID)
			response.Fail(c, response.New(http.StatusUnsupportedMediaType, "error.bad_request").WithMsg(err.Error()))
			return
		}
		_ = h.saveSession(s)
	}
	if s.Offset == s.Length {
		if err := h.finish(c, s, f); err != nil {
			if !s.Completed {
				_ = h.drop(s.ID)
			}
			response.Fail(c, err)
			return
		}
	}
	c.Header("Upload-Offset", strconv.FormatInt(s.Offset, 10))
	c.Status(http.StatusNoContent)
}

// verify 读文件头做真实类型校验，通过后记录 MIME。
func (h *tusHandler) verify(s *session, f *os.File) error {
	head := make([]byte, min(int64(sniffLen), s.Length))
	if _, err := f.ReadAt(head, 0); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	mt, err := checkContent(s.Kind, s.Type, s.Name, head)
	if err != nil {
		return err
	}
	s.Type, s.Verified = mt, true
	return nil
}

// finish 上传完成：校验、哈希、移到正式目录、写 sys_file。
func (h *tusHandler) finish(c *gin.Context, s *session, f *os.File) error {
	if time.Since(s.CreatedAt) >= sessionMaxAge {
		return badRequest("上传已过期，请重新创建上传")
	}
	if !s.Verified {
		if err := h.verify(s, f); err != nil {
			h.drop(s.ID)
			return response.New(http.StatusUnsupportedMediaType, "error.bad_request").WithMsg(err.Error())
		}
	}
	sum := sha256.New()
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := io.Copy(sum, f); err != nil {
		return err
	}
	_ = f.Close()

	id, _ := idgen.Parse(s.ID)
	tid, _ := idgen.Parse(s.TenantID)
	rel := finalRel(s.TenantID, s.ID, s.Name, time.Now())
	ctx := c.Request.Context()
	category := classify(s.Type, s.Name)
	visibility := s.Visibility
	if s.VariantOf != 0 {
		// 副本的可见性跟随原文件，客户端传什么都不算数；在落盘前解析，失败时临时文件仍可续传。
		var parent File
		if err := h.svc.db.WithContext(ctx).First(&parent, "id = ?", s.VariantOf).Error; err != nil {
			h.drop(s.ID)
			return err
		}
		visibility = parent.Visibility
	}
	hash := hex.EncodeToString(sum.Sum(nil))
	tmp := h.svc.store.tmpPath(s.ID)
	blobs := h.svc.store.blobs
	// 本地存储直接改名（零拷贝）；S3 把本地临时文件一次性交给驱动（超过阈值自动走分片，失败自动 Abort）。
	adopter, adopted := blobs.(storage.Adopter)
	if adopted {
		if err := adopter.Adopt(ctx, rel, tmp); err != nil {
			return err
		}
	} else {
		src, err := os.Open(tmp)
		if err != nil {
			return err
		}
		err = blobs.Put(ctx, rel, src, s.Length, putOptions(s.Name, s.Type, category, hash))
		_ = src.Close()
		if err != nil { // 临时文件和会话都还在，客户端重试 PATCH（长度 0）即可重新上传
			return err
		}
	}
	rec := File{
		Name: s.Name, StoragePath: rel, MimeType: s.Type, Category: category, Size: s.Length,
		Hash: hash, Visibility: visibility, Token: randomToken(), Uploader: s.Username, VariantOf: s.VariantOf,
	}
	rec.ID, rec.TenantID = id, tid
	if err := h.svc.db.WithContext(ctx).Create(&rec).Error; err != nil {
		// 入库失败：本地把文件放回临时区；S3 删掉刚传的对象。两种情况客户端都可重试。
		if adopted {
			if p, ok := blobs.(interface{ Path(string) (string, error) }); ok {
				if from, perr := p.Path(rel); perr == nil {
					_ = os.Rename(from, tmp)
				}
			}
		} else {
			dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			_ = blobs.Delete(dctx, rel)
			cancel()
		}
		return err
	}
	tenant.StorageMeter.Add(tid, s.Length)
	s.Completed = true
	// 保留可重试的完成状态；释放失败时不能重新入库或再次扣减其它上传的预占。
	if err := h.saveSession(s); err != nil {
		return err
	}
	return h.drop(s.ID)
}
