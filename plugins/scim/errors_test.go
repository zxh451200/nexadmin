package scim

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// writeSCIMError 的映射必须有真实驱动文案的用例：GORM 没开 TranslateError，
// 生产上拿到的是 MySQL 1062 / PG 23505 的原文，而不是 gorm.ErrDuplicatedKey。
// 只测 gorm.ErrDuplicatedKey 的那条分支等于没测（它永远不会命中）。
func TestWriteSCIMErrorMapsDriverDuplicateKeyTo409(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantType   string
	}{
		{
			name:       "mysql 1062",
			err:        errors.New(`Error 1062 (23000): Duplicate entry '1-ext-7' for key 'plg_scim_group.uk_plg_scim_group_external'`),
			wantStatus: http.StatusConflict,
			wantType:   "uniqueness",
		},
		{
			name:       "postgres 23505",
			err:        errors.New(`duplicate key value violates unique constraint "uk_plg_scim_group_external" (SQLSTATE 23505)`),
			wantStatus: http.StatusConflict,
			wantType:   "uniqueness",
		},
		{
			name:       "gorm 翻译后的哨兵",
			err:        gorm.ErrDuplicatedKey,
			wantStatus: http.StatusConflict,
			wantType:   "uniqueness",
		},
		{
			name:       "无关错误仍是 500",
			err:        errors.New("context deadline exceeded"),
			wantStatus: http.StatusInternalServerError,
			wantType:   "",
		},
		{
			// 反向边界：连接串里带 "unique constraint" 字样的故障不能冒充 409。
			name:       "非唯一冲突的相似文案",
			err:        errors.New("dial tcp: lookup db: no such host"),
			wantStatus: http.StatusInternalServerError,
			wantType:   "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(rec)
			ctx.Request = httptest.NewRequest(http.MethodPatch, "/api/v1/plugins/scim/Groups/1", nil)
			writeSCIMError(ctx, tc.err)

			if rec.Code != tc.wantStatus {
				t.Fatalf("HTTP %d，想要 %d（body=%s）", rec.Code, tc.wantStatus, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("错误体不是 JSON：%v（%s）", err, rec.Body.String())
			}
			if got, _ := body["status"].(string); got != strconv.Itoa(tc.wantStatus) {
				t.Errorf("status 字段 = %q，想要 %d", got, tc.wantStatus)
			}
			gotType, _ := body["scimType"].(string)
			if gotType != tc.wantType {
				t.Errorf("scimType = %q，想要 %q", gotType, tc.wantType)
			}
			if ct := rec.Header().Get("Content-Type"); tc.wantType != "" && !strings.HasPrefix(ct, "application/scim+json") {
				t.Errorf("Content-Type = %q，SCIM 错误要用 application/scim+json", ct)
			}
		})
	}
}
