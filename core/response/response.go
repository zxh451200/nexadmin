// Package response 是统一响应格式，与前端 web/src/framework/request 约定一致：
//
//	成功  HTTP 200  {"code":0,"message":"ok","data":...}
//	失败  HTTP 4xx/5xx  {"code":<与 HTTP 状态相同>,"message":"...","data":null}
//	分页  data = {"list":[],"total":0,"page":1,"pageSize":10}
//
// ID 一律是字符串（见 pkg/idgen.ID），业务 handler 不用关心。
package response

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// Body 是所有接口的统一响应体。
type Body struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
	Reason  string `json:"reason,omitempty"` // 机器可识别原因，如 NOT_INITIALIZED
}

// PageData 是分页数据，字段名与前端 Page<T> 一致。
type PageData[T any] struct {
	List     []T   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}

// OK 返回成功。
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Body{Code: 0, Message: "ok", Data: data})
}

// Page 返回分页数据；list 为 nil 时输出 []。
func Page[T any](c *gin.Context, list []T, total int64, page, pageSize int) {
	if list == nil {
		list = []T{}
	}
	OK(c, PageData[T]{List: list, Total: total, Page: page, PageSize: pageSize})
}

// ParsePage 读取 page / pageSize 查询参数（默认 1 / 10，pageSize 上限 200）。
func ParsePage(c *gin.Context) (page, pageSize int) {
	page, _ = strconv.Atoi(c.Query("page"))
	pageSize, _ = strconv.Atoi(c.Query("pageSize"))
	if page < 1 {
		page = 1
	}
	if page > 1_000_000 {
		page = 1_000_000
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return page, pageSize
}

// Fail 返回失败并中止后续 handler。*Error 按其状态码输出；其他错误一律 500，
// 细节只进日志（通过 c.Error），不暴露给调用方。
func Fail(c *gin.Context, err error) {
	var e *Error
	if !errors.As(err, &e) {
		_ = c.Error(err)
		e = Internal()
	} else if e.Err != nil {
		_ = c.Error(e.Err)
	}
	c.AbortWithStatusJSON(e.Status, Body{
		Code:    e.Code,
		Message: e.message(c.Request.Context()),
		Reason:  e.Reason,
	})
}

// BindJSON 绑定并校验 JSON 请求体；失败时已写入 400 响应，返回 false。
func BindJSON(c *gin.Context, obj any) bool {
	if err := c.ShouldBindJSON(obj); err != nil {
		Fail(c, BadRequest("error.bad_request").Wrap(err))
		return false
	}
	return true
}

// BindQuery 绑定并校验查询参数；失败时已写入 400 响应，返回 false。
func BindQuery(c *gin.Context, obj any) bool {
	if err := c.ShouldBindQuery(obj); err != nil {
		Fail(c, BadRequest("error.bad_request").Wrap(err))
		return false
	}
	return true
}
