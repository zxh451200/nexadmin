package openapi

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/middleware"
	"nexadmin/core/response"
)

const (
	DefaultRateWindowSec = 60
	MaxRateWindowSec     = 24 * 60 * 60
	MaxRateLimit         = 1_000_000
)

// ValidateRateLimit 校验凭证配额；0 表示不限流。
func ValidateRateLimit(limit, windowSec int) *response.Error {
	if limit < 0 || limit > MaxRateLimit {
		return response.BadRequest("openapi.rate_limit_invalid")
	}
	if windowSec < 1 || windowSec > MaxRateWindowSec {
		return response.BadRequest("openapi.rate_window_invalid")
	}
	return nil
}

func (a *authn) quota(c *gin.Context, kind string, id, tenantID int64, limit, windowSec int) bool {
	if limit <= 0 || a.Cache == nil {
		return true
	}
	if windowSec <= 0 {
		windowSec = DefaultRateWindowSec
	}
	window := time.Duration(windowSec) * time.Second
	key := "openapi-call:" + strconv.FormatInt(tenantID, 10) + ":" + kind + ":" + strconv.FormatInt(id, 10) + ":w" + strconv.Itoa(windowSec)
	limiter := middleware.NewLimiter(a.Cache, a.Log, "openapi-call", limit, window)
	res := limiter.HitRule(c.Request.Context(), key, limit, window)
	c.Header("X-RateLimit-Limit", strconv.Itoa(res.Limit))
	c.Header("X-RateLimit-Remaining", strconv.FormatInt(res.Remaining, 10))
	c.Header("X-RateLimit-Reset", strconv.Itoa(max(int((res.ResetAfter+time.Second-1)/time.Second), 1)))
	if res.Allowed {
		return true
	}
	setCallAuditFailure(c, "openapi.rate_limited")
	middleware.TooManyRequests(c, res.RetryAfter)
	return false
}
