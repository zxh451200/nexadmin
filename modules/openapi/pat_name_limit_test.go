package openapi

// POST /profile/tokens 的名称闸按码点计（utf8.RuneCountInString），与前端表单的 100 同一口径。
// 按字节计的话 100 个汉字（300 字节）会被误拒，所以边界用汉字打：
// 恰好 100 个汉字必须放行（请求会走到配额检查，返回 409），第 101 个汉字必须被拒（400，且不碰库）。
// 把 >100 改成 >99，或把 utf8.RuneCountInString 换成 len，这条都会红。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/config"
	"nexadmin/core/i18n"
	"nexadmin/pkg/idgen"
)

// tokenCreatePost 用给定的 name 打一次创建接口，返回状态码与响应里的文案。
func tokenCreatePost(t *testing.T, h *handler, uid idgen.ID, name string) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) { auth.SetClaims(c, &auth.Claims{UserID: uid}) })
	e.POST("/profile/tokens", h.tokenCreate)
	body, err := json.Marshal(map[string]string{"name": name, "scope": "read"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/profile/tokens", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	var out struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是 JSON：%s", w.Body.String())
	}
	return w.Code, out.Message
}

func TestTokenCreateNameGateCountsCodepoints(t *testing.T) {
	const uid = idgen.ID(42)
	b, err := i18n.New([]config.Language{{Code: "zh-CN", Name: "简体中文"}}, "zh-CN", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	i18n.SetDefault(b)
	t.Cleanup(func() { i18n.SetDefault(nil) })

	ctx := context.Background()
	wantNameErr := i18n.T(ctx, "openapi.name_required")
	wantQuotaErr := i18n.T(ctx, "openapi.token_limit")
	if wantNameErr == "openapi.name_required" || wantQuotaErr == "openapi.token_limit" {
		t.Fatal("两条词条没加载出来，断言会退化成空比较")
	}
	if wantNameErr == wantQuotaErr {
		t.Fatal("名称闸与配额的文案撞了，用例分不开这两种拒绝")
	}

	t.Run("恰好 100 个汉字放行到配额检查", func(t *testing.T) {
		h, mock := batchMockDB(t)
		// 配额先返回「已满」：能走到这一步就说明名称闸放行了，且不必再模拟写入。
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_pat` WHERE user_id = \\?").
			WithArgs(int64(uid)).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(maxTokensPerUser))

		status, msg := tokenCreatePost(t, h, uid, strings.Repeat("令", 100))
		if status != http.StatusConflict || msg != wantQuotaErr {
			t.Errorf("status = %d message = %q，想要 409 + %q（名称放行后撞配额）", status, msg, wantQuotaErr)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("名称闸放行后应查一次配额：%v", err)
		}
	})

	t.Run("第 101 个汉字被拒且不碰库", func(t *testing.T) {
		h, mock := batchMockDB(t)
		status, msg := tokenCreatePost(t, h, uid, strings.Repeat("令", 101))
		if status != http.StatusBadRequest {
			t.Errorf("status = %d，想要 400", status)
		}
		if msg != wantNameErr {
			t.Errorf("文案 = %q，想要名称闸的那句 %q（不是 scope/过期时间的错）", msg, wantNameErr)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("被名称闸拦下时不该有库调用：%v", err)
		}
	})

	t.Run("空名称与非空超长走同一句", func(t *testing.T) {
		h, mock := batchMockDB(t)
		if status, msg := tokenCreatePost(t, h, uid, "   "); status != http.StatusBadRequest || msg != wantNameErr {
			t.Errorf("空名称 status = %d message = %q，想要 400 + %q", status, msg, wantNameErr)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("空名称不该有库调用：%v", err)
		}
	})
}
