package website

import (
	"context"

	"math"
	"strconv"
	"strings"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 评价展示与录入的口径集中在这里：
// 公开站只读已发布评价，聚合（均分/分布）与列表一次查完，避免模板里再算。

const (
	// reviewListLimit 是商详一次展示的评价条数：阿里首屏也是折叠几条 + 「查看全部」，
	// 这里干脆限定条数并把总数告诉访客，不做二次分页（评价量大时再补）。
	reviewListLimit = 8
	reviewTextLimit = 1000
)

// reviewStarRow 是评分分布的一档（5 星 → 1 星）。
type reviewStarRow struct {
	Star    int
	Count   int
	Percent int
}

// productReviewStats 是商详「评价」区的完整视图。
// Total=0 时模板整段不渲染（没评价就不摆一个空壳）。
type productReviewStats struct {
	Total int
	// AvgValue 给模板画星级用（四舍五入到整星），Average 是给人看的一位小数文本。
	AvgValue float64
	Average  string // 保留一位小数，模板直接显示，不在模板里做浮点格式化
	Stars    []reviewStarRow
	List     []ProductReview
	// WithText 是有正文的条数：只有星级没有正文的评价，列表里出引用块会很空。
	WithText int
}

// StarsFloat 把整数星级转成画星用的浮点值（模板 {{starRow .StarsFloat}}）。
func (r ProductReview) StarsFloat() float64 { return float64(clampRating(r.Rating)) }

// PublicProductReviews 组装商详评价区：只取 published 的评价，按时间倒序。
// 查询失败不挡详情页——评价是增强信息，静默降级成不展示。
func (s *Service) PublicProductReviews(ctx context.Context, productID int64) (productReviewStats, error) {
	var out productReviewStats
	if productID == 0 {
		return out, nil
	}
	var rows []ProductReview
	err := s.db.WithContext(ctx).
		Where("product_id = ? AND published = ?", productID, true).
		Order("created_at DESC, id DESC").
		Find(&rows).Error
	if err != nil {
		return out, err
	}
	if len(rows) == 0 {
		return out, nil
	}
	out.Total = len(rows)
	counts := [5]int{} // 索引 0 = 5 星
	sum := 0
	for _, r := range rows {
		star := clampRating(r.Rating)
		sum += star
		counts[5-star]++
		if strings.TrimSpace(r.Content) != "" {
			out.WithText++
		}
	}
	out.AvgValue = math.Round(float64(sum)/float64(out.Total)*10) / 10
	out.Average = formatAvg(out.AvgValue)
	out.Stars = make([]reviewStarRow, 0, 5)
	for i, n := range counts {
		star := 5 - i
		pct := 0
		if out.Total > 0 {
			pct = int(math.Round(float64(n) * 100 / float64(out.Total)))
		}
		out.Stars = append(out.Stars, reviewStarRow{Star: star, Count: n, Percent: pct})
	}
	if len(rows) > reviewListLimit {
		rows = rows[:reviewListLimit]
	}
	out.List = rows
	return out, nil
}

// formatAvg 把均分固定成一位小数：4.75 → "4.8"，5 → "5.0"。
// 固定位数比「去掉末尾 0」更合适：评价区旁边还有一排星星，宽度不能随分数跳动。
func formatAvg(v float64) string {
	return strconv.FormatFloat(math.Round(v*10)/10, 'f', 1, 64)
}

// clampRating 把星级钳到 1-5：后台脏数据（0、负数、8 星）不能把分布算穿。
func clampRating(r int) int {
	if r < 1 {
		return 1
	}
	if r > 5 {
		return 5
	}
	return r
}

// ReviewForm 是后台录入/修改评价的表单。
// ProductID 用字符串：雪花 ID 从前端过来是字符串（与列表接口回传的形态一致），这里再解析。
type ReviewForm struct {
	ProductID string `json:"productId"`
	Author    string `json:"author"`
	Country   string `json:"country"`
	Rating    int    `json:"rating"`
	Content   string `json:"content"`
	Verified  bool   `json:"verified"`
	Quantity  string `json:"quantity"`
	Published *bool  `json:"published"` // 指针：不传保持原值
}

// AdminListReviews 返回某个商品的全部评价（含草稿），供后台编辑。
// productID=0 时返回全部，后台评价总表用得上。
func (s *Service) AdminListReviews(ctx context.Context, productID int64) ([]ProductReview, error) {
	q := s.db.WithContext(ctx).Model(&ProductReview{})
	if productID > 0 {
		q = q.Where("product_id = ?", productID)
	}
	var rows []ProductReview
	err := q.Order("created_at DESC, id DESC").Limit(500).Find(&rows).Error
	return rows, err
}

// ReviewTotals 返回按商品分组的评价数（后台列表展示「N 条评价」不逐条 preload）。
func (s *Service) ReviewTotals(ctx context.Context, productIDs []int64) (map[int64]int, error) {
	out := make(map[int64]int, len(productIDs))
	if len(productIDs) == 0 {
		return out, nil
	}
	type row struct {
		ProductID int64
		N         int
	}
	var rows []row
	err := s.db.WithContext(ctx).Model(&ProductReview{}).
		Select("product_id, COUNT(*) AS n").
		Where("product_id IN ?", productIDs).
		Group("product_id").
		Scan(&rows).Error
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		out[r.ProductID] = r.N
	}
	return out, nil
}

