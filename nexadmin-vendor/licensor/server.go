package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

//go:embed index.html
var indexHTML []byte

const (
	tokenCookie = "licensor_token"
	maxBody     = 64 << 10
)

type server struct {
	priv  ed25519.PrivateKey
	kid   string
	warns []string
	tok   [32]byte // 口令的 sha256，比较时两边等长，常量时间
	st    *store
	now   func() time.Time
}

func newServer(priv ed25519.PrivateKey, warns []string, token string, st *store) *server {
	return &server{
		priv: priv, warns: warns, st: st, now: time.Now,
		kid: keyID(priv.Public().(ed25519.PublicKey)),
		tok: sha256.Sum256([]byte(token)),
	}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("GET /api/info", s.auth(s.info))
	mux.HandleFunc("POST /api/issue", s.auth(s.issue))
	mux.HandleFunc("GET /api/records", s.auth(s.records))
	mux.HandleFunc("GET /api/export.csv", s.auth(s.exportCSV))
	mux.HandleFunc("POST /api/revoke", s.auth(s.revoke))
	mux.HandleFunc("GET /api/revoked.json", s.auth(s.exportRevoked))
	return mux
}

func (s *server) tokenOK(t string) bool {
	h := sha256.Sum256([]byte(t))
	return subtle.ConstantTimeCompare(h[:], s.tok[:]) == 1
}

func (s *server) authed(r *http.Request) bool {
	if t := r.Header.Get("X-Licensor-Token"); t != "" {
		return s.tokenOK(t)
	}
	if a := r.Header.Get("Authorization"); strings.HasPrefix(a, "Bearer ") {
		return s.tokenOK(strings.TrimPrefix(a, "Bearer "))
	}
	if c, err := r.Cookie(tokenCookie); err == nil {
		return s.tokenOK(c.Value)
	}
	return false
}

func (s *server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authed(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "需要访问口令"})
			return
		}
		h(w, r)
	}
}

func setHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; img-src data:; base-uri 'none'; form-action 'none'")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	setHeaders(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *server) index(w http.ResponseWriter, _ *http.Request) {
	setHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

// login 校验口令并写 HttpOnly + SameSite=Strict 的 cookie；要求自定义头，挡掉跨站表单提交。
func (s *server) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
	var in struct {
		Token string `json:"token"`
	}
	if r.Header.Get("X-Requested-With") != "licensor" || json.NewDecoder(r.Body).Decode(&in) != nil || !s.tokenOK(in.Token) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "口令错误"})
		return
	}
	http.SetCookie(w, &http.Cookie{Name: tokenCookie, Value: in.Token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil, MaxAge: 8 * 3600})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) info(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"kid": s.kid, "warnings": s.warns})
}

type issueReq struct {
	Plugin   string `json:"plugin"`
	Customer string `json:"customer"`
	Expires  string `json:"expires"`
	Domains  string `json:"domains"`
	Note     string `json:"note"`
}

// parseExpires 与 nexadmin cmd/nexadmin 的 sign 保持一致：RFC3339，或 YYYY-MM-DD（当天结束，UTC）。
func parseExpires(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("有效期格式应为 2027-01-01 或 RFC3339")
	}
	return t.Add(24*time.Hour - time.Second).UTC(), nil
}

// issueLicense 校验参数、签名并返回授权文本和记录（不落盘）。
func (s *server) issueLicense(in issueReq, now time.Time) ([]byte, Record, error) {
	in.Plugin, in.Customer, in.Note = strings.TrimSpace(in.Plugin), strings.TrimSpace(in.Customer), strings.TrimSpace(in.Note)
	if !validID(in.Plugin) {
		return nil, Record{}, fmt.Errorf("非法的插件 id %q", in.Plugin)
	}
	if in.Customer == "" || len([]rune(in.Customer)) > 100 {
		return nil, Record{}, fmt.Errorf("客户名必填且不超过 100 字")
	}
	if len([]rune(in.Note)) > 500 {
		return nil, Record{}, fmt.Errorf("备注不超过 500 字")
	}
	exp, err := parseExpires(strings.TrimSpace(in.Expires))
	if err != nil {
		return nil, Record{}, err
	}
	if !exp.After(now) {
		return nil, Record{}, fmt.Errorf("有效期必须晚于当前时间")
	}
	// 签发记录 ID 同时作为授权序列号写进载荷，吊销时靠它命中。
	var idb [6]byte
	if _, err := rand.Read(idb[:]); err != nil {
		return nil, Record{}, fmt.Errorf("生成序列号失败: %w", err)
	}
	id := hex.EncodeToString(idb[:])
	l := License{Plugin: in.Plugin, Customer: in.Customer, IssuedAt: now.UTC().Format(time.RFC3339), ExpiresAt: exp.Format(time.RFC3339), Serial: id}
	for _, d := range strings.FieldsFunc(in.Domains, func(r rune) bool { return r == ',' || r == '，' || r == '\n' || r == ' ' }) {
		if d = strings.TrimSpace(d); d != "" {
			l.Domains = append(l.Domains, d)
		}
	}
	sort.Strings(l.Domains)
	raw, err := signLicense(s.priv, l)
	if err != nil {
		return nil, Record{}, err
	}
	if _, err := verifyLicense(raw, l.Plugin, now, s.priv.Public().(ed25519.PublicKey)); err != nil {
		return nil, Record{}, fmt.Errorf("签发后自检失败: %w", err)
	}
	return raw, Record{ID: id, Serial: id, IssuedAt: l.IssuedAt, Plugin: l.Plugin, Customer: l.Customer,
		ExpiresAt: l.ExpiresAt, Domains: l.Domains, KID: s.kid, Note: in.Note}, nil
}

