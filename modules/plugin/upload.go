package plugin

import (
	"io"
	"mime/multipart"
	"os"

	"github.com/gin-gonic/gin"

	"nexadmin/core/plugin/market"
	"nexadmin/core/response"
)

const maxUploadBytes = 64 << 20

func saveUpload(c *gin.Context, dst *os.File, fh *multipart.FileHeader, limit int64) error {
	src, err := fh.Open()
	if err != nil {
		return response.BadRequest("plugin.upload_file_required").Wrap(err)
	}
	defer src.Close()
	n, err := io.Copy(dst, io.LimitReader(src, limit+1))
	if err != nil {
		return response.BadRequest("plugin.upload_failed").Wrap(err)
	}
	if n > limit {
		return response.BadRequest("plugin.upload_too_large", limit>>20)
	}
	return nil
}

func startUploadZip(root, zipPath string) string {
	return marketRunner.Run("install", "upload", func(x *market.Ctx) error {
		return runUpload(root, zipPath, x, productionInstallOperations())
	})
}
