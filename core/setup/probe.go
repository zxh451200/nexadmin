package setup

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	coreai "nexadmin/core/ai"
	"nexadmin/core/response"
	"nexadmin/core/storage"
)

// Probes accept deployment endpoints, including private networks, but never
// follow redirects with credentials. Responses contain only fixed codes.
func (r *Runner) probeGuard(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if _, err := os.Stat(MarkerPath(r.Cfg)); !os.IsNotExist(err) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	// Require the setup token even on loopback for outbound credentialed probes.
	if r.Token == "" || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Setup-Token")), []byte(r.Token)) != 1 {
		response.Fail(c, response.Forbidden("setup.token_invalid"))
		return
	}
	r.probeMu.Lock()
	if time.Now().Before(r.probeNext) {
		r.probeMu.Unlock()
		c.Header("Retry-After", "2")
		response.Fail(c, response.New(http.StatusTooManyRequests, "setup.probe_rate_limited"))
		return
	}
	r.probeNext = time.Now().Add(2 * time.Second)
	r.probeMu.Unlock()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	c.Next()
}

func bindProbe(c *gin.Context, req *optionalReq) bool {
	dec := json.NewDecoder(c.Request.Body)
	if err := dec.Decode(req); err != nil {
		response.Fail(c, response.BadRequest("setup.probe_invalid"))
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		response.Fail(c, response.BadRequest("setup.probe_invalid"))
		return false
	}
	return true
}

func probeClient() *http.Client {
	return &http.Client{
		Timeout: 8 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func probeURL(raw string, defaultHTTPS bool) (*url.URL, bool) {
	raw = strings.TrimSpace(raw)
	if defaultHTTPS && !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	return u, err == nil && (u.Scheme == "http" || u.Scheme == "https") &&
		u.Hostname() != "" && u.User == nil && u.RawQuery == "" &&
		!u.ForceQuery && u.Fragment == "" && u.Opaque == ""
}

func probeResult(c *gin.Context, ok bool, code, scope string) {
	response.OK(c, gin.H{"ok": ok, "code": code, "scope": scope})
}

func probeFailure(c *gin.Context, ctx context.Context, err error, scope string) {
	code := "connection"
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		code = "timeout"
	} else if ctx.Err() != nil {
		code = "cancelled"
	}
	// Deliberately omit err: URLs, upstream bodies and SDK errors may contain secrets.
	_ = err
	probeResult(c, false, code, scope)
}

func probeStatus(c *gin.Context, status int, scope string) {
	code := "upstream"
	switch {
	case scope == "ai" && (status == http.StatusNotFound || status == http.StatusMethodNotAllowed || status == http.StatusNotImplemented):
		code = "modelsUnsupported"
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		code = "denied"
	case status == http.StatusNotFound:
		code = "notFound"
	case status >= 300 && status < 400:
		code = "redirect"
	case status == http.StatusTooManyRequests:
		code = "upstreamLimited"
	}
	probeResult(c, false, code, scope)
}

func (r *Runner) testS3(c *gin.Context) {
	const scope = "s3"
	var req optionalReq
	if !bindProbe(c, &req) {
		return
	}
	u, valid := probeURL(req.S3Endpoint, true)
	if !valid || strings.Trim(u.Path, "/") != "" ||
		strings.ContainsAny(req.S3Bucket, "/\\?# \t\r\n") || req.S3Bucket == "." || req.S3Bucket == ".." {
		probeResult(c, false, "invalid", scope)
		return
	}
	client := probeClient()
	defer client.CloseIdleConnections()
	s3, err := storage.NewS3(storage.S3Config{
		Endpoint: u.String(), Bucket: strings.TrimSpace(req.S3Bucket),
		AccessKey: strings.TrimSpace(req.S3AccessKey), SecretKey: req.S3SecretKey,
		Region: strings.TrimSpace(req.S3Region), PathStyle: req.S3PathStyle, HTTPClient: client,
	})
	if err != nil {
		probeResult(c, false, "invalid", scope)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	err = s3.HeadBucket(ctx)
	if err != nil {
		var upstream *storage.S3Error
		if errors.As(err, &upstream) {
			probeStatus(c, upstream.Status, scope)
		} else {
			probeFailure(c, ctx, err, scope)
		}
		return
	}
	probeResult(c, true, "ok", scope)
}

func (r *Runner) testAI(c *gin.Context) {
	const scope = "ai"
	var req optionalReq
	if !bindProbe(c, &req) {
		return
	}
	cfg := (coreai.Config{Enabled: true, BaseURL: req.AIBaseURL, APIKey: req.AIAPIKey, Model: req.AIModel}).Normalize()
	u, valid := probeURL(cfg.BaseURL, false)
	if !valid || !cfg.Ready() {
		probeResult(c, false, "invalid", scope)
		return
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/models"
	u.RawPath = ""
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	out, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		probeResult(c, false, "invalid", scope)
		return
	}
	out.Header.Set("Accept", "application/json")
	if cfg.APIKey != "" {
		out.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	client := probeClient()
	defer client.CloseIdleConnections()
	res, err := client.Do(out)
	if err != nil {
		probeFailure(c, ctx, err, scope)
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		probeStatus(c, res.StatusCode, scope)
		return
	}
	// A login HTML page or arbitrary 200 is not evidence of an AI endpoint.
	body, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil {
		probeFailure(c, ctx, err, scope)
		return
	}
	var models struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if len(body) > 1<<20 || json.Unmarshal(body, &models) != nil || models.Data == nil {
		probeResult(c, false, "invalidResponse", scope)
		return
	}
	for _, model := range models.Data {
		if model.ID == cfg.Model {
			probeResult(c, true, "ok", scope)
			return
		}
	}
	probeResult(c, false, "modelNotListed", scope)
}
