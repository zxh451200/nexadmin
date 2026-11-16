package website

import (
	"context"

	pi18n "nexadmin/pkg/i18n"
)

// ContentNeighbor 是公开详情页「上一篇 / 下一篇」的一项。
// 只带标题与 slug：链接由模板用 URL() 拼，语言前缀与转义不在服务层重复实现。
type ContentNeighbor struct {
	Title string
	Slug  string
}

// neighborRef 是排序窗口里的一行定位信息：id 用来回查完整行，slug 用来找当前页的位置。
type neighborRef struct {
	id   int64
	slug string
}

// neighborIDs 在已按列表口径排好的序列里找当前项，返回前一项 / 后一项的 id（没有就是 0）。
// 「上一篇」是列表里更靠前（更新）的那条，「下一篇」是更靠后（更旧）的那条，
// 和访客在列表页看到的顺序一致。
func neighborIDs(seq []neighborRef, slug string) (int64, int64) {
	for i, it := range seq {
		if it.slug != slug {
			continue
		}
		var prev, next int64
		if i > 0 {
			prev = seq[i-1].id
		}
		if i+1 < len(seq) {
			next = seq[i+1].id
		}
		return prev, next
	}
	return 0, 0
}

// PublishedPostNeighbors 取当前新闻在该语言已发布序列中的上一篇 / 下一篇。
// 排序与语言口径和 PublishedPostsPage 完全一致：置顶优先、发布时间倒序、id 倒序；
// 预览中的草稿不在序列里，返回空，模板就不渲染导航。
func (s *Service) PublishedPostNeighbors(ctx context.Context, lang, slug string) (*ContentNeighbor, *ContentNeighbor, error) {
	var window []Post
	if err := livePostTx(s.db.WithContext(ctx)).
		Select("id", "slug", "published_json").
		Order("pinned DESC, published_at DESC, id DESC").
		Limit(newsPageCap).Find(&window).Error; err != nil {
		return nil, nil, err
	}
	seq := make([]neighborRef, 0, len(window))
	for _, p := range window {
		if len(Languages()) > 1 && !pi18n.Published(true, p.PublishedJSON, lang, siteLocales{}) {
			continue
		}
		seq = append(seq, neighborRef{id: int64(p.ID), slug: p.Slug})
	}
	prevID, nextID := neighborIDs(seq, slug)
	ids := make([]int64, 0, 2)
	if prevID != 0 {
		ids = append(ids, prevID)
	}
	if nextID != 0 {
		ids = append(ids, nextID)
	}
	if len(ids) == 0 {
		return nil, nil, nil
	}
	var rows []Post
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	byID := make(map[int64]*ContentNeighbor, len(rows))
	for i := range rows {
		rows[i].localize(lang)
		byID[int64(rows[i].ID)] = &ContentNeighbor{Title: rows[i].Title, Slug: rows[i].Slug}
	}
	return byID[prevID], byID[nextID], nil
}

// PublishedProductNeighbors 取当前商品在该语言已上架序列中的上一条 / 下一条。
// 排序口径与 PublishedProductsPage 一致：更新时间倒序、id 倒序。
func (s *Service) PublishedProductNeighbors(ctx context.Context, lang, slug string) (*ContentNeighbor, *ContentNeighbor, error) {
	var window []Product
	if err := liveProductTx(s.db.WithContext(ctx)).
		Select("id", "slug", "published_json").
		Order("updated_at DESC, id DESC").
		Limit(productPageCap).Find(&window).Error; err != nil {
		return nil, nil, err
	}
	seq := make([]neighborRef, 0, len(window))
	for _, p := range window {
		if len(Languages()) > 1 && !pi18n.Published(true, p.PublishedJSON, lang, siteLocales{}) {
			continue
		}
		seq = append(seq, neighborRef{id: int64(p.ID), slug: p.Slug})
	}
	prevID, nextID := neighborIDs(seq, slug)
	ids := make([]int64, 0, 2)
	if prevID != 0 {
		ids = append(ids, prevID)
	}
	if nextID != 0 {
		ids = append(ids, nextID)
	}
	if len(ids) == 0 {
		return nil, nil, nil
	}
	var rows []Product
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	byID := make(map[int64]*ContentNeighbor, len(rows))
	for i := range rows {
		rows[i].localize(lang)
		byID[int64(rows[i].ID)] = &ContentNeighbor{Title: rows[i].Title, Slug: rows[i].Slug}
	}
	return byID[prevID], byID[nextID], nil
}
