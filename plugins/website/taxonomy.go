package website

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/slug"

	"gorm.io/gorm"
)

// 标签是与栏目分层的第二维度：栏目（Category）是编辑部定好的有限集合，标签（Tag）是
// 编辑器里打字即长的自由主题词。名字决定身份——去重、复用都按名字，slug 只负责出现在 URL 里。
// slug 的派生口径与全站统一（pkg/slug 行业标准）：拉丁名压成小写连字符；纯中文派生不出
// 拉丁名时回退随机 base36 串，不再把原始 CJK 字符塞进 URL（2026-11-12 起，旧数据不迁移）。

const (
	// maxTagsPerPost 是一篇新闻的标签上限：标签是导航线索，堆到几十个等于没有。
	maxTagsPerPost = 20
	// tagMaxRunes 与 plg_website_tag.name 的列宽一致（VARCHAR(64) 按字符计）。
	tagMaxRunes = 64
	// publicTagLimit 是公开侧一次下发的标签数上限：标签会自由生长，列表要有界。
	publicTagLimit = 50
)

// normalizeTagName 压掉多余空白并按字符限长；名字是标签的身份，比较时统一走小写。
func normalizeTagName(s string) string {
	return trimRunes(strings.Join(strings.Fields(s), " "), tagMaxRunes)
}

// tagSlugOf 是标签 URL 标识的唯一派生入口：走 pkg/slug 的行业标准生成
// （NFKD 去变音符、小写、非字母数字压单连字符），纯中文等压不出拉丁时回退随机串，
// 不再把原始 CJK 字符塞进 URL——与全站 slug 口径一致（2026-11-12 统一）。
func tagSlugOf(name string) string {
	return slug.From(name, slug.MaxLen)
}

// termCountRow 是「某个栏目/标签下有多少篇新闻」的一行聚合结果。
type termCountRow struct {
	TermID int64 `gorm:"column:term_id"`
	Count  int64 `gorm:"column:c"`
}

// 表名与分组列写死在各自的查询里，不从参数拼：这两张关联表没有第三种形态。
func categoryPostCounts(db *gorm.DB) (map[int64]int64, error) {
	var rows []termCountRow
	err := db.Table("plg_website_post_category").
		Select("category_id AS term_id, COUNT(*) AS c").Group("category_id").Scan(&rows).Error
	return termCountMap(rows), err
}

func tagPostCounts(db *gorm.DB) (map[int64]int64, error) {
	var rows []termCountRow
	err := db.Table("plg_website_post_tag").
		Select("tag_id AS term_id, COUNT(*) AS c").Group("tag_id").Scan(&rows).Error
	return termCountMap(rows), err
}

func termCountMap(rows []termCountRow) map[int64]int64 {
	out := make(map[int64]int64, len(rows))
	for _, r := range rows {
		out[r.TermID] = r.Count
	}
	return out
}

// uniqueTagSlug 在已有 slug 上让号：foo → foo-2 → foo-3。
// 「Foo Bar」与「foo-bar」是两个名字、同一个 slug，不让号就会撞唯一索引。
func uniqueTagSlug(tx *gorm.DB, base string) (string, error) {
	slug := base
	for n := 2; ; n++ {
		var cnt int64
		if err := tx.Model(&Tag{}).Where("slug = ?", slug).Count(&cnt).Error; err != nil {
			return "", err
		}
		if cnt == 0 {
			return slug, nil
		}
		if n > 20 {
			return "", response.BadRequest("website.slug_taken")
		}
		suffix := "-" + strconv.Itoa(n)
		slug = trimRunes(base, tagMaxRunes-len(suffix)) + suffix
	}
}

