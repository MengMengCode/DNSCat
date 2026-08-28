// Package webui 负责定位 Web 控制台的静态资源。
//
// 资源有两种来源：
//   - 编译期嵌入：用构建标签 embedui 编译时，web/dist 的副本被打进二进制。
//     发布产物走这条路径，使二进制自带控制台，不依赖进程的工作目录。
//   - 运行期磁盘：默认构建下从当前工作目录的 web/dist 读取，便于本地开发时
//     前端热更新后无需重新编译 Go。
//
// 之所以要嵌入：服务端原先只用相对路径 "web/dist" 读资源，而 systemd 单元的
// WorkingDirectory 指向数据目录，那里并没有前端产物，导致二进制安装的主控
// 控制台整个加载不出来。
package webui

import (
	"io/fs"
	"os"
)

// DiskDir 是磁盘回退目录，相对进程的当前工作目录。
const DiskDir = "web/dist"

// FS 返回可用的静态资源文件系统，以及一个用于日志的来源描述。
// 优先返回编译期嵌入的资源；不可用时回退到磁盘目录；两者都没有则返回 nil。
//
// 返回的 fs.FS 以前端产物根为起点，即可直接读取 "index.html"、"assets/..."。
func FS() (fs.FS, string) {
	if embeddedFS != nil {
		if sub, err := fs.Sub(embeddedFS, embeddedRoot); err == nil {
			// 校验 index.html 存在：构建时若漏拷前端产物，embed 目录可能只有占位文件，
			// 这时应当回退到磁盘而不是提供一个空白控制台。
			if _, err := fs.Stat(sub, "index.html"); err == nil {
				return sub, "embedded"
			}
		}
	}

	if st, err := os.Stat(DiskDir); err == nil && st.IsDir() {
		if _, err := os.Stat(DiskDir + "/index.html"); err == nil {
			return os.DirFS(DiskDir), "disk:" + DiskDir
		}
	}

	return nil, ""
}

// Embedded 报告本二进制是否带有嵌入的控制台资源。
func Embedded() bool {
	return embeddedFS != nil
}
