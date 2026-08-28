//go:build !embedui

package webui

import "io/fs"

// 默认构建不嵌入任何资源。
//
// 这样做的原因：web/dist 是构建产物且不入版本库，全新克隆里并不存在。
// 若无条件写 //go:embed dist，缺目录会直接导致编译失败，
// 连 go build ./... 与 go test ./... 都跑不起来。
var (
	embeddedFS   fs.FS = nil
	embeddedRoot       = "."
)
