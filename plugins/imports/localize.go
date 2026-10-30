package imports

// localize.go 处理来源标题与列名的多语言。契约上 Title/Label 是兜底文案，
// TitleKey/LabelKey 有值时按请求语言渲染；模板表头随之本地化。
// 反过来，解析上传文件时必须认识「所有已配置语言」的表头——用户可能拿英文模板
// 切到中文界面上传，表头匹配不能只看当前语言。

import (
	"context"
	"strings"

	"nexadmin/core/i18n"
)

// requiredSuffix 是必填列的表头后缀。没有 i18n 包(单测)时退回中文，保持模板口径稳定。
func requiredSuffix(ctx context.Context) string {
	const key = "imports.required_suffix"
	if v := i18n.T(ctx, key); v != key {
		return v
	}
	return "(必填)"
}

// localize 返回标题与列名已按请求语言渲染的副本(原 Source 的 Handler/Key 不变)。
func localize(ctx context.Context, s Source) Source {
	if s.TitleKey != "" {
		if v := i18n.T(ctx, s.TitleKey); v != s.TitleKey {
			s.Title = v
		}
	}
	cols := make([]Column, len(s.Columns))
	copy(cols, s.Columns)
	for i := range cols {
		if cols[i].LabelKey == "" {
			continue
		}
		if v := i18n.T(ctx, cols[i].LabelKey); v != cols[i].LabelKey {
			cols[i].Label = v
		}
	}
	s.Columns = cols
	return s
}

// labelIndex 把「表头文案 → 列 key」摊平：兜底 Label、各配置语言的译文、
// 以及它们带必填后缀的写法都认，避免跨语言模板上传失败。
func labelIndex(s Source) map[string]string {
	b := i18n.Default()
	var langs []string
	if b != nil {
		for _, l := range b.Languages() {
			langs = append(langs, l.Code)
		}
	}
	suffixes := []string{"(必填)"}
	for _, l := range langs {
		if b != nil {
			if v := b.T(l, "imports.required_suffix"); v != "imports.required_suffix" {
				suffixes = append(suffixes, v)
			}
		}
	}
	idx := make(map[string]string, len(s.Columns)*4)
	for _, c := range s.Columns {
		labels := []string{c.Label}
		for _, l := range langs {
			if c.LabelKey != "" && b != nil {
				labels = append(labels, b.T(l, c.LabelKey))
			}
		}
		for _, label := range labels {
			label = strings.TrimSpace(label)
			if label == "" || label == c.LabelKey {
				continue
			}
			idx[label] = c.Key
			for _, suf := range suffixes {
				idx[label+suf] = c.Key
			}
		}
	}
	return idx
}
