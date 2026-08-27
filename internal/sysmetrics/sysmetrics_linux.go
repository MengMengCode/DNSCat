//go:build linux

package sysmetrics

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// 上一次采样的累计值，用于把内核累计计数器换算成当前速率。
var (
	prevCPUBusy  uint64
	prevCPUTotal uint64
	prevNetRx    uint64
	prevNetTx    uint64
	prevNetAt    time.Time
)

// collectPlatform 由 Collect 在持锁状态下调用。
func collectPlatform() Snapshot {
	s := Snapshot{}
	s.CPUPercent = readCPUPercent()
	s.MemPercent = readMemPercent()
	s.DiskPercent = readDiskPercent("/")
	s.NetRxBps, s.NetTxBps = readNetBps()
	return s
}

// readCPUPercent 解析 /proc/stat 首行的累计 jiffies，用与上次采样的差值计算占用率。
func readCPUPercent() float64 {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return 0
	}
	fields := strings.Fields(scanner.Text())
	// 形如：cpu user nice system idle iowait irq softirq steal guest guest_nice
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0
	}

	var total, idle uint64
	for i, raw := range fields[1:] {
		v, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			continue
		}
		total += v
		// idle(第 4 列) 与 iowait(第 5 列) 都视为空闲
		if i == 3 || i == 4 {
			idle += v
		}
	}
	busy := total - idle

	prevBusy, prevTotal := prevCPUBusy, prevCPUTotal
	prevCPUBusy, prevCPUTotal = busy, total

	// 首次采样没有基准，或计数器回绕时不产出数值。
	if prevTotal == 0 || total <= prevTotal || busy < prevBusy {
		return 0
	}
	pct := float64(busy-prevBusy) / float64(total-prevTotal) * 100
	return clampPercent(pct)
}

// readMemPercent 用 MemTotal 与 MemAvailable 计算已用内存百分比。
// MemAvailable 是内核给出的「可分配内存」估计值，比 MemFree 更贴近实际可用量。
func readMemPercent() float64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()

	var totalKB, availKB uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			totalKB = parseMeminfoKB(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			availKB = parseMeminfoKB(line)
		}
		if totalKB > 0 && availKB > 0 {
			break
		}
	}
	if totalKB == 0 || availKB > totalKB {
		return 0
	}
	return clampPercent(float64(totalKB-availKB) / float64(totalKB) * 100)
}

// parseMeminfoKB 取 "MemTotal:  16337072 kB" 中的数值（单位 kB）。
func parseMeminfoKB(line string) uint64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	v, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// readDiskPercent 用 statfs 计算挂载点已用空间百分比，算法与 df 一致：
// 分母取「已用 + 非特权可用」，避免把预留给 root 的空间算成可用容量。
func readDiskPercent(path string) float64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	if st.Blocks == 0 {
		return 0
	}
	used := st.Blocks - st.Bfree
	denom := used + st.Bavail
	if denom == 0 {
		return 0
	}
	return clampPercent(float64(used) / float64(denom) * 100)
}

// readNetBps 汇总 /proc/net/dev 各网卡收发字节数，与上次采样求差得到字节/秒。
// 跳过回环与容器虚拟网卡，避免把本机内部流量计入节点带宽。
func readNetBps() (int64, int64) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	var rx, tx uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		idx := strings.IndexByte(line, ':')
		if idx <= 0 {
			continue // 前两行是表头，没有冒号
		}
		iface := strings.TrimSpace(line[:idx])
		if skipInterface(iface) {
			continue
		}
		fields := strings.Fields(line[idx+1:])
		// 冒号后依次是 16 列统计，第 1 列为接收字节、第 9 列为发送字节。
		if len(fields) < 9 {
			continue
		}
		if v, err := strconv.ParseUint(fields[0], 10, 64); err == nil {
			rx += v
		}
		if v, err := strconv.ParseUint(fields[8], 10, 64); err == nil {
			tx += v
		}
	}

	now := time.Now()
	prevRx, prevTx, prevAt := prevNetRx, prevNetTx, prevNetAt
	prevNetRx, prevNetTx, prevNetAt = rx, tx, now

	// 首次采样无基准；计数器回绕（重启网卡等）时同样跳过本轮。
	if prevAt.IsZero() || rx < prevRx || tx < prevTx {
		return 0, 0
	}
	elapsed := now.Sub(prevAt).Seconds()
	if elapsed <= 0 {
		return 0, 0
	}
	return int64(float64(rx-prevRx) / elapsed), int64(float64(tx-prevTx) / elapsed)
}

// skipInterface 过滤回环与容器/虚拟网卡。
func skipInterface(name string) bool {
	if name == "lo" {
		return true
	}
	for _, prefix := range []string{"veth", "docker", "br-", "virbr", "cni", "flannel", "tun", "tap"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func clampPercent(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
