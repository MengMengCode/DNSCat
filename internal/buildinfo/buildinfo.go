// Package buildinfo 提供编译期注入的版本信息。
//
// 三个变量由链接器在构建时写入，见 .github/workflows/release.yml：
//
//	go build -ldflags "-X dnscat/internal/buildinfo.Version=v1.2.3 \
//	                   -X dnscat/internal/buildinfo.Commit=abc1234 \
//	                   -X dnscat/internal/buildinfo.Date=2026-08-26T00:00:00Z"
//
// 之所以走 ldflags 而不是在源码里写常量：版本号是发布动作的产物，
// 写进源码就必须在打 tag 前多提交一次「把版本改成 x.y.z」，
// 那个提交本身又不在 tag 里，容易出现二进制自报版本与 tag 不一致。
package buildinfo

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// 由 -ldflags -X 注入；未注入时走 fallback（见 Version()）。
var (
	Version = ""
	Commit  = ""
	Date    = ""
)

const devVersion = "dev"

// version 返回版本号。
//
// 未经 ldflags 注入时（例如开发机上直接 go build）退回读模块构建信息，
// 但只采信真实的 tag 版本：
//   - "(devel)" 与空串显然不是版本号；
//   - Go 会从 VCS 信息合成形如 v0.0.0-20260828094108-ef31024aa1c9 的伪版本，
//     它看着像正式版本号，实际只是「某个未发布的提交」。这类一并归为 dev，
//     真实提交号由 Commitish() 单独给出，不会丢信息。
func version() string {
	if Version != "" {
		return Version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		v := bi.Main.Version
		if v != "" && v != "(devel)" && !strings.HasPrefix(v, "v0.0.0-") {
			return v
		}
	}
	return devVersion
}

// commit 返回短提交号。未注入时尝试从 VCS 构建信息里取。
func commit() string {
	if Commit != "" {
		return Commit
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" {
				if len(s.Value) > 7 {
					return s.Value[:7]
				}
				return s.Value
			}
		}
	}
	return ""
}

// Short 返回形如 "v1.2.3" 或 "dev" 的版本号，供日志横幅与心跳上报使用。
func Short() string { return version() }

// Commitish 返回短提交号，未知时为空串。
func Commitish() string { return commit() }

// Full 返回完整的一行版本描述，供 --version 输出。
//
// 形如：dnscat-server v1.2.3 (abc1234, 2026-08-26T00:00:00Z, go1.25.0, linux/amd64)
func Full(program string) string {
	var b strings.Builder
	b.WriteString(program)
	b.WriteString(" ")
	b.WriteString(version())

	var parts []string
	if c := commit(); c != "" {
		parts = append(parts, c)
	}
	if Date != "" {
		parts = append(parts, Date)
	}
	parts = append(parts, runtime.Version())
	parts = append(parts, fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH))

	b.WriteString(" (")
	b.WriteString(strings.Join(parts, ", "))
	b.WriteString(")")
	return b.String()
}
