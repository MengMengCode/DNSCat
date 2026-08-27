package acme

import "strings"

// Provider 描述一家 ACME 证书颁发机构（CA）的接入参数。
//
// 之所以需要显式登记 Staging 地址：真实签发受 CA 速率限制约束
// （Let's Encrypt 生产环境同一组域名每周仅允许 5 张重复证书），
// 调试阶段必须切到 staging 才不会把生产配额烧掉。
type Provider struct {
	// Name 是展示与存储用的服务商标识，与前端下拉框、数据库 provider 字段一致。
	Name string
	// DirectoryURL 生产环境 ACME Directory 地址。
	DirectoryURL string
	// StagingURL 测试环境 Directory 地址；为空表示该 CA 未提供 staging。
	StagingURL string
	// RequiresEAB 表示该 CA 强制要求 External Account Binding。
	// ZeroSSL 与 Google Trust Services 必须先在其控制台获取 EAB 凭证才能建账户，
	// 缺失凭证时应在提交前就拦截，而不是等 CA 返回 403。
	RequiresEAB bool
}

// 四家受支持的 ACME CA。Directory 地址取自各家官方文档。
var providers = []Provider{
	{
		Name:         "Let's Encrypt",
		DirectoryURL: "https://acme-v02.api.letsencrypt.org/directory",
		StagingURL:   "https://acme-staging-v02.api.letsencrypt.org/directory",
		RequiresEAB:  false,
	},
	{
		Name:         "ZeroSSL",
		DirectoryURL: "https://acme.zerossl.com/v2/DV90",
		StagingURL:   "", // ZeroSSL 未提供公开 staging 环境
		RequiresEAB:  true,
	},
	{
		Name:         "Buypass",
		DirectoryURL: "https://api.buypass.com/acme/directory",
		StagingURL:   "https://api.test4.buypass.no/acme/directory",
		RequiresEAB:  false,
	},
}

// DefaultProviderName 是未指定服务商时的兜底选择。
const DefaultProviderName = "Let's Encrypt"

// LookupProvider 按名称查找服务商，采用宽松匹配（忽略大小写与空白），
// 以兼容历史数据中可能存在的大小写差异。未命中时回退到 Let's Encrypt。
func LookupProvider(name string) Provider {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized != "" {
		for _, p := range providers {
			if strings.ToLower(p.Name) == normalized {
				return p
			}
		}
		// 二次宽松匹配：允许「Google」命中「Google PKI」这类简写。
		for _, p := range providers {
			if strings.Contains(strings.ToLower(p.Name), normalized) ||
				strings.Contains(normalized, strings.ToLower(p.Name)) {
				return p
			}
		}
	}
	return providers[0]
}

// ListProviders 返回全部受支持的服务商，供 API 暴露给前端渲染选择器，
// 使前端不必硬编码 CA 列表与 EAB 必填规则。
func ListProviders() []Provider {
	out := make([]Provider, len(providers))
	copy(out, providers)
	return out
}

// ResolveDirectoryURL 决定本次签发实际使用的 Directory 地址，优先级如下：
//  1. 显式覆盖地址（系统设置 acme_ca_url 非空时）——便于对接私有 CA 或 Pebble 测试服务；
//  2. staging 开启且该 CA 提供 staging 时使用 staging；
//  3. 该 CA 的生产地址。
//
// 覆盖地址优先级最高是有意为之：私有 PKI / 内网测试场景需要完全接管此项。
func ResolveDirectoryURL(p Provider, override string, staging bool) string {
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		return trimmed
	}
	if staging && p.StagingURL != "" {
		return p.StagingURL
	}
	return p.DirectoryURL
}
