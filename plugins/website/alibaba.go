package website

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/i18n"

	"gorm.io/gorm"

	"nexadmin/core/cache"
	"nexadmin/core/job"
	"nexadmin/core/response"
	"nexadmin/core/secret"
	pi18n "nexadmin/pkg/i18n"
	"nexadmin/pkg/idgen"
)

// 阿里国际站同步（二期）：开放平台凭据管理、类目代理、发品链路与批量任务。
//
// 发品走 schema 流程（developer.alibaba.com treeId=456&articleId=119213）：
//   - alibaba.icbu.product.schema.get(category_id)：取发品规则，也用它提前验证类目 ID 合法；
//   - alibaba.icbu.product.schema.add(category_id, xml)：提交已填值的 field XML；
//   - 图片必须先传阿里图片银行（alibaba.icbu.photobank.upload，multipart，file_name + image_bytes，
//     返回 upload_image_response{file_id, photobank_url}，单图上限 5MB）；
//   - market：1=下单品（需要价格/SKU/物流），2=询盘品（信息要求低，作为默认）。
//
// XML 的 field 结构以官方 schema 文档为准；字段名按文档写死，真实联调若发现出入，
// 只需要改 buildAliProductXML 一处（错误信息会带上网关原文，便于对照）。

// 阿里发品类型：对应 schema 的 market 字段。
const (
	AliMarketInquiry   = "inquiry"   // 询盘品（market=2）
	AliMarketWholesale = "wholesale" // 下单品（market=1）
)

// aliMarketValue 把本站的发品类型转成阿里 schema 的 market 值。
func aliMarketValue(market string) (string, error) {
	switch market {
	case AliMarketInquiry, "":
		return "2", nil
	case AliMarketWholesale:
		return "1", nil
	default:
		return "", response.BadRequest("website.ali_market_invalid")
	}
}

// 发品图片上限：阿里的主图位（含主图）最多 6 张。
const aliMaxImages = 6

// AliConfigView 是凭据的接口视图：Secret/Session 只回「已配置」，明文永不出去。
type AliConfigView struct {
	AppKey     string `json:"appKey"`
	HasSecret  bool   `json:"hasSecret"`
	HasSession bool   `json:"hasSession"`
	ApiGateway string `json:"apiGateway"`
}

