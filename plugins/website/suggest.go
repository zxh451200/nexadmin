package website

import (
	"context"
	"time"

	"nexadmin/pkg/idgen"
)

// 询盘跟进的时效口径：超过这个时长还没人处理，后台标红并给出同类商品推荐，
// 让运营可以直接把「同款替代 / 相近规格」回给买家，而不是让线索晾着。
const msgOverdueAfter = 24 * time.Hour

// SuggestedProduct 是回给后台的一条「可推荐商品」。
// 只带运营真正用得上的字段：标题、价格、站内链接，不塞整份商品详情。
type SuggestedProduct struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Slug     string  `json:"slug"`
	Currency string  `json:"currency"`
	PriceMin float64 `json:"priceMin"`
	PriceMax float64 `json:"priceMax"`
	Moq      int     `json:"moq"`
	CoverURL string  `json:"coverUrl"`
	// Path 是站点默认语言下的详情页路径，运营复制即用（默认语言不带语言前缀）。
	Path string `json:"path"`
}

// msgOverdue 报告一条留言是否已经超时未处理：只有未处理（new/read）且超过 24 小时才算。
// 处理过的线索不再标红 —— 跟进完了再红一次只会让红点失去意义。
func msgOverdue(m *Message, now time.Time) bool {
	if m.Status == MsgHandled {
		return false
	}
	return now.Sub(m.CreatedAt) > msgOverdueAfter
}

// attachMessageSources 给一页留言批量补来源商品：一次查询搞定，避免每条留言回查一次商品。
// 草稿商品也照常返回 —— 后台要看的是「买家问的是哪一款」，哪怕它已经下架。
func (s *Service) attachMessageSources(ctx context.Context, rows []Message) {
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		if r.ProductID > 0 {
			ids = append(ids, int64(r.ProductID))
		}
	}
	if len(ids) == 0 {
		return
	}
	var prods []Product
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&prods).Error; err != nil {
		return // 来源商品查不到不影响留言本身的展示
	}
	byID := make(map[int64]SuggestedProduct, len(prods))
	for _, p := range prods {
		byID[int64(p.ID)] = SuggestedProduct{
			ID: p.ID.String(), Title: p.Title, Slug: p.Slug,
			Currency: p.Currency, PriceMin: p.PriceMin, PriceMax: p.PriceMax,
			Moq: p.Moq, CoverURL: p.CoverURL, Path: "/products/" + p.Slug,
		}
	}
	for i := range rows {
		if rows[i].ProductID == 0 {
			continue
		}
		if p, ok := byID[int64(rows[i].ProductID)]; ok {
			cp := p
			rows[i].SourceProduct = &cp
		}
	}
}

// SuggestedProductsForMessage 给出这条询盘可以推荐的商品：
// 来源商品同品类优先，不足则用其他已上架商品补齐；没有来源商品就直接给最新上架。
// 推荐是锦上添花，查不到就返回空列表，不把接口做成 500。
func (s *Service) SuggestedProductsForMessage(ctx context.Context, id idgen.ID, limit int) ([]SuggestedProduct, error) {
	if limit <= 0 || limit > 8 {
		limit = 4
	}
	var msg Message
	if err := s.db.WithContext(ctx).First(&msg, "id = ?", int64(id)).Error; err != nil {
		return nil, err
	}
	var rows []Product
	if msg.ProductID > 0 {
		var src Product
		if err := s.db.WithContext(ctx).Preload("Categories").First(&src, "id = ?", int64(msg.ProductID)).Error; err == nil {
			catIDs := make([]int64, 0, len(src.Categories))
			for _, c := range src.Categories {
				catIDs = append(catIDs, int64(c.ID))
			}
			if len(catIDs) > 0 {
				if err := liveProductTx(s.db.WithContext(ctx)).
					Where("id <> ?", int64(src.ID)).
					Where("id IN (SELECT product_id FROM plg_website_product_category_rel WHERE category_id IN ?)", catIDs).
					Order("updated_at DESC, id DESC").
					Limit(limit).
					Find(&rows).Error; err != nil {
					return nil, err
				}
			}
		}
	}
	if len(rows) < limit {
		skip := make([]int64, 0, len(rows)+1)
		if msg.ProductID > 0 {
			skip = append(skip, int64(msg.ProductID))
		}
		for _, r := range rows {
			skip = append(skip, int64(r.ID))
		}
		q := liveProductTx(s.db.WithContext(ctx))
		if len(skip) > 0 {
			q = q.Where("id NOT IN ?", skip)
		}
		var extra []Product
		if err := q.Order("updated_at DESC, id DESC").Limit(limit - len(rows)).Find(&extra).Error; err != nil {
			return nil, err
		}
		rows = append(rows, extra...)
	}
	out := make([]SuggestedProduct, 0, len(rows))
	for _, r := range rows {
		out = append(out, SuggestedProduct{
			ID:       r.ID.String(),
			Title:    r.Title,
			Slug:     r.Slug,
			Currency: r.Currency,
			PriceMin: r.PriceMin,
			PriceMax: r.PriceMax,
			Moq:      r.Moq,
			CoverURL: r.CoverURL,
			Path:     "/products/" + r.Slug,
		})
	}
	return out, nil
}
