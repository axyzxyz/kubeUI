// Package web 提供前端 SPA 静态资源的 embed 与回退服务。
//
// go:embed 不能跨模块,因此构建顺序为:先构建 frontend 产物,再由根 Makefile /
// Dockerfile 将 frontend/dist 拷贝到 backend/web/dist,最后编译本包。
// 本仓库随版本控制一个占位 index.html:未拷贝产物时构建仍可通过,运行期返回
// 明确的提示页(而非构建失败)。
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler 返回 SPA 静态资源处理器:
//   - 命中 dist 内文件(如 /assets/*.js)时按文件服务;
//   - 其余路径回退 index.html(vue-router history 模式);
//   - /api 前缀不属于前端路由,直接 404(防御未注册的 API 路径)。
func Handler() http.HandlerFunc {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}
	}
	fileServer := http.FileServer(http.FS(sub))
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":40400,"message":"not found"}`))
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if f, openErr := sub.Open(path); openErr == nil {
				_ = f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		index, readErr := fs.ReadFile(sub, "index.html")
		if readErr != nil {
			http.Error(w, "frontend assets not embedded", http.StatusNotFound)
			return
		}
		// gin NoRoute 会预设 404,回退 index 必须显式写 200,
		// 否则 SPA 路由响应会带 404 状态。
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	}
}
