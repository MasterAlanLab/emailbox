// Package webui 承载编译进二进制的前端产物。
//
// 前端产物由 scripts/build.sh 从 web/dist 复制进本包的 static/ 目录，
// 再由 go:embed 编译进二进制。放在这里而不是仓库根的 static/：
// go:embed 的路径不能越出所在包的目录，根目录的 static/ 对任何包都是不可见的。
package webui

import (
	"embed"
	"io/fs"
)

// assets 是前端构建产物。
//
// 用 all: 前缀是为了把 .gitkeep 这种点号开头的文件也匹配进来。没有它，
// 在「尚未构建前端」的状态下 static/ 里只剩 .gitkeep，embed 会以
// 「no matching files found」直接编译失败——而这恰恰是本仓库最常见的状态：
// make dev 下前端由 Vite 在 5173 提供，后端根本不需要产物在场，
// 刚 clone 完就跑 go test 的人也不该被一个前端构建挡住。
//
//go:embed all:static
var assets embed.FS

// FS 返回以 static/ 为根的前端产物文件系统。
func FS() (fs.FS, error) {
	return fs.Sub(assets, "static")
}

// Built 报告前端产物是否真的被构建进来了。
//
// 判据是 index.html 而不是「目录非空」：只含 .gitkeep 的空壳同样能通过 embed，
// 但它一个页面都服务不了。调用方据此区分「开发态，前端在 Vite 上」与
// 「打包产物缺前端」——后者在桌面版里必须是启动失败，因为桌面版没有 Vite 兜底。
func Built() bool {
	sub, err := FS()
	if err != nil {
		return false
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return false
	}
	return true
}
