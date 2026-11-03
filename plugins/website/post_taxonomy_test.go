package website

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 新闻的批量操作、自动摘要与定时发布是这次对标补齐的主链路；
// 纯函数在这里钉死，落库语义由集成测试（TestIT*）覆盖。

func TestPostPreviewScopeIsolation(t *testing.T) {
	key := []byte("unit-test-key")
	now := time.Unix(1_800_000_000, 0)
	post, err := signPostPreview(key, "launch", now)
	if err != nil {
		t.Fatal(err)
	}
	page, err := signPagePreview(key, "launch", now)
	if err != nil {
		t.Fatal(err)
	}

	// 页面与新闻的 slug 是两张表各自唯一，同一个 slug 两边都能存在：
	// 作用域不同，令牌不能把新闻草稿的预览权带到页面预览（或反过来）。
	if verifyPagePreview(key, "launch", post, now) {
		t.Error("post token must not verify as page preview")
	}
	if verifyPostPreview(key, "launch", page, now) {
		t.Error("page token must not verify as post preview")
	}
	if !verifyPostPreview(key, "launch", post, now) {
		t.Error("post token must verify for its own slug")
	}
}

func TestSetPostsStatusValidation(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db)

	if _, err := s.SetPostsStatus(context.Background(), nil, "published"); err == nil {
		t.Error("empty ids must fail")
	} else {
		var be *response.Error
		if !errors.As(err, &be) || be.Key != "website.post_ids_empty" {
			t.Errorf("want website.post_ids_empty, got %v", err)
		}
	}
	if _, err := s.SetPostsStatus(context.Background(), []idgen.ID{1}, "someday"); err == nil {
		t.Error("unknown status must fail")
	} else {
		var be *response.Error
		if !errors.As(err, &be) || be.Key != "website.post_status_invalid" {
			t.Errorf("want website.post_status_invalid, got %v", err)
		} else if be.Status != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", be.Status)
		}
	}
	// 校验失败在碰库之前就该拒绝：不能有任何 SQL 往来。
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSummaryFromHTML(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"剥掉标签压空白", "<p>你好，</p><p>世界</p>", "你好， 世界"},
		{"还原实体", "<p>a &amp; b &lt;tag&gt;</p>", "a & b <tag>"},
		{"空内容给空串", "<p></p>  <br/>", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := summaryFromHTML(tc.in, summaryMaxRunes); got != tc.want {
				t.Errorf("summaryFromHTML = %q, want %q", got, tc.want)
			}
		})
	}

	long := "<p>" + strings.Repeat("字", summaryMaxRunes+10) + "</p>"
	got := summaryFromHTML(long, summaryMaxRunes)
	if runeCount := len([]rune(strings.TrimSuffix(got, "…"))); runeCount != summaryMaxRunes {
		t.Errorf("truncated summary has %d runes, want %d", runeCount, summaryMaxRunes)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated summary must end with ellipsis, got %q", got)
	}
	// 中文按 rune 截，不会出现半截字节；短于上限不加省略号。
	if short := summaryFromHTML("<p>短摘要</p>", summaryMaxRunes); short != "短摘要" {
		t.Errorf("short summary = %q", short)
	}
}

func TestTruncateSlugFallbacks(t *testing.T) {
	if got := truncateSlug("", "post"); got != "post" {
		t.Errorf("empty slug must fall back to post, got %q", got)
	}
	if got := truncateSlug("", "page"); got != "page" {
		t.Errorf("empty slug must fall back to page, got %q", got)
	}
	if got := truncateSlug(strings.Repeat("a", 70)+"-copy", "post"); len(got) > 64 {
		t.Errorf("len = %d, want <= 64", len(got))
	}
}

func TestNewsPagesOf(t *testing.T) {
	cases := []struct {
		total int64
		want  int
	}{
		{0, 1}, {1, 1}, {20, 1}, {21, 2}, {40, 2}, {41, 3},
	}
	for _, tc := range cases {
		if got := pagesOf(tc.total, newsPageSize); got != tc.want {
			t.Errorf("pagesOf(%d) = %d, want %d", tc.total, got, tc.want)
		}
	}
	if got := pageOf(0); got != 1 {
		t.Errorf("pageOf(0) = %d, want 1", got)
	}
	if got := pageOf(-3); got != 1 {
		t.Errorf("pageOf(-3) = %d, want 1", got)
	}
}