// resolveTagsTx 按名字找或建标签，返回顺序与输入一致、按名字去重后的集合。
// 必须在事务里调用：新建的标签跟着新闻一起成败，别留下「新闻没存上、标签先长出来」的孤儿。
func resolveTagsTx(tx *gorm.DB, names []string) ([]Tag, error) {
	out := make([]Tag, 0, len(names))
	seen := make(map[string]bool, len(names))
	for _, raw := range names {
		name := normalizeTagName(raw)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		var row Tag
		err := tx.Where("LOWER(name) = ?", key).First(&row).Error
		switch {
		case err == nil:
		case errors.Is(err, gorm.ErrRecordNotFound):
			slug, serr := uniqueTagSlug(tx, tagSlugOf(name))
			if serr != nil {
				return nil, serr
			}
			row = Tag{Name: name, Slug: slug}
			if cerr := tx.Create(&row).Error; cerr != nil {
				return nil, cerr
			}
		default:
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

// CategoryBySlug 按 slug 取栏目：公开归档页用它区分「链接写错了」和「这个栏目还没内容」。
func (s *Service) CategoryBySlug(ctx context.Context, slug string) (*Category, error) {
	var row Category
	if err := s.db.WithContext(ctx).Where("slug = ?", slug).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// TagBySlug 按 slug 取标签，语义同 CategoryBySlug。
func (s *Service) TagBySlug(ctx context.Context, slug string) (*Tag, error) {
	var row Tag
	if err := s.db.WithContext(ctx).Where("slug = ?", slug).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// ListTags 是管理端的标签清单，带每个标签的新闻数，按使用量降序、同量按名字。
func (s *Service) ListTags(ctx context.Context) ([]Tag, error) {
	var rows []Tag
	if err := s.db.WithContext(ctx).Order("name").Find(&rows).Error; err != nil {
		return nil, err
	}
	counts, err := tagPostCounts(s.db.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].PostCount = counts[int64(rows[i].ID)]
	}
	return rows, nil
}

// ListPublicTags 是公开侧下发的标签集合：只取有新闻在用的，按使用量排序并限流。
func (s *Service) ListPublicTags(ctx context.Context) ([]Tag, error) {
	rows, err := s.ListTags(ctx)
	if err != nil {
		return nil, err
	}
	used := make([]Tag, 0, len(rows))
	for _, t := range rows {
		if t.PostCount > 0 {
			used = append(used, t)
		}
	}
	sortTagsByUsage(used)
	if len(used) > publicTagLimit {
		used = used[:publicTagLimit]
	}
	return used, nil
}

// sortTagsByUsage 按使用量降序、同量按名字：标签没有人工排序，用量就是唯一的序。
func sortTagsByUsage(rows []Tag) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].PostCount != rows[j].PostCount {
			return rows[i].PostCount > rows[j].PostCount
		}
		return rows[i].Name < rows[j].Name
	})
}

// SaveTag 是标签管理：改名可以，slug 只在没给的时候派生——已分享的 /news?tag=xxx 不该因为改个名断掉。
func (s *Service) SaveTag(ctx context.Context, id idgen.ID, f TagForm) (*Tag, error) {
	name := normalizeTagName(f.Name)
	if name == "" {
		return nil, response.BadRequest("website.tag_name_required")
	}
	var row Tag
	if id != 0 {
		if err := s.db.WithContext(ctx).First(&row, "id = ?", int64(id)).Error; err != nil {
			return nil, err
		}
	} else {
		row = Tag{}
	}
	row.Name = name
	if strings.TrimSpace(f.Slug) == "" {
		slug := tagSlugOf(name)
		if slug != row.Slug {
			unique, err := uniqueTagSlug(s.db.WithContext(ctx), slug)
			if err != nil {
				return nil, err
			}
			slug = unique
		}
		row.Slug = slug
	} else {
		// 显式 slug 与其他内容同一口径：校验后撞名自动让号（base-2、base-3…），不再报错打断
		slugOut, err := s.resolveSlug(ctx, &Tag{}, f.Slug, name, id)
		if err != nil {
			return nil, err
		}
		row.Slug = slugOut
	}
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteTag 连同新闻-标签关联一起删：留着悬空关联，新闻详情会渲染出一个点进去没有结果的标签。
func (s *Service) DeleteTag(ctx context.Context, id idgen.ID) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&Tag{}, "id = ?", int64(id)).Error; err != nil {
			return err
		}
		return tx.Exec("DELETE FROM plg_website_post_tag WHERE tag_id = ?", int64(id)).Error
	})
}

// termLink 是公开侧栏目/标签导航的一项。
// Href 可选：需要带附加查询参数（如产品中心的检索词）时由后端拼好完整链接，
// 避免模板里「文本 + 动作」拼 URL 被 html/template 二次转义。
type termLink struct {
	Name   string
	Slug   string
	Active bool
	Href   string
}

// termFilter 是公开新闻列表的收窄条件：Kind 为 category/tag 时按对应 slug 过滤，空 = 全部新闻。
type termFilter struct {
	Kind string
	Slug string
}

// CategoryLinks 把分类集合转成模板可渲染的导航项，并标出当前归档的那一项。
func CategoryLinks(rows []Category, active string) []termLink {
	out := make([]termLink, 0, len(rows))
	for _, c := range rows {
		out = append(out, termLink{Name: c.Title, Slug: c.Slug, Active: c.Slug == active})
	}
	return out
}

// TagLinks 同 CategoryLinks，作用于标签。
func TagLinks(rows []Tag, active string) []termLink {
	out := make([]termLink, 0, len(rows))
	for _, t := range rows {
		out = append(out, termLink{Name: t.Name, Slug: t.Slug, Active: t.Slug == active})
	}
	return out
}
