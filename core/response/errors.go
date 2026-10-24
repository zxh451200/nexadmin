package response

import (
	"context"
	"fmt"
	"net/http"

	"nexadmin/core/i18n"
)

// Error 是业务错误。Status 即 HTTP 状态码，Code 与之相同（前端按 code 判断，423 = 账号锁定）。
// 文案用 i18n key，Fail 时按请求语言翻译；也可用 WithMsg 直接给文本。
type Error struct {
	Status int
	Code   int
	Reason string
	Key    string
	Args   []any
	Msg    string
	Err    error // 仅进日志
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Key, e.Err)
	}
	if e.Msg != "" {
		return e.Msg
	}
	return e.Key
}

func (e *Error) Unwrap() error { return e.Err }

func (e *Error) message(ctx context.Context) string {
	if e.Msg != "" {
		return e.Msg
	}
	return i18n.T(ctx, e.Key, e.Args...)
}

// Wrap 附带内部错误（只进日志，不返回给调用方）。
func (e *Error) Wrap(err error) *Error {
	c := *e
	c.Err = err
	return &c
}

// WithMsg 用固定文本覆盖 i18n 文案。
func (e *Error) WithMsg(msg string) *Error {
	c := *e
	c.Msg = msg
	return &c
}

// New 创建业务错误，key 是 i18n key。
func New(status int, key string, args ...any) *Error {
	return &Error{Status: status, Code: status, Key: key, Args: args}
}

func BadRequest(key string, args ...any) *Error   { return New(http.StatusBadRequest, key, args...) }
func Unauthorized(key string, args ...any) *Error { return New(http.StatusUnauthorized, key, args...) }
func Forbidden(key string, args ...any) *Error    { return New(http.StatusForbidden, key, args...) }
func NotFound(key string, args ...any) *Error     { return New(http.StatusNotFound, key, args...) }
func Conflict(key string, args ...any) *Error     { return New(http.StatusConflict, key, args...) }

// Internal 是通用 500。
func Internal() *Error { return New(http.StatusInternalServerError, "error.internal") }

// Unavailable 是 503，用于依赖的服务（如登录提供者）未启用。
func Unavailable(key string, args ...any) *Error {
	return New(http.StatusServiceUnavailable, key, args...)
}

// Locked 表示账号被锁定（423），message 为 "LOCKED:<剩余秒数>"，前端据此展示倒计时。
func Locked(seconds int) *Error {
	return New(423, "error.locked").WithMsg(fmt.Sprintf("LOCKED:%d", seconds))
}

// NotInitialized 是系统未初始化时业务 API 的统一响应：503 + reason NOT_INITIALIZED。
func NotInitialized() *Error {
	e := New(http.StatusServiceUnavailable, "error.not_initialized")
	e.Reason = "NOT_INITIALIZED"
	return e
}
