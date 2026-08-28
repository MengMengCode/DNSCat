package api

import (
	"net/http"
	"runtime"

	"dnscat/internal/buildinfo"

	"github.com/gin-gonic/gin"
)

type VersionHandler struct{}

func NewVersionHandler() *VersionHandler {
	return &VersionHandler{}
}

// GetVersion 返回当前运行二进制的构建信息，供控制台右上角展示。
//
// 版本号取自编译期注入（见 internal/buildinfo），因此它反映的是「正在跑的这个
// 进程」，而不是前端打包时的某个值——升级只换了二进制、控制台资源来自同一份
// 嵌入 FS 的场景下，两者必须一致才有意义。
//
// 该接口注册在需要鉴权的分组内：精确的版本号能让攻击者按已知 CVE 定位，
// 没有必要对未登录访客暴露，而右上角本身也只在登录后可见。
func (h *VersionHandler) GetVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"version":    buildinfo.Short(),
		"commit":     buildinfo.Commitish(),
		"date":       buildinfo.Date,
		"go_version": runtime.Version(),
		"platform":   runtime.GOOS + "/" + runtime.GOARCH,
	})
}
