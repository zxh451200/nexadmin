package plugin

import (
	"encoding/base64"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
)

type fakeModule struct{}

func (fakeModule) Name() string                          { return "fake" }
func (fakeModule) Register(*gin.RouterGroup, *core.Deps) {}

func base64Std(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