// productRating 是列表卡的评分摘要（均分 + 条数）。
type productRating struct {
	Avg   float64
	Count int
}

// ProductRatingSummaries 一次算完一批商品的评分：列表页几十张卡不能一张查一次库
// （ReviewTotals 那个逐商品循环的做法只适合后台单页少量商品）。
// 只算已发布评价，与商详口径一致；没有评价的商品不在 map 里。
func (s *Service) ProductRatingSummaries(ctx context.Context, productIDs []int64) (map[int64]productRating, error) {
	out := make(map[int64]productRating, len(productIDs))
	if len(productIDs) == 0 {
		return out, nil
	}
	type row struct {
		ProductID int64
		N         int
		Avg       float64
	}
	var rows []row
	err := s.db.WithContext(ctx).Model(&ProductReview{}).
		Select("product_id, COUNT(*) AS n, AVG(rating) AS avg").
		Where("product_id IN ? AND published = ?", productIDs, true).
		Group("product_id").
		Scan(&rows).Error
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		out[r.ProductID] = productRating{Avg: math.Round(r.Avg*10) / 10, Count: r.N}
	}
	return out, nil
}

// CreateReview 录入一条评价：商品必须存在（草稿商品也行，上线后自然生效）。
func (s *Service) CreateReview(ctx context.Context, f ReviewForm) (*ProductReview, error) {
	pid, err := reviewProductID(f.ProductID)
	if err != nil {
		return nil, err
	}
	if err := s.checkReviewProduct(ctx, int64(pid)); err != nil {
		return nil, err
	}
	row := &ProductReview{
		ProductID: pid,
		Author:    trimLen(f.Author, 64),
		Country:   trimLen(f.Country, 64),
		Rating:    clampRating(defaultInt(f.Rating, 5)),
		Content:   trimLen(f.Content, reviewTextLimit),
		Verified:  f.Verified,
		Quantity:  trimLen(f.Quantity, 64),
		Published: f.Published == nil || *f.Published,
	}
	if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

// UpdateReview 修改评价；Published 为 nil 时保持原值（表单分块保存不误下线）。
func (s *Service) UpdateReview(ctx context.Context, id int64, f ReviewForm) (*ProductReview, error) {
	var row ProductReview
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return nil, response.NotFound("website.review_not_found")
	}
	row.Author = trimLen(f.Author, 64)
	row.Country = trimLen(f.Country, 64)
	row.Rating = clampRating(defaultInt(f.Rating, row.Rating))
	row.Content = trimLen(f.Content, reviewTextLimit)
	row.Verified = f.Verified
	row.Quantity = trimLen(f.Quantity, 64)
	if f.Published != nil {
		row.Published = *f.Published
	}
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteReview 删除评价（物理删除：运营误录的测试数据不该留痕在商品页）。
func (s *Service) DeleteReview(ctx context.Context, id int64) error {
	res := s.db.WithContext(ctx).Delete(&ProductReview{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return response.NotFound("website.review_not_found")
	}
	return nil
}

func (s *Service) checkReviewProduct(ctx context.Context, productID int64) error {
	if productID <= 0 {
		return response.BadRequest("website.review_product_required")
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&Product{}).Where("id = ?", productID).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return response.BadRequest("website.review_product_required")
	}
	return nil
}

// reviewProductID 解析表单里的商品 ID（雪花字符串）：空或非数字统一报「需要商品」，
// 不让调用方猜哪个字段错了。
func reviewProductID(raw string) (idgen.ID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, response.BadRequest("website.review_product_required")
	}
	pid, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || pid <= 0 {
		return 0, response.BadRequest("website.review_product_required")
	}
	return idgen.ID(pid), nil
}

func defaultInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
