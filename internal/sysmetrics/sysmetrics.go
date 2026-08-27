// Package sysmetrics 采集边缘节点所在主机的真实资源使用率。
//
// 设计取舍：直接读取内核提供的 /proc 与 statfs，不引入第三方依赖，
// 以免为节点二进制增加体积与供应链风险。非 Linux 平台返回零值快照
// （见 sysmetrics_other.go），保证开发机（Windows/macOS）仍可编译。
package sysmetrics

import "sync"

// Snapshot 是一次系统资源采样结果。
// 百分比字段取值 0-100；网络速率单位为字节/秒。
type Snapshot struct {
	CPUPercent  float64
	MemPercent  float64
	DiskPercent float64
	NetRxBps    int64
	NetTxBps    int64
}

// CPU 与网络都是内核累计计数器，必须由两次采样求差值才能得到「当前速率」。
// 这里保存上一次的累计值，因此首次调用时 CPU 与网络为 0，从第二次起才有意义。
var mu sync.Mutex

// Collect 采集当前主机的资源使用率。并发安全。
func Collect() Snapshot {
	mu.Lock()
	defer mu.Unlock()
	return collectPlatform()
}
