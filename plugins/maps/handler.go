package maps

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
)

type handler struct{ svc *service }

// Config GET /plugins/maps/config
func (h *handler) Config(c *gin.Context) {
	cfg, err := h.svc.Config(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view(cfg))
}

// SaveConfig PUT /plugins/maps/config
func (h *handler) SaveConfig(c *gin.Context) {
	var in configReq
	if !response.BindJSON(c, &in) {
		return
	}
	v, err := h.svc.SaveConfig(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Regeo GET /plugins/maps/regeo?provider=amap|baidu|google&lng=&lat=
func (h *handler) Regeo(c *gin.Context) {
	provider := validProvider(c)
	lng, err1 := strconv.ParseFloat(c.Query("lng"), 64)
	lat, err2 := strconv.ParseFloat(c.Query("lat"), 64)
	if err1 != nil || err2 != nil {
		response.Fail(c, badRequest("maps.coords_invalid"))
		return
	}
	addr, err := h.svc.Regeo(c.Request.Context(), provider, lng, lat)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"address": addr})
}

// validProvider 校验 provider 参数并回传强类型；非法时直接回 400。
func validProvider(c *gin.Context) Provider {
	p := Provider(c.Query("provider"))
	if p != ProviderAMap && p != ProviderBaidu && p != ProviderGoogle {
		response.Fail(c, badRequest("maps.provider_invalid"))
		return ""
	}
	return p
}

// Geocode GET /plugins/maps/geocode?provider=…&address= —— 地址 → 坐标（登录即可用：
// 选点/搜索是所有表单用户都会碰的基础能力，键配置才需要 maps:config）。
func (h *handler) Geocode(c *gin.Context) {
	provider := validProvider(c)
	if provider == "" {
		return
	}
	lng, lat, err := h.svc.Geocode(c.Request.Context(), provider, c.Query("address"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"lng": lng, "lat": lat})
}

// Poi GET /plugins/maps/poi?provider=…&keywords=&city= —— POI 关键字检索（city 可选，百度空 = 全国）。
func (h *handler) Poi(c *gin.Context) {
	provider := validProvider(c)
	if provider == "" {
		return
	}
	places, err := h.svc.PlaceSearch(c.Request.Context(), provider, c.Query("keywords"), c.Query("city"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, places)
}

func badRequest(key string, args ...any) *response.Error {
	return response.BadRequest(key, args...)
}
