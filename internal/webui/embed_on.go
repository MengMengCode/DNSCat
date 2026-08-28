//go:build embedui

package webui

import (
	"embed"
	"io/fs"
)

// 用构建标签 embedui 编译时把前端产物打进二进制。
//
// go:embed 只能引用本包目录下的路径，不能用 ../web/dist，
// 因此构建流程需先把 web/dist 拷到 internal/webui/dist，再执行：
//
//	go build -tags embedui ./cmd/server
//
// all: 前缀保证以点号或下划线开头的文件也被纳入。
//
//go:embed all:dist
var distFS embed.FS

var (
	embeddedFS   fs.FS = distFS
	embeddedRoot       = "dist"
)
