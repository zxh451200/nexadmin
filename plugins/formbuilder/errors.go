package formbuilder

import (
	"errors"
	"strings"

	"nexadmin/core/response"
)

func bad(msg string) *response.Error { return response.BadRequest("error.bad_request").WithMsg(msg) }

func conflict(msg string) *response.Error { return response.Conflict("error.bad_request").WithMsg(msg) }

func forbidden(msg string) *response.Error {
	return response.Forbidden("error.forbidden").WithMsg(msg)
}

var (
	errDefTooLarge = bad("表单定义过大")
	errDefInvalid  = bad("表单定义不是合法的 JSON")
	errNotFound    = response.NotFound("error.not_found")

	// 存储层语义错误（service 负责翻译成用户可见的响应）。
	errStoreFull         = errors.New("formbuilder: submission limit reached")
	errStoreDuplicate    = errors.New("formbuilder: duplicate submission")
	errStoreNotPublished = errors.New("formbuilder: form is not published")
	errStoreConflict     = errors.New("formbuilder: concurrent modification")
)

// isDuplicateKey 判断唯一键冲突。三种引擎的报错文案不同，这里不引入驱动包，按文案判断：
// MySQL/MariaDB "Duplicate entry ... (Error 1062)"，PostgreSQL "duplicate key value violates unique constraint (SQLSTATE 23505)"。
func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "duplicate entry") || strings.Contains(s, "error 1062") ||
		strings.Contains(s, "duplicate key") || strings.Contains(s, "23505") || strings.Contains(s, "unique constraint")
}
