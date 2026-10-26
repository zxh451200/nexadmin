package website

import (
	"compress/gzip"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// 官网公开路由的 gzip 压缩：HTML/CSS/JS/SVG/JSON/XML 文本资源压完通常只有原大小的 1/4，
// 生产环境走公网时首屏收益明显；图片、字体、插件 zip 包本身已压缩，跳过省 CPU。
// 只挂在官网 pub 路由组上，不碰框架引擎的 /ws 与 SSE 流。

var gzipPool = sync.Pool{
	New: func() any {
		w, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed)
		return w
	},
}

// wantsGzip 按路径判断是否压缩：文本扩展名一律压；无扩展名的是页面（HTML），但
// /plugins/<id>/download* 是现场打包的 zip，排除。
func wantsGzip(p string) bool {
	if strings.Contains(p, "/download") {
		return false
	}
	switch strings.ToLower(path.Ext(p)) {
	case ".css", ".js", ".svg", ".json", ".xml", ".txt", ".html", ".htm":
		return true
	case "":
		return true
	default:
		return false
	}
}

type gzipWriter struct {
	gin.ResponseWriter
	gz      *gzip.Writer
	written bool
}

func (w *gzipWriter) Write(b []byte) (int, error) {
	w.written = true
	return w.gz.Write(b)
}

// gin 的字符串渲染走 WriteString；gzip.Writer 没有导出该方法，转 Write 兜住，避免绕过压缩。
func (w *gzipWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *gzipWriter) close() error {
	if !w.written {
		return nil
	}
	return w.gz.Close()
}

func gzipFilter() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Next()
			return
		}
		if !strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") ||
			c.GetHeader("Range") != "" || !wantsGzip(c.Request.URL.Path) {
			c.Next()
			return
		}
		gz := gzipPool.Get().(*gzip.Writer)
		gz.Reset(c.Writer)
		h := c.Writer.Header()
		h.Set("Content-Encoding", "gzip")
		h.Add("Vary", "Accept-Encoding")
		h.Del("Content-Length")
		gw := &gzipWriter{ResponseWriter: c.Writer, gz: gz}
		c.Writer = gw
		c.Next()
		if err := gw.close(); err != nil {
			// 尾部落盘失败已无法补救（响应头已发出），只能丢弃；连接由 gin 正常收尾。
			_ = err
		}
		gzipPool.Put(gz)
	}
}
