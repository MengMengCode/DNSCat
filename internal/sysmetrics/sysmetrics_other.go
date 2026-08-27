//go:build !linux

package sysmetrics

// collectPlatform 在非 Linux 平台返回零值快照。
// 边缘节点实际运行在 Linux 容器中；此实现只为让开发机（Windows/macOS）能编译整个模块。
func collectPlatform() Snapshot {
	return Snapshot{}
}
