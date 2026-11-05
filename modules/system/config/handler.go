package config

import (
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// platformCaller 报告调用方是否为平台级。判据单源在 tenant.IsPlatformCallerID（经 core.IsPlatformCaller），
// 这里不再自己写一份 `Super || tid == DefaultID`——中间件、载荷、通知收件人与这一处必须同一条线。
func platformCaller(c *gin.Context) bool { return core.IsPlatformCaller(c) }

// secretKeyHints 是设置分区里凡是命中就按密钥处理的字段名片段。比对前去掉下划线/连字符并转小写，
// 所以 smtpPassword、access_key、accessKey、appSecret 都算命中。
// 这些值不属于任何租户，却决定整个部署往哪发信、谁能登进来：租户管理员的界面不需要看见它们。
var secretKeyHints = []string{"password", "passwd", "secret", "apikey", "accesskey", "privatekey", "token", "keya", "keyb", "keyc"}

// isSecretKey 报告字段名是否按密钥处理。
func isSecretKey(k string) bool {
	lower := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(k))
	for _, hint := range secretKeyHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

// restrictAI 把 GET /settings 里的 ai 分区缩减成租户可见的最小信息（不含地址与密钥）。
func restrictAI(all map[string]any) {
	if ai, ok := all["ai"].(map[string]any); ok {
		all["ai"] = map[string]any{"enabled": ai["enabled"], "model": ai["model"]}
	}
}

// restrictSecrets 递归抹掉非平台调用方不该看到的密钥字段（保留键名与结构，值置空）：
// 界面还能渲染「已配置/未配置」，但拿不到真凭据。整分区不返回会让设置页变成一片空白，
// 那是另一种「入口蒸发」。
func restrictSecrets(v any) {
	restrictSecretsIn(v, false)
}

// restrictSecretsIn 递归抹密钥字段；robotURL 表示当前正处在 notify.robots 列表里。
//
// 服务层的 maskSecrets 对所有人生效，覆盖 password / secretKey / apiKey / clientSecret 四个名字；
// 这里补的是它盖不到的两类，且只对非平台调用方生效：accessKey 与 token 类命名，
// 以及 notify.robots[].url——钉钉 / 飞书 / 企业微信的 webhook 地址自带 access_key 或 access_token，
// 拿到 URL 就能往群里发消息，而字段名是 url，按名字表永远漏。列表外的 url（site.logoUrl 之类）
// 是普通展示字段，不许误伤。
func restrictSecretsIn(v any, robotURL bool) {
	switch node := v.(type) {
	case map[string]any:
		for k, child := range node {
			if k == "robots" {
				restrictSecretsIn(child, true)
				continue
			}
			if isSecretKey(k) || (robotURL && k == "url") {
				// 字符串值换成与 ai 分区同一个掩码（界面据此仍能显示「已配置」）；
				// 非字符串的密钥形态（对象/数组）没有可展示的意义，删键。
				if s, ok := child.(string); ok && s != "" {
					node[k] = secretMask
				} else {
					delete(node, k)
				}
				continue
			}
			restrictSecretsIn(child, false)
		}
	case []any:
		for _, item := range node {
			restrictSecretsIn(item, robotURL)
		}
	}
}

// Handler 是参数与设置页的 HTTP 层。
type Handler struct{ svc *Service }

func pathID(c *gin.Context) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").Wrap(err))
		return 0, false
	}
	return id, true
}

func done(c *gin.Context, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// List GET /configs
func (h *Handler) List(c *gin.Context) {
	var q ListQuery
	if !response.BindQuery(c, &q) {
		return
	}
	page, size := response.ParsePage(c)
	list, total, err := h.svc.List(c.Request.Context(), q, page, size)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// Get GET /configs/:id
func (h *Handler) Get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	v, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Create POST /configs
func (h *Handler) Create(c *gin.Context) {
	var in Input
	if !response.BindJSON(c, &in) {
		return
	}
	v, err := h.svc.Create(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Update PUT /configs/:id
func (h *Handler) Update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in Input
	if !response.BindJSON(c, &in) {
		return
	}
	v, err := h.svc.Update(c.Request.Context(), id, in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// DeleteOne DELETE /configs/:id
func (h *Handler) DeleteOne(c *gin.Context) {
	if id, ok := pathID(c); ok {
		done(c, h.svc.Delete(c.Request.Context(), []idgen.ID{id}))
	}
}

// DeleteMany DELETE /configs?ids=1,2
func (h *Handler) DeleteMany(c *gin.Context) {
	var ids []idgen.ID
	for _, s := range strings.Split(c.Query("ids"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		id, err := idgen.Parse(s)
		if err != nil {
			response.Fail(c, response.BadRequest("error.bad_request").Wrap(err))
			return
		}
		ids = append(ids, id)
	}
	done(c, h.svc.Delete(c.Request.Context(), ids))
}

// GetUpload GET /configs/upload 与 GET /config/upload（后者供上传组件读取，登录即可）
func (h *Handler) GetUpload(c *gin.Context) {
	v, err := h.svc.Upload(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// SaveUpload PUT /configs/upload
func (h *Handler) SaveUpload(c *gin.Context) {
	var in UploadSettings
	if !response.BindJSON(c, &in) {
		return
	}
	v, err := h.svc.SaveUpload(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// UploadTypes GET /configs/upload-types
func (h *Handler) UploadTypes(c *gin.Context) { response.OK(c, uploadTypeOptions) }

// RefreshCache POST /configs/refresh-cache：参数每次直接读库；唯一的进程内缓存是上传限制（几秒短缓存），这里立即丢弃它。
func (h *Handler) RefreshCache(c *gin.Context) {
	h.svc.invalidateUpload()
	response.OK(c, nil)
}

// ---- 设置页 ----

// Settings GET /settings
func (h *Handler) Settings(c *gin.Context) {
	v, err := h.svc.AllSettings(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	if !platformCaller(c) {
		restrictAI(v)
		// 其余分区的凭据同样不外泄：security/auth/sms/notify 里的密码与密钥是部署级的。
		restrictSecrets(v)
	}
	response.OK(c, v)
}

// PublicSettings GET /settings/public（匿名）
func (h *Handler) PublicSettings(c *gin.Context) {
	v, err := h.svc.Public(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// SaveSection PUT /settings/:section
// 整表 sys_setting 没有 tenant_id：任何分区都是部署级的一行，所以写入口在路由上统一挂
// core.RequirePlatform()（module.go），这里不再按分区名特判——原先只有 ai 分区受限，
// security / auth / sms / notify 谁都能改，一个租户把 maxAttempts 设成 1 就能把别人全锁在门外。
func (h *Handler) SaveSection(c *gin.Context) {
	var body map[string]any
	if !response.BindJSON(c, &body) {
		return
	}
	v, err := h.svc.SaveSection(c.Request.Context(), c.Param("section"), body)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// TestSection POST /settings/:section/test
// 同 SaveSection：测试一把也要拿部署级凭据去连外部服务，写入口已经整组只认平台调用方。
func (h *Handler) TestSection(c *gin.Context) {
	var body map[string]any
	if !response.BindJSON(c, &body) {
		return
	}
	v, err := h.svc.TestSection(c.Request.Context(), c.Param("section"), body)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}
