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
	provider := Provider(c.Query("provider"))
	if provider != ProviderAMap && provider != ProviderBaidu && provider != ProviderGoogle {
		response.Fail(c, badRequest("maps.provider_invalid"))
		return
	}
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

func badRequest(key string, args ...any) *response.Error {
	return response.BadRequest(key, args...)
}