// aliConfigRow 取单行配置；没有行时返回空配置（nil err）。
func (s *Service) aliConfigRow(ctx context.Context) (*AliConfig, error) {
	var row AliConfig
	err := s.db.WithContext(ctx).First(&row, "id = (SELECT MIN(id) FROM plg_website_ali_config)").Error
	if err == gorm.ErrRecordNotFound {
		return &AliConfig{ApiGateway: AliGatewayDefault}, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func aliConfigView(row *AliConfig) *AliConfigView {
	return &AliConfigView{
		AppKey:     row.AppKey,
		HasSecret:  row.AppSecretEnc != "",
		HasSession: row.SessionKeyEnc != "",
		ApiGateway: row.ApiGateway,
	}
}

// GetAliConfig 读凭据视图。
func (s *Service) GetAliConfig(ctx context.Context) (*AliConfigView, error) {
	row, err := s.aliConfigRow(ctx)
	if err != nil {
		return nil, err
	}
	return aliConfigView(row), nil
}

// aliSecretMask 是框架设置掩码（modules/system/config）的同款值：前端把已配置的
// 密码框渲染成它，保存时空串或掩码都表示「保持旧值」。
const aliSecretMask = "********"

// SaveAliConfig 保存凭据：AppKey/网关照存；Secret/Session 三态——空或掩码 = 保持旧值，
// 其余重新加密落库。返回保存后的视图。
func (s *Service) SaveAliConfig(ctx context.Context, f AliConfigForm) (*AliConfigView, error) {
	row, err := s.aliConfigRow(ctx)
	if err != nil {
		return nil, err
	}
	row.AppKey = trimLen(strings.TrimSpace(f.AppKey), 64)
	gw := strings.TrimSpace(f.ApiGateway)
	if gw != "" {
		if !strings.HasPrefix(gw, "https://") {
			return nil, response.BadRequest("website.ali_gateway_https")
		}
		row.ApiGateway = trimLen(gw, 100)
	} else if row.ApiGateway == "" {
		row.ApiGateway = AliGatewayDefault
	}
	if v := strings.TrimSpace(f.AppSecret); v != "" && v != aliSecretMask {
		enc, serr := secret.Encrypt(v)
		if serr != nil {
			return nil, serr
		}
		row.AppSecretEnc = enc
	}
	if v := strings.TrimSpace(f.SessionKey); v != "" && v != aliSecretMask {
		enc, serr := secret.Encrypt(v)
		if serr != nil {
			return nil, serr
		}
		row.SessionKeyEnc = enc
	}
	if err := s.db.WithContext(ctx).Save(row).Error; err != nil {
		return nil, err
	}
	return aliConfigView(row), nil
}

// aliCredentials 取解密后的凭据；未配置返回明确的业务错误。
func (s *Service) aliCredentials(ctx context.Context) (AliCredentials, error) {
	row, err := s.aliConfigRow(ctx)
	if err != nil {
		return AliCredentials{}, err
	}
	if row.AppKey == "" || row.AppSecretEnc == "" {
		return AliCredentials{}, response.BadRequest("website.ali_not_configured")
	}
	appSecret, err := secret.Decrypt(row.AppSecretEnc)
	if err != nil {
		return AliCredentials{}, fmt.Errorf("ali app secret decrypt: %w", err)
	}
	session := ""
	if row.SessionKeyEnc != "" {
		if session, err = secret.Decrypt(row.SessionKeyEnc); err != nil {
			return AliCredentials{}, fmt.Errorf("ali session decrypt: %w", err)
		}
	}
	return AliCredentials{
		AppKey: row.AppKey, AppSecret: appSecret, Session: session, Gateway: row.ApiGateway,
	}, nil
}

// TestAliConnection 连接测试：调 alibaba.icbu.category.get.new（cat_id=0，免费且无需
// 授权）——网关可达、AppKey/Secret 签名通过即成功；SessionKey 的有效性在首次发品时验证。
func (s *Service) TestAliConnection(ctx context.Context) error {
	c, err := s.aliCredentials(ctx)
	if err != nil {
		return err
	}
	c.Session = ""
	_, err = aliCall(ctx, c, "alibaba.icbu.category.get.new", map[string]string{"cat_id": "0"})
	return err
}

// AliCategoryNode 是下发给前端的类目树节点（精简：只留选择器需要的字段）。
type AliCategoryNode struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	CnName string `json:"cnName"`
	Leaf   bool   `json:"leaf"`
}

// aliCategoriesTTL 是类目树缓存时长：类目变化以月计，缓存一天足够。
const aliCategoriesTTL = 24 * time.Hour

// AliCategories 拉阿里类目的子类目（parentID "0" = 一级），带缓存。
func (s *Service) AliCategories(ctx context.Context, parentID string) ([]AliCategoryNode, error) {
	if strings.TrimSpace(parentID) == "" {
		parentID = "0"
	}
	ck := cache.GlobalKey("website", "ali", "cat", parentID)
	if s.cache != nil {
		if raw, err := s.cache.Get(ctx, ck); err == nil && raw != "" {
			var cached []AliCategoryNode
			if json.Unmarshal([]byte(raw), &cached) == nil {
				return cached, nil
			}
		}
	}
	c, err := s.aliCredentials(ctx)
	if err != nil {
		return nil, err
	}
	// 类目接口无需授权（免费 API），不带 session 调用，避免 session 过期挡住选类目。
	c.Session = ""
	// 响应节点 result：类目数组，字段 category_id / name / cn_name / leaf_category。
	res, err := aliCall(ctx, c, "alibaba.icbu.category.get.new", map[string]string{"cat_id": parentID})
	if err != nil {
		return nil, err
	}
	raw, ok := res["result"]
	if !ok {
		// 空结果也当空数组返回（叶子类目没有子级）。
		return []AliCategoryNode{}, nil
	}
	var items []struct {
		CategoryID   any    `json:"category_id"`
		Name         string `json:"name"`
		CnName       string `json:"cn_name"`
		LeafCategory bool   `json:"leaf_category"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("ali categories parse: %w (%s)", err, aliSnippet(raw))
	}
	out := make([]AliCategoryNode, 0, len(items))
	for _, it := range items {
		out = append(out, AliCategoryNode{
			ID:     fmt.Sprint(it.CategoryID),
			Name:   it.Name,
			CnName: it.CnName,
			Leaf:   it.LeafCategory,
		})
	}
	if s.cache != nil {
		if b, jerr := json.Marshal(out); jerr == nil {
			_ = s.cache.Set(ctx, ck, string(b), aliCategoriesTTL)
		}
	}
	return out, nil
}

// BindAliCategory 把选好的阿里叶子类目 ID 回填到商品上。
func (s *Service) BindAliCategory(ctx context.Context, id idgen.ID, catID string) error {
	catID = strings.TrimSpace(catID)
	if catID == "" || len(catID) > 32 {
		return response.BadRequest("website.ali_category_invalid")
	}
	if _, err := s.ProductByID(ctx, id); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&Product{}).Where("id = ?", int64(id)).
		Update("alibaba_category_id", catID).Error
}

// markAliSync 回写同步结果：成功时写阿里商品 ID 与时间并清错误；失败写原因。
// 成功标 pending 而不是 synced：阿里发品后有一段审核期，直接标「已发布」会与
// 实际状态错位——审核结果由 RefreshAliReviewStatus 从阿里侧拉回后流转到 synced / rejected。
func (s *Service) markAliSync(ctx context.Context, id idgen.ID, aliPID, errText string) error {
	updates := map[string]any{"sync_status": ProductSyncFailed, "sync_error": trimLen(errText, 500)}
	if aliPID != "" {
		now := time.Now().UTC()
		updates = map[string]any{
			"sync_status": ProductSyncPending, "alibaba_product_id": aliPID,
			"synced_at": &now, "sync_error": "",
		}
	}
	return s.db.WithContext(ctx).Model(&Product{}).Where("id = ?", int64(id)).Updates(updates).Error
}

// mapAliProductStatus 把阿里侧的商品状态字串映射成本地同步状态；返回 ok=false 表示
// 该字串不在已知集合里（保持原状，绝不因为认识不了的字串把商品改状态）。
// 已知值取自阿里国际站的商品状态枚举的常见写法，多形态容错；真实凭据联调后再校准。
func mapAliProductStatus(raw string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case s == "":
		return "", false
	case strings.Contains(s, "approv"), strings.Contains(s, "publish"),
		strings.Contains(s, "online"), strings.Contains(s, "pass"):
		return ProductSyncSynced, true
	case strings.Contains(s, "audit"), strings.Contains(s, "review"),
		strings.Contains(s, "check"), strings.Contains(s, "pend"), strings.Contains(s, "wait"):
		return ProductSyncPending, true
	case strings.Contains(s, "reject"), strings.Contains(s, "invalid"),
		strings.Contains(s, "deny"), strings.Contains(s, "fail"):
		return ProductSyncRejected, true
	}
	return "", false
}

// RefreshAliReviewStatus 从阿里侧刷新已发品商品的审核状态：调 product.list 分页拉回
// 本店商品状态，按 alibaba_product_id 匹配回写 sync_status。只动 pending / synced /
// rejected 三态的行——failed 的行得先重新发品，拉状态改变不了什么。
// 返回（扫描的阿里商品行数、回写的本地条数）。
func (s *Service) RefreshAliReviewStatus(ctx context.Context, baseURL string) (int, int, error) {
	c, err := s.aliCredentials(ctx)
	if err != nil {
		return 0, 0, err
	}
	// 本地待刷新集合：aliPID → 商品 ID
	rows, err := s.db.WithContext(ctx).Model(&Product{}).
		Where("alibaba_product_id <> '' AND sync_status IN ?", []string{ProductSyncPending, ProductSyncSynced, ProductSyncRejected}).
		Select("id, alibaba_product_id").Rows()
	if err != nil {
		return 0, 0, err
	}
	byAli := map[string]idgen.ID{}
	var ids []idgen.ID
	for rows.Next() {
		var id idgen.ID
		var aliPID string
		if err := rows.Scan(&id, &aliPID); err != nil {
			rows.Close()
			return 0, 0, err
		}
		aliPID = strings.TrimSpace(aliPID)
		if aliPID != "" && aliPID != "ok" {
			byAli[aliPID] = id
			ids = append(ids, id)
		}
	}
	rows.Close()
	if len(byAli) == 0 {
		return 0, 0, nil
	}

	scanned := 0
	updated := 0
	// 分页拉回本店商品：最多 5 页（250 条），超出说明店铺规模远超演示形态，该走导出了
	for page := int64(1); page <= 5; page++ {
		res, err := aliCall(ctx, c, "alibaba.icbu.product.list", map[string]string{
			"currentPage": strconv.FormatInt(page, 10),
			"pageSize":    "50",
		})
		if err != nil {
			return scanned, updated, err
		}
		list := aliProductStatusRows(res)
		if len(list) == 0 {
			break
		}
		scanned += len(list)
		for _, it := range list {
			id, hit := byAli[it.id]
			if !hit {
				continue
			}
			status, ok := mapAliProductStatus(it.status)
			if !ok {
				continue
			}
			res := s.db.WithContext(ctx).Model(&Product{}).Where("id = ?", int64(id)).
				Update("sync_status", status)
			if res.Error != nil {
				return scanned, updated, res.Error
			}
			updated += int(res.RowsAffected)
		}
		if len(list) < 50 {
			break
		}
	}
	return scanned, updated, nil
}

// aliProductStatusRows 从 product.list 的响应里提取商品行的 id 与状态。
// 阿里同一接口在不同文档版本下的包裹结构不一（result.products / result.list / 直接数组），
// 多形态容错——自顶向下找第一个「元素同时带 id 与状态」的数组整组收下；
// 真凭据联调时以网关原文为准校准。
// aliStatusRow 是 product.list 提取出来的商品行：阿里商品 ID + 侧状态字串。
type aliStatusRow struct{ id, status string }

func aliProductStatusRows(res map[string]json.RawMessage) []aliStatusRow {
	var out []aliStatusRow
	found := false
	var dig func(raw json.RawMessage)
	dig = func(raw json.RawMessage) {
		if found || len(raw) == 0 {
			return
		}
		var v any
		if json.Unmarshal(raw, &v) != nil {
			return
		}
		switch x := v.(type) {
		case []any:
			ids, stats := 0, 0
			for _, it := range x {
				m, ok := it.(map[string]any)
				if !ok {
					continue
				}
				_, st := aliRowIDStatus(m)
				if _, hasID := m["id"]; hasID {
					ids++
				}
				if st != "" {
					stats++
				}
			}
			// 数组里多数元素都带 id 与状态：认定这就是商品行列表，整组收下
			if len(x) > 0 && ids == len(x) && stats == len(x) {
				for _, it := range x {
					m, ok := it.(map[string]any)
					if !ok {
						continue
					}
					id, st := aliRowIDStatus(m)
					if id != "" {
						out = append(out, aliStatusRow{id, st})
					}
				}
				found = true
				return
			}
			for _, it := range x {
				if m, ok := it.(map[string]any); ok {
					if raw2, err := json.Marshal(m); err == nil {
						dig(raw2)
						if found {
							return
						}
					}
				}
			}
		case map[string]any:
			for _, child := range x {
				if raw2, err := json.Marshal(child); err == nil {
					dig(raw2)
					if found {
						return
					}
				}
			}
		}
	}
	for _, key := range []string{"result", "data"} {
		if raw, ok := res[key]; ok {
			// methodToNode 的包裹（..._result）在 aliParse 里已剥，这里 result 一般即业务体
			dig(raw)
		}
	}
	return out
}

// aliRowIDStatus 从商品行里取 id 与状态：status 可能直接在行上，也可能包在 productStatusInfo 里。
func aliRowIDStatus(m map[string]any) (string, string) {
	id := ""
	switch v := m["id"].(type) {
	case string:
		id = v
	case float64:
		id = strconv.FormatFloat(v, 'f', 0, 64)
	}
	status := ""
	if v, ok := m["status"].(string); ok {
		status = v
	} else if psi, ok := m["productStatusInfo"].(map[string]any); ok {
		status, _ = psi["status"].(string)
	}
	return id, status
}

// aliEnglish 取商品的英文母本内容（对齐阿里发品语言）：
// 已配置语言里找 en 前缀的译文；找不到（或该语言没填）回退主列——默认语言是英文的站点主列即英文。
func aliEnglish(p *Product) (title, keywords, summary, content string) {
	title, keywords, summary, content = p.Title, p.Keywords, p.Summary, p.Content
	for _, l := range Languages() {
		if !strings.HasPrefix(strings.ToLower(l.Code), "en") {
			continue
		}
		tr := translations(p.I18nJSON, l.Code)
		title = pi18n.Pick(tr, "title", title)
		keywords = pi18n.Pick(tr, "keywords", keywords)
		summary = pi18n.Pick(tr, "summary", summary)
		content = pi18n.Pick(tr, "content", content)
		break
	}
	return title, keywords, summary, content
}

// aliSyncResult 是一次同步（或预览）的产物：提交出去的 xml、阿里侧商品 ID、
// 缺失的必填字段与映射过程中的提醒（枚举匹配不上、图片上传失败等）。
type aliSyncResult struct {
	AlibabaProductID string   `json:"alibabaProductId"`
	XML              string   `json:"xml"`
	Missing          []string `json:"missing"`
	Notes            []string `json:"notes"`
	Updated          bool     `json:"updated"` // true = 走增量更新（已有阿里商品 ID），不会重复建品
}

// aliLanguageDefault 是提交给阿里的文案语种：我们同步的是英文母本（见 aliEnglish）。
const aliLanguageDefault = "en_US"

// PreviewAliSync 只做「取规则 + 映射 + 自检」：不传图、不提交，用于发品前把缺什么摊开给管理员看。
func (s *Service) PreviewAliSync(ctx context.Context, id idgen.ID, market, baseURL string) (*aliSyncResult, error) {
	return s.aliSync(ctx, id, market, baseURL, true)
}

// SyncProduct 把一个商品发到阿里国际站（已有阿里商品 ID 时走增量更新），返回阿里商品 ID。
// 任何失败都会回写 sync_status=failed 与 sync_error 并把错误向上返回（单发场景直接 toast）。
func (s *Service) SyncProduct(ctx context.Context, id idgen.ID, market, baseURL string) (string, error) {
	res, err := s.aliSync(ctx, id, market, baseURL, false)
	if err != nil {
		return "", err
	}
	return res.AlibabaProductID, nil
}

// aliSync 是发品/更新的唯一实现，dryRun=true 时不产生任何写操作（预览）。
func (s *Service) aliSync(ctx context.Context, id idgen.ID, market, baseURL string, dryRun bool) (*aliSyncResult, error) {
	fail := func(err error) (*aliSyncResult, error) { return nil, err }
	marketValue, err := aliMarketValue(market)
	if err != nil {
		return fail(err)
	}
	p, err := s.ProductByID(ctx, id)
	if err != nil {
		return fail(err)
	}
	if p.AlibabaCategoryID == "" {
		return fail(response.BadRequest("website.ali_category_required"))
	}
	title, keywords, _, content := aliEnglish(p)
	if strings.TrimSpace(title) == "" {
		return fail(response.BadRequest("website.ali_english_required"))
	}
	if marketValue == "1" && p.PriceMin <= 0 {
		return fail(response.BadRequest("website.ali_price_required"))
	}
	c, err := s.aliCredentials(ctx)
	if err != nil {
		if !dryRun {
			_ = s.markAliSync(ctx, id, "", err.Error())
		}
		return fail(err)
	}

	// ① 取该类目的发品规则：规则就是表单，缺了它我们连「该填什么」都不知道。
	res, err := aliCallNested(ctx, c, "alibaba.icbu.product.schema.get", aliPublishParam{
		CatID: p.AlibabaCategoryID, Language: aliLanguageDefault,
	})
	if err != nil {
		if !dryRun {
			_ = s.markAliSync(ctx, id, "", err.Error())
		}
		return fail(err)
	}
	getBiz := aliReadBiz(res)
	if e := getBiz.err(); e != nil {
		err = response.BadRequest("website.ali_gateway_rejected", e.Error())
		if !dryRun {
			_ = s.markAliSync(ctx, id, "", err.Error())
		}
		return fail(err)
	}
	sc, err := parseAliSchema(getBiz.Data)
	if err != nil {
		err = response.BadRequest("website.ali_schema_unreadable", err.Error())
		if !dryRun {
			_ = s.markAliSync(ctx, id, "", err.Error())
		}
		return fail(err)
	}

	// ② 图片：阿里的发品图必须来自图片银行，且值上要挂 fileId（官方 3.4.1）。
	// 预览不产生写操作，直接用本地/CDN 地址占位并说明。
	images, notes := s.aliBankImages(ctx, c, p, baseURL, dryRun)

	// ③ 按规则映射本地数据。
	mapped := aliMap(ctx, sc, p, marketValue, title, keywords, content, images)
	notes = append(notes, mapped.Notes...)
	missing := make([]string, 0, len(mapped.Missing))
	for _, m := range mapped.Missing {
		missing = append(missing, m.label())
	}
	out := &aliSyncResult{XML: mapped.XML, Missing: missing, Notes: notes, Updated: aliNumericID(p.AlibabaProductID)}
	if len(missing) > 0 {
		// 提交前就自检：必填缺什么直接列出来，而不是等网关回一句 isv 错误。
		// 预览模式不报错——missing 列表本身就是这次预览的结论。
		if dryRun {
			return out, nil
		}
		err = response.BadRequest("website.ali_required_missing", strings.Join(missing, "、"))
		_ = s.markAliSync(ctx, id, "", err.Error())
		return out, err
	}
	if dryRun {
		return out, nil
	}

	// ④ 提交：已有阿里商品 ID 走增量更新，否则新发。
	var submit map[string]json.RawMessage
	if out.Updated {
		submit, err = aliCallNested(ctx, c, "alibaba.icbu.product.schema.update", aliPublishParam{
			CatID: p.AlibabaCategoryID, ProductID: p.AlibabaProductID, Language: aliLanguageDefault, XML: mapped.XML,
		})
	} else {
		submit, err = aliSubmitAdd(ctx, c, p.AlibabaCategoryID, mapped.XML)
	}
	if err != nil {
		_ = s.markAliSync(ctx, id, "", err.Error())
		return out, err
	}
	biz := aliReadBiz(submit)
	if e := biz.err(); e != nil {
		// biz_success=false 时网关仍是 HTTP 200：不看这道就会把失败当成「同步成功」。
		wrapped := response.BadRequest("website.ali_gateway_rejected", e.Error())
		_ = s.markAliSync(ctx, id, "", wrapped.Error())
		return out, wrapped
	}
	aliPID := biz.ProductID
	if aliPID == "" {
		aliPID = aliStringField(submit, "product_id", "productID", "productId")
	}
	if aliPID == "" {
		aliPID = "ok"
	}
	out.AlibabaProductID = aliPID
	if len(notes) > 0 {
		zap.L().Info("ali sync submitted with notes", zap.String("slug", p.Slug), zap.Strings("notes", notes))
	}
	if err := s.markAliSync(ctx, id, aliPID, ""); err != nil {
		return out, err
	}
	return out, nil
}

// aliBankImages 把主图 + 图集送进阿里图片银行（上限 6 张），返回可提交的图片与提醒。
// 预览模式不上传：用原地址占位，让管理员看到「会提交哪几张」。
func (s *Service) aliBankImages(ctx context.Context, c AliCredentials, p *Product, baseURL string, dryRun bool) ([]aliImage, []string) {
	urls := append([]string{p.CoverURL}, p.ProductImages()...)
	urls = urls[:min(len(urls), aliMaxImages)]
	notes := make([]string, 0, 2)
	out := make([]aliImage, 0, len(urls))
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if strings.HasPrefix(u, "/") {
			u = strings.TrimSuffix(baseURL, "/") + u
		}
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			notes = append(notes, i18n.T(ctx, "website.ali_note_image_skipped", u))
			continue
		}
		if dryRun {
			out = append(out, aliImage{URL: u})
			continue
		}
		fileID, aliURL, err := s.aliUploadImageURL(ctx, c, u)
		if err != nil {
			// 单张失败不阻断发品，但必须让管理员看到——之前是静默 continue，
			// 表现就是「同步成功了，但阿里那边少了两张图」。
			notes = append(notes, i18n.T(ctx, "website.ali_note_image_failed", u, err.Error()))
			continue
		}
		out = append(out, aliImage{FileID: fileID, URL: aliURL})
	}
	return out, notes
}

// aliSubmitAdd 提交新发品。官方发布文档写的是 flat category_id / xml，
// 而同族的 get / render / update 在 API 页里都是嵌套 param_product_top_publish_request；
// 两种形态各试一次，失败时把两段网关原文都带回，联调时一眼看出该用哪种。
func aliSubmitAdd(ctx context.Context, c AliCredentials, catID, xml string) (map[string]json.RawMessage, error) {
	res, err := aliCall(ctx, c, "alibaba.icbu.product.schema.add", map[string]string{
		"category_id": catID, "xml": xml, "language": aliLanguageDefault,
	})
	if err == nil {
		return res, nil
	}
	if !aliIsInvalidParam(err) {
		return nil, err
	}
	res2, err2 := aliCallNested(ctx, c, "alibaba.icbu.product.schema.add", aliPublishParam{
		CatID: catID, XML: xml, Language: aliLanguageDefault,
	})
	if err2 != nil {
		return nil, fmt.Errorf("%w；改用嵌套入参同样失败：%v", err, err2)
	}
	return res2, nil
}

// aliIsInvalidParam 判断网关是否回了「非法参数」——只有这种才值得换一种入参形态重试。
func aliIsInvalidParam(err error) bool {
	var e *aliAPIError
	if !errors.As(err, &e) {
		return false
	}
	return strings.Contains(strings.ToLower(e.SubCode), "invalid-parameter")
}

// aliNumericID 报告一个 ID 是否像阿里侧的真实商品 ID（历史数据里存过 "ok" 这类占位）。
func aliNumericID(s string) bool {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return err == nil && n > 0
}

// aliUploadImageURL 下载一张图片并传到阿里图片银行。
func (s *Service) aliUploadImageURL(ctx context.Context, c AliCredentials, imgURL string) (int64, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imgURL, nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("download %s: %w", imgURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("download %s: status %d", imgURL, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return 0, "", err
	}
	name := imgURL
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.IndexByte(name, '?'); i >= 0 {
		name = name[:i]
	}
	if name == "" {
		name = "product.jpg"
	}
	return aliUploadImage(ctx, c, name, data)
}

// aliStringField 依次尝试多个键名取字符串值（对响应字段形态做温和容错）。
func aliStringField(res map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		if raw, ok := res[k]; ok {
			var s string
			if json.Unmarshal(raw, &s) == nil && s != "" {
				return s
			}
			// 数字形态（TOP 的 id 常是 number）
			var n json.Number
			if json.Unmarshal(raw, &n) == nil {
				return n.String()
			}
		}
	}
	return ""
}

// xmlAttrEscape 是发品值的转义别名：复用 public.go 的 xmlEscape（sitemap 同一套转义规则）。
func xmlAttrEscape(s string) string {
	return xmlEscape(s)
}

// SubmitAliSync 提交批量同步任务：params 只放商品 ID/类型/站点根地址（都不是敏感信息），
// 凭据由 job 执行时从库里解密读（job.Submit 会拒绝键名含敏感词的参数）。
func (s *Service) SubmitAliSync(ctx context.Context, ids []idgen.ID, market string, owner idgen.ID, baseURL string) (idgen.ID, error) {
	if len(ids) == 0 {
		return 0, response.BadRequest("website.product_ids_empty")
	}
	if _, err := aliMarketValue(market); err != nil {
		return 0, err
	}
	m := job.Default()
	if m == nil || !m.Ready() {
		return 0, response.BadRequest("website.ali_sync_unavailable")
	}
	stringIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			stringIDs = append(stringIDs, id.String())
		}
	}
	j, err := m.Submit(ctx, job.Submit{
		Type:  "website.ali-sync",
		Owner: owner,
		Params: map[string]string{
			"ids":     strings.Join(stringIDs, ","),
			"market":  market,
			"baseURL": strings.TrimSuffix(baseURL, "/"),
		},
	})
	if err != nil {
		return 0, err
	}
	return j.ID, nil
}

// RunAliSyncJob 是批量同步的 job 体：逐个发品、报进度、每个之间停 1 秒防频控。
// 注册在 plugin.go Register 里（闭包捕获 svc）。
func (s *Service) RunAliSyncJob(ctx context.Context, j *job.Job, report job.Report) (job.ResultFile, error) {
	var params struct {
		IDs     string `json:"ids"`
		Market  string `json:"market"`
		BaseURL string `json:"baseURL"`
	}
	if err := j.Decode(&params); err != nil {
		return job.ResultFile{}, job.Errorf("params: %v", err)
	}
	var ids []idgen.ID
	for _, part := range strings.Split(params.IDs, ",") {
		if v, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil && v > 0 {
			ids = append(ids, idgen.ID(v))
		}
	}
	if len(ids) == 0 {
		return job.ResultFile{}, job.Errorf("no products to sync")
	}
	ok, failed := 0, 0
	var lastErr string
	for i, id := range ids {
		if ctx.Err() != nil {
			return job.ResultFile{}, job.Errorf("canceled: %v", ctx.Err())
		}
		p, perr := s.ProductByID(ctx, id)
		name := id.String()
		if perr == nil {
			name = p.Title
		}
		aliPID, err := s.SyncProduct(ctx, id, params.Market, params.BaseURL)
		if err != nil {
			failed++
			lastErr = err.Error()
			report((i+1)*100/len(ids), fmt.Sprintf("失败 %s：%s", name, err.Error()))
		} else {
			ok++
			report((i+1)*100/len(ids), fmt.Sprintf("已同步 %s（%s）", name, aliPID))
		}
		select {
		case <-ctx.Done():
			return job.ResultFile{}, job.Errorf("canceled")
		case <-time.After(time.Second):
		}
	}
	if failed > 0 {
		return job.ResultFile{}, job.Errorf("同步完成：%d 成功 / %d 失败；最后失败原因：%s", ok, failed, lastErr)
	}
	report(100, fmt.Sprintf("同步完成：%d 成功 / 0 失败", ok))
	return job.ResultFile{}, nil
}
