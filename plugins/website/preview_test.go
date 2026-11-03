package website

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/response"
)

// 草稿预览令牌与页面复制：一个是公开端唯一的例外放行，一个是内容复用的主链路，
// 都值得钉住——签名错一格就是草稿泄露，去重漏一次就是 unique index 报错打到用户脸上。

func TestPagePreviewToken(t *testing.T) {
	key := []byte("unit-test-key")
	now := time.Unix(1_800_000_000, 0)
	token, err := signPagePreview(key, "about", now)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || !strings.Contains(token, ".") {
		t.Fatalf("token shape wrong: %q", token)
	}

	cases := []struct {
		name  string
		slug  string
		token string
		at    time.Time
		want  bool
	}{
		{"本人本人有效", "about", token, now, true},
		{"有效期内一直有效", "about", token, now.Add(previewTTL - time.Second), true},
		{"过期作废", "about", token, now.Add(previewTTL), false},
		{"换个 slug 不行", "contact", token, now, false},
		{"改过签名不行", "about", token[:len(token)-2] + "ff", now, false},
		{"缺签名段", "about", "123456789", now, false},
		{"过期时间不是数字", "about", "soon." + token[strings.Index(token, ".")+1:], now, false},
		{"空令牌", "about", "", now, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := verifyPagePreview(key, tc.slug, tc.token, tc.at); got != tc.want {
				t.Errorf("verifyPagePreview(%q) = %v, want %v", tc.token, got, tc.want)
			}
		})
	}

	// 换密钥签的令牌不认：预览密钥与运行时其他用途隔离在同一把 JWT 密钥上，换站即失效。
	other, err := signPagePreview([]byte("another-key"), "about", now)
	if err != nil {
		t.Fatal(err)
	}
	if verifyPagePreview(key, "about", other, now) {
		t.Error("token signed with another key must not verify")
	}

	// 未配置签名密钥：签发要报业务错误，校验一律拒绝（预览功能整体关闭）。
	if _, err := signPagePreview(nil, "about", now); err == nil {
		t.Error("sign without key must fail")
	} else {
		var be *response.Error
		if !errors.As(err, &be) || be.Key != "website.preview_unavailable" {
			t.Errorf("want website.preview_unavailable, got %v", err)
		} else if be.Status != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", be.Status)
		}
	}
	if verifyPagePreview(nil, "about", token, now) {
		t.Error("verify without key must reject")
	}
}

func TestPagePreviewKeyOfDeps(t *testing.T) {
	if previewKeyOf(nil) != nil {
		t.Error("nil deps must give nil key")
	}
}

func TestCopyPageSlugDedupe(t *testing.T) {
	srcRows := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"id", "slug", "title", "seo_desc", "content", "published", "i18n_json"}).
			AddRow(7, "about", "关于我们", "desc", "<p>正文</p>", true, "")
	}

	t.Run("无撞车直接 -copy", func(t *testing.T) {
		db, m := mockDB(t)
		m.ExpectQuery(`SELECT \* FROM .plg_website_page. WHERE id = \? ORDER BY .* LIMIT \?`).
			WithArgs(int64(7), 1).WillReturnRows(srcRows())
		m.ExpectQuery(`SELECT count\(\*\) FROM .plg_website_page. WHERE slug = \?`).
			WithArgs("about-copy").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		m.ExpectBegin()
		m.ExpectExec("INSERT INTO .plg_website_page.").WillReturnResult(sqlmock.NewResult(8, 1))
		m.ExpectCommit()

		row, err := NewService(db).CopyPage(context.Background(), 7)
		if err != nil {
			t.Fatal(err)
		}
		if row.Slug != "about-copy" {
			t.Errorf("slug = %q, want about-copy", row.Slug)
		}
		if row.Published {
			t.Error("copy must be a draft")
		}
		if row.Title != "关于我们" || row.Content != "<p>正文</p>" || row.I18nJSON != "" {
			t.Errorf("copy content mismatch: %+v", row)
		}
		if err := m.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("撞车递增 -copy-2", func(t *testing.T) {
		db, m := mockDB(t)
		m.ExpectQuery(`SELECT \* FROM .plg_website_page. WHERE id = \? ORDER BY .* LIMIT \?`).
			WithArgs(int64(7), 1).WillReturnRows(srcRows())
		m.ExpectQuery(`SELECT count\(\*\) FROM .plg_website_page. WHERE slug = \?`).
			WithArgs("about-copy").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		m.ExpectQuery(`SELECT count\(\*\) FROM .plg_website_page. WHERE slug = \?`).
			WithArgs("about-copy-2").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		m.ExpectBegin()
		m.ExpectExec("INSERT INTO .plg_website_page.").WillReturnResult(sqlmock.NewResult(9, 1))
		m.ExpectCommit()

		row, err := NewService(db).CopyPage(context.Background(), 7)
		if err != nil {
			t.Fatal(err)
		}
		if row.Slug != "about-copy-2" {
			t.Errorf("slug = %q, want about-copy-2", row.Slug)
		}
		if err := m.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}

// 接近列宽上限的 slug：拼上 -copy 再截回 64，且不因截断产生空串。
func TestCopyPageTruncatesLongSlug(t *testing.T) {
	base := strings.Repeat("a", 64)
	got := truncateSlug(base+"-copy", "page")
	if len(got) > 64 {
		t.Errorf("len = %d, want <= 64", len(got))
	}
	if got != strings.Repeat("a", 64) {
		t.Errorf("truncateSlug = %q", got)
	}
	if truncateSlug("", "page") != "page" {
		t.Errorf("empty slug must fall back, got %q", truncateSlug("", "page"))
	}
}