func (s *server) issue(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	var in issueReq
	if r.Header.Get("X-Requested-With") != "licensor" || json.NewDecoder(r.Body).Decode(&in) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	raw, rec, err := s.issueLicense(in, s.now())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.st.Add(rec); err != nil {
		log.Printf("写签发记录失败: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "写入签发记录失败，未返回授权"})
		return
	}
	log.Printf("已签发 plugin=%s customer=%q expires=%s kid=%s", rec.Plugin, rec.Customer, rec.ExpiresAt, rec.KID) // 不记录授权全文
	writeJSON(w, http.StatusOK, map[string]any{"license": string(raw) + "\n", "record": rec})
}

type recView struct {
	Record
	DaysLeft     int    `json:"days_left"`
	Expiring     bool   `json:"expiring"`
	Expired      bool   `json:"expired"`
	Revoked      bool   `json:"revoked"`
	RevokedAt    string `json:"revoked_at,omitempty"`
	RevokeReason string `json:"revoke_reason,omitempty"`
}

func (s *server) views() []recView {
	now := s.now()
	var out []recView
	revoked := s.st.RevokedMap()
	for _, r := range s.st.List() {
		d := r.DaysLeft(now)
		v := recView{Record: r, DaysLeft: d, Expiring: r.ExpiringSoon(now), Expired: d < 0}
		if e, ok := revoked[r.Serial]; ok && r.Serial != "" {
			v.Revoked, v.RevokedAt, v.RevokeReason = true, e.RevokedAt, e.Reason
		}
		out = append(out, v)
	}
	return out
}

func (s *server) records(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"records": s.views()})
}

// csvCell 防 CSV 公式注入：以 = + - @ 开头的单元格前加单引号。
func csvCell(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

func (s *server) exportCSV(w http.ResponseWriter, r *http.Request) {
	only := r.URL.Query().Get("expiring") == "1"
	setHeaders(w)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="issued.csv"`)
	_, _ = w.Write([]byte("\xef\xbb\xbf")) // BOM，Excel 正确识别 UTF-8
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"签发时间", "插件", "客户", "到期时间", "剩余天数", "域名", "kid", "备注", "序列号", "吊销时间", "吊销原因"})
	for _, v := range s.views() {
		if only && !v.Expiring {
			continue
		}
		_ = cw.Write([]string{v.IssuedAt, csvCell(v.Plugin), csvCell(v.Customer), v.ExpiresAt, fmt.Sprint(v.DaysLeft),
			csvCell(strings.Join(v.Domains, ";")), v.KID, csvCell(v.Note), csvCell(v.Serial), v.RevokedAt, csvCell(v.RevokeReason)})
	}
	cw.Flush()
}

type revokeReq struct {
	Serial string `json:"serial"`
	Reason string `json:"reason"`
}

// revoke 把一条已签发授权标记为吊销（与签发一样要求口令 + 自定义请求头）。吊销只写本地记录，
// 要让客户端生效，必须导出清单并随升级包分发给客户（/api/revoked.json）。
func (s *server) revoke(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	var in revokeReq
	if r.Header.Get("X-Requested-With") != "licensor" || json.NewDecoder(r.Body).Decode(&in) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	in.Serial, in.Reason = strings.TrimSpace(in.Serial), strings.TrimSpace(in.Reason)
	if in.Reason == "" || len([]rune(in.Reason)) > maxReasonRunes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("吊销原因必填且不超过 %d 字（会显示在客户的插件管理页）", maxReasonRunes)})
		return
	}
	e, rec, err := s.st.Revoke(in.Serial, in.Reason, s.now())
	switch {
	case errors.Is(err, errNoSuchSerial):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	case errors.Is(err, errAlreadyRevoked):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	case err != nil:
		log.Printf("写吊销记录失败: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "写入吊销记录失败"})
		return
	}
	log.Printf("已吊销 serial=%s plugin=%s customer=%q", e.Serial, rec.Plugin, rec.Customer)
	writeJSON(w, http.StatusOK, map[string]any{"revoked": e})
}

// exportRevoked 用当前私钥签名并导出全量吊销清单；客户项目里放到 config/license-revoked.json。
// 导出前自检（验签 + 类型）；还没有任何吊销记录时返回 404。
func (s *server) exportRevoked(w http.ResponseWriter, _ *http.Request) {
	d := s.st.Revoked()
	if len(d.Entries) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "还没有吊销记录"})
		return
	}
	raw, err := signRevocations(s.priv, d)
	if err == nil {
		_, err = verifyRevocations(raw, s.priv.Public().(ed25519.PublicKey))
	}
	if err != nil {
		log.Printf("导出吊销清单失败: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "签名吊销清单失败"})
		return
	}
	setHeaders(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="license-revoked.json"`)
	_, _ = w.Write(append(raw, '\n'))
}
