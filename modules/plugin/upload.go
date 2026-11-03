package plugin

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"

	"nexadmin/core/plugin"
	"nexadmin/core/plugin/market"
	"nexadmin/core/response"
)

// 本地安装包上传：离线 / 私有源环境拿不到 GitHub zipball，市场安装帮不上忙；
// 直接传 zip 走与市场安装相同的安装 → 重建 → 自重启链路。

// maxUploadBytes 是上传安装包的大小上限：现有市场插件包都在 10MB 量级，64MB 足够宽容。
const maxUploadBytes = 64 << 20

// saveUpload 把 multipart 文件落盘（边拷边限长，不信任 Content-Length）。
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

// zipPluginID 从安装包里直接读 plugin.yaml 的 id（解压安装前先拿到，ChangeSources 回滚才有着落）：
// 只认根、一层或两层目录下的 plugin.yaml，与 locatePackage 的查找口径一致。
func zipPluginID(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	for _, f := range zr.File {
		n := strings.ReplaceAll(f.Name, "\\", "/")
		if !strings.HasSuffix(n, "/plugin.yaml") && n != "plugin.yaml" {
			continue
		}
		if depth := strings.Count(strings.Trim(n, "/"), "/"); depth > 2 {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		var man struct {
			ID string `yaml:"id"`
		}
		err = yaml.NewDecoder(rc).Decode(&man)
		rc.Close()
		if err != nil || man.ID == "" {
			continue
		}
		if !plugin.ValidID(man.ID) {
			return "", fmt.Errorf("安装包里的插件 id %q 不合法", man.ID)
		}
		return man.ID, nil
	}
	return "", fmt.Errorf("安装包里找不到 plugin.yaml（或其中缺少 id）")
}

// startUploadZip 本地上传安装全流程：解包安装 → 重建 → 自重启。
// 与市场安装一样包在 ChangeSources 里：重建失败时恢复安装前的源码树。
// 已装同名插件时直接覆盖（force 语义）：本地上传没有「版本对不比对」可言，以包为准。
func startUploadZip(root, zipPath string) string {
	return marketRunner.Run("install", "upload", func(x *market.Ctx) error {
		id, err := zipPluginID(zipPath)
		if err != nil {
			return err
		}
		err = market.ChangeSources(root, id, func() error {
			x.Log("安装上传的插件包 %s…", id)
			withWeb := market.HasWeb(zipPath)
			if _, err := market.InstallSub(root, zipPath, "", false); err != nil {
				return err
			}
			x.Log("插件 %s 已装入源码树，开始重建…", id)
			if err := market.Rebuild(context.Background(), root, withWeb, x.LogPlain); err != nil {
				return fmt.Errorf("重建失败: %w", err)
			}
			return nil
		})
		if err != nil {
			x.Log("失败，源码已恢复到安装前状态；可修正安装包后重试")
			return err
		}
		return triggerRestart(root, x)
	})
}
