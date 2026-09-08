package server

import (
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/labstack/echo/v5"

	"emailbox/pkg/webui"
)

// staticFilePath 把 URL 路径映射成嵌入文件系统里的路径。
//
// 先以根路径清洗再去掉前导斜杠：path.Clean("/"+p) 保证结果里不会残留 ".."，
// 于是 "/assets/../../etc/passwd" 会被折叠成 "/etc/passwd" 而不是逃出去。
// io/fs 本身也会拒绝非法路径，但这里不依赖那一层——清洗是这个函数的职责，
// 换一个 fs 实现不该让路径穿透重新变得可能。
func staticFilePath(urlPath string) string {
	return strings.TrimPrefix(path.Clean("/"+urlPath), "/")
}

// regularFile 判断嵌入文件系统里的路径是否为可直接返回的普通文件。
// 目录必须排除：FileFS 对目录会回落到其中的 index.html，
// 于是 /assets/.. 这类请求会拿到带一年强缓存的首页。
func regularFile(fsys fs.FS, name string) bool {
	if name == "" {
		return false
	}
	info, err := fs.Stat(fsys, name)
	return err == nil && info.Mode().IsRegular()
}

// setupStaticFiles 把嵌入的前端产物挂到 echo 上。
//
// 前端产物没被构建进来时只告警不失败：make dev 下前端由 Vite 提供，
// 后端不需要产物在场。桌面版没有这个兜底，缺产物在 New 里就已经报错了。
func setupStaticFiles(e *echo.Echo) {
	if !webui.Built() {
		slog.Warn("二进制内未嵌入前端产物，跳过静态文件服务", "hint", "开发模式下前端由 Vite 提供；打包请运行 make build")
		return
	}
	fsys, err := webui.FS()
	if err != nil {
		slog.Error("读取嵌入的前端产物失败，跳过静态文件服务", "error", err)
		return
	}

	// 带哈希的资源文件，长期强缓存。
	e.GET("/assets/*", func(c *echo.Context) error {
		name := staticFilePath(c.Request().URL.Path)
		if !regularFile(fsys, name) {
			return echo.NewHTTPError(http.StatusNotFound, "File not found")
		}
		c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")

		return c.FileFS(name, fsys)
	})

	// favicon（短期缓存）。
	e.GET("/favicon.ico", func(c *echo.Context) error {
		c.Response().Header().Set("Cache-Control", "public, max-age=86400")
		return c.FileFS("favicon.ico", fsys)
	})

	// 网站图标 SVG（长期缓存）。
	e.GET("/vite.svg", func(c *echo.Context) error {
		c.Response().Header().Set("Cache-Control", "public, max-age=604800")
		return c.FileFS("vite.svg", fsys)
	})

	// SPA 路由：非 API 的请求都回落到 index.html。
	e.GET("/*", func(c *echo.Context) error {
		urlPath := c.Request().URL.Path
		if strings.HasPrefix(urlPath, "/api") {
			return echo.NewHTTPError(http.StatusNotFound, "API endpoint not found")
		}

		name := staticFilePath(urlPath)
		if urlPath == "/" {
			name = "index.html"
		}
		if regularFile(fsys, name) {
			// HTML 走协商缓存，否则改版后用户会一直拿到旧壳。
			if path.Ext(name) == ".html" {
				c.Response().Header().Set("Cache-Control", "no-cache")
			}

			return c.FileFS(name, fsys)
		}

		c.Response().Header().Set("Cache-Control", "no-cache")

		return c.FileFS("index.html", fsys)
	})
}
