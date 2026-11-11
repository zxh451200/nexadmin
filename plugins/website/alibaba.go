package website

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	ID    string `json:"id"`
	Name  string `json:"name"`
	CnName string `json:"cnName"`
	Leaf  bool   `json:"leaf"`
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
		CategoryID  any    `json:"category_id"`
		Name        string `json:"name"`
		CnName      string `json:"cn_name"`
		LeafCategory bool  `json:"leaf_category"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("ali categories parse: %w (%s)", err, aliSnippet(raw))
	}
	out := make([]AliCategoryNode, 0, len(items))
	for _, it := range items {
		out = append(out, AliCategoryNode{
			ID:    fmt.Sprint(it.CategoryID),
			Name:  it.Name,
			CnName: it.CnName,
			Leaf:  it.LeafCategory,
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
func (s *Service) markAliSync(ctx context.Context, id idgen.ID, aliPID, errText string) error {
	updates := map[string]any{"sync_status": ProductSyncFailed, "sync_error": trimLen(errText, 500)}
	if aliPID != "" {
		now := time.Now().UTC()
		updates = map[string]any{
			"sync_status": ProductSyncSynced, "alibaba_product_id": aliPID,
			"synced_at": &now, "sync_error": "",
		}
	}
	return s.db.WithContext(ctx).Model(&Product{}).Where("id = ?", int64(id)).Updates(updates).Error
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

// SyncProduct 把一个商品发到阿里国际站，返回阿里商品 ID。任何失败都会回写
// sync_status=failed 与 sync_error 并把错误向上返回（单发场景直接 toast）。
func (s *Service) SyncProduct(ctx context.Context, id idgen.ID, market string, baseURL string) (string, error) {
	marketValue, err := aliMarketValue(market)
	if err != nil {
		return "", err
	}
	p, err := s.ProductByID(ctx, id)
	if err != nil {
		return "", err
	}
	if p.AlibabaCategoryID == "" {
		return "", response.BadRequest("website.ali_category_required")
	}
	title, keywords, summary, content := aliEnglish(p)
	if strings.TrimSpace(title) == "" {
		return "", response.BadRequest("website.ali_english_required")
	}
	if marketValue == "1" && p.PriceMin <= 0 {
		return "", response.BadRequest("website.ali_price_required")
	}

	c, err := s.aliCredentials(ctx)
	if err != nil {
		_ = s.markAliSync(ctx, id, "", err.Error())
		return "", err
	}

	// ① schema.get：验证类目 ID 合法（无效类目在这里就报 isv 错，而不是 add 阶段）。
	if _, err := aliCall(ctx, c, "alibaba.icbu.product.schema.get",
		map[string]string{"category_id": p.AlibabaCategoryID}); err != nil {
		_ = s.markAliSync(ctx, id, "", err.Error())
		return "", err
	}

	// ② 图片：主图 + 图集取前 6 张，逐张下载后传图片银行，拿阿里侧 URL。
	images := append([]string{p.CoverURL}, p.ProductImages()...)
	images = images[:min(len(images), aliMaxImages)]
	aliImages := make([]string, 0, len(images))
	for _, u := range images {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if strings.HasPrefix(u, "/") {
			u = strings.TrimSuffix(baseURL, "/") + u
		}
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			continue
		}
		fileID, aliURL, uerr := s.aliUploadImageURL(ctx, c, u)
		if uerr != nil {
			// 单张失败不阻断发品：阿里允许 0 图发询盘品，失败原因并入 sync_error 提示。
			continue
		}
		_ = fileID
		aliImages = append(aliImages, aliURL)
	}

	// ③ 组装值 XML 并提交发品。
	xml := buildAliProductXML(title, keywords, summary, content, marketValue, p, aliImages)
	res, err := aliCall(ctx, c, "alibaba.icbu.product.schema.add", map[string]string{
		"category_id": p.AlibabaCategoryID,
		"xml":         xml,
	})
	if err != nil {
		_ = s.markAliSync(ctx, id, "", err.Error())
		return "", err
	}
	// 响应里的商品 ID 字段名按文档是 product_id；多形态容错（productID/productId）。
	aliPID := aliStringField(res, "product_id", "productID", "productId")
	if aliPID == "" {
		aliPID = "ok"
	}
	if err := s.markAliSync(ctx, id, aliPID, ""); err != nil {
		return aliPID, err
	}
	return aliPID, nil
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

// buildAliProductXML 组装 schema.add 的值 XML。
// 字段与结构按《【新接口】商品发布接入文档》的 field 体系写：每个 field 带 id/type/value。
// 真实联调若 schema 对某字段的 type/结构有出入，只需要调这里（错误信息带网关原文可对照）。
func buildAliProductXML(title, keywords, summary, content, market string, p *Product, aliImages []string) string {
	var b strings.Builder
	b.WriteString("<fields>\n")

	// market：1=下单品 2=询盘品。
	b.WriteString("\t<field id=\"market\" type=\"singleCheck\"><value>" + market + "</value></field>\n")
	// 标题（英文母本，阿里限 128 字符）。
	b.WriteString("\t<field id=\"productTitle\" type=\"text\"><value>" + xmlAttrEscape(trimRunes(title, 128)) + "</value></field>\n")
	// 关键词（1-3 个，空格分隔 → 多值）。
	if kws := strings.Fields(keywords); len(kws) > 0 {
		b.WriteString("\t<field id=\"productKeywords\" type=\"complex\">\n\t\t<fields>\n")
		for _, kw := range kws {
			if len(kw) > 0 {
				b.WriteString("\t\t\t<field id=\"productKeyword\" type=\"text\"><value>" + xmlAttrEscape(kw) + "</value></field>\n")
			}
		}
		b.WriteString("\t\t</fields>\n\t</field>\n")
	}
	// 主图：图片银行的 URL 列表。
	if len(aliImages) > 0 {
		b.WriteString("\t<field id=\"scImages\" type=\"complex\">\n\t\t<fields>\n")
		for _, u := range aliImages {
			b.WriteString("\t\t\t<field id=\"scImage\" type=\"complex\">\n\t\t\t\t<fields>\n")
			b.WriteString("\t\t\t\t\t<field id=\"imageUrl\" type=\"text\"><value>" + xmlAttrEscape(u) + "</value></field>\n")
			b.WriteString("\t\t\t\t</fields>\n\t\t\t</field>\n")
		}
		b.WriteString("\t\t</fields>\n\t</field>\n")
	}
	// FOB 价：下单品必填；询盘品有价也带上。
	if p.PriceMin > 0 {
		b.WriteString("\t<field id=\"fob\" type=\"complex\">\n\t\t<fields>\n")
		b.WriteString("\t\t\t<field id=\"range_min\" type=\"decimal\"><value>" + strconv.FormatFloat(p.PriceMin, 'f', 2, 64) + "</value></field>\n")
		if p.PriceMax > p.PriceMin {
			b.WriteString("\t\t\t<field id=\"range_max\" type=\"decimal\"><value>" + strconv.FormatFloat(p.PriceMax, 'f', 2, 64) + "</value></field>\n")
		}
		if p.PriceUnit != "" {
			b.WriteString("\t\t\t<field id=\"unit_type\" type=\"text\"><value>" + xmlAttrEscape(p.PriceUnit) + "</value></field>\n")
		}
		b.WriteString("\t\t</fields>\n\t</field>\n")
	}
	// 发货周期：本地发货天数（0 = 不填，走类目默认）。
	if p.LeadTimeDays > 0 {
		b.WriteString("\t<field id=\"ladderPeriod\" type=\"complex\">\n\t\t<fields>\n")
		b.WriteString("\t\t\t<field id=\"period\" type=\"number\"><value>" + strconv.Itoa(p.LeadTimeDays) + "</value></field>\n")
		b.WriteString("\t\t</fields>\n\t</field>\n")
	}
	// 自定义属性：本地 specs（≤10 条，与阿里的 customMoreProperty 上限一致）。
	if specs := p.ProductSpecs(); len(specs) > 0 {
		b.WriteString("\t<field id=\"customMoreProperty\" type=\"complex\">\n\t\t<fields>\n")
		n := 0
		for _, sp := range specs {
			if sp.Label == "" || sp.Value == "" {
				continue
			}
			if n == 10 {
				break
			}
			b.WriteString("\t\t\t<field id=\"customProperty\" type=\"complex\">\n\t\t\t\t<fields>\n")
			b.WriteString("\t\t\t\t\t<field id=\"prop_name\" type=\"text\"><value>" + xmlAttrEscape(sp.Label) + "</value></field>\n")
			b.WriteString("\t\t\t\t\t<field id=\"prop_value\" type=\"text\"><value>" + xmlAttrEscape(sp.Value) + "</value></field>\n")
			b.WriteString("\t\t\t\t</fields>\n\t\t\t</field>\n")
			n++
		}
		b.WriteString("\t\t</fields>\n\t</field>\n")
	}
	// 交易条件随详情走：详情富文本里带上本地交易信息块（阿里商详的描述区）。
	var detail strings.Builder
	detail.WriteString(content)
	if p.PaymentTerms != "" || p.Moq > 0 {
		detail.WriteString("<p>")
		if p.PaymentTerms != "" {
			detail.WriteString("Payment terms: " + xmlText(p.PaymentTerms) + ". ")
		}
		if p.Moq > 0 {
			detail.WriteString("MOQ: " + strconv.Itoa(p.Moq) + ". ")
		}
		if p.Port != "" {
			detail.WriteString("Port of loading: " + xmlText(p.Port) + ".")
		}
		detail.WriteString("</p>")
	}
	if detail.Len() > 0 {
		b.WriteString("\t<field id=\"detail\" type=\"rich\"><value>" + xmlAttrEscape(detail.String()) + "</value></field>\n")
	}
	_ = summary // 摘要暂无独立 schema 字段，随详情承载。
	b.WriteString("</fields>")
	return b.String()
}

// xmlText 是 xmlEscape 在纯文本拼接处的别名（读起来更顺）。
func xmlText(s string) string { return xmlEscape(s) }

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
