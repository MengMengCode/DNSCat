package model

import "time"

// 安全日志保留条数的默认值与允许区间。上限设一个硬顶，避免单域名把内存吃满。
const (
	DefaultLogRetentionLimit = 10000
	MinLogRetentionLimit     = 100
	MaxLogRetentionLimit     = 200000
)

// AXFR 策略取值。
const (
	AXFRPolicyDeny      = "deny"      // 一律拒绝区域传送（默认，权威服务器最安全的选择）
	AXFRPolicyAllowlist = "allowlist" // 仅允许白名单内的从服务器地址
)

// ANY 查询策略取值。
const (
	AnyPolicyAllow   = "allow"   // 正常返回全部 RRset（放大攻击风险最高）
	AnyPolicyMinimal = "minimal" // RFC 8482：返回极简 HINFO 应答
	AnyPolicyRefuse  = "refuse"  // 直接 REFUSED
)

// DomainSecurityPolicy 是单个托管区域（域名）的安全防护策略。
// 通过 Domain.SecurityPolicy 关联并随集群区域快照下发，因此主控与全部边缘节点
// 都会用同一份策略在解析入口处执行，而不是只在主控生效。
type DomainSecurityPolicy struct {
	ID       uint `gorm:"primaryKey" json:"id"`
	DomainID uint `gorm:"uniqueIndex;not null" json:"domain_id"`

	// Enabled 是该域名安全防护的总开关，关闭后下面所有规则一律不执行。
	Enabled bool `gorm:"default:true" json:"enabled"`

	// 1. 查询速率限制：按「客户端 IP + 本区域」做令牌桶限速。
	RateLimitEnabled bool `gorm:"default:true" json:"rate_limit_enabled"`
	RateLimitQPS     int  `gorm:"default:50" json:"rate_limit_qps"`
	RateLimitBurst   int  `gorm:"default:100" json:"rate_limit_burst"`

	// 2. DNS Flood / DDoS 防护：单 IP 短时间内超过阈值即临时封禁（丢包，不回应答）。
	FloodProtectionEnabled bool `gorm:"default:true" json:"flood_protection_enabled"`
	FloodThresholdQPS      int  `gorm:"default:200" json:"flood_threshold_qps"`
	FloodBanSeconds        int  `gorm:"default:300" json:"flood_ban_seconds"`

	// 3. RRL（Response Rate Limiting）：限制对同一客户端同类应答的发送速率，
	// 超限后按 SLIP 比例返回 TC=1 迫使正常解析器改用 TCP，其余丢弃。
	RRLEnabled         bool `gorm:"default:true" json:"rrl_enabled"`
	RRLResponsesPerSec int  `gorm:"default:100" json:"rrl_responses_per_sec"`
	RRLSlipRatio       int  `gorm:"default:2" json:"rrl_slip_ratio"`

	// 4. IP / ASN / 地区黑名单（AllowedIPs 为放行白名单，优先级最高）。
	BlacklistEnabled bool   `gorm:"default:false" json:"blacklist_enabled"`
	BlockedIPs       string `gorm:"type:text" json:"blocked_ips"`       // 换行/逗号分隔，支持 IP 与 CIDR
	BlockedASNs      string `gorm:"type:text" json:"blocked_asns"`     // 逗号分隔，如 4134,4837
	BlockedCountries string `gorm:"type:text" json:"blocked_countries"` // 逗号分隔国家码/大洲码，如 cn,us,EU
	AllowedIPs       string `gorm:"type:text" json:"allowed_ips"`      // 白名单：命中则跳过全部限制

	// 5. AXFR 限制：区域传送默认拒绝，避免整区数据被拖走。
	AXFRPolicy     string `gorm:"size:16;default:'deny'" json:"axfr_policy"`
	AXFRAllowedIPs string `gorm:"type:text" json:"axfr_allowed_ips"`

	// 7. 异常 QTYPE 防护：拒绝已废弃/易被滥用的查询类型。
	QTypeFilterEnabled bool   `gorm:"default:true" json:"qtype_filter_enabled"`
	BlockedQTypes      string `gorm:"type:text" json:"blocked_qtypes"` // 逗号分隔类型名，如 ANY,AXFR,MAILA

	// 8. ANY 限制：allow / minimal(RFC 8482) / refuse。
	AnyPolicy string `gorm:"size:16;default:'minimal'" json:"any_policy"`

	// 9. 单域名 QPS 限制：整个区域的总查询速率上限（防单域名被打爆拖垮全局）。
	ZoneQPSLimitEnabled bool `gorm:"default:false" json:"zone_qps_limit_enabled"`
	ZoneQPSLimit        int  `gorm:"default:2000" json:"zone_qps_limit"`

	// 10. Resolver / IP ACL：开启后只有白名单内的解析器地址可以查询本区域。
	ACLEnabled    bool   `gorm:"default:false" json:"acl_enabled"`
	ACLAllowedIPs string `gorm:"type:text" json:"acl_allowed_ips"`

	// 11. NXDOMAIN / 随机子域（水刑）攻击防护：单 IP 每分钟触发的 NXDOMAIN
	// 超过阈值即临时封禁，抑制伪随机子域枚举放大。
	NXProtectionEnabled bool `gorm:"default:true" json:"nx_protection_enabled"`
	NXThresholdPerMin   int  `gorm:"default:60" json:"nx_threshold_per_min"`
	NXBanSeconds        int  `gorm:"default:300" json:"nx_ban_seconds"`

	// 12. 查询日志与攻击统计。QueryLog 记录全部查询（高流量下慎开），
	// AttackLog 只记录被拦截的事件。
	QueryLogEnabled  bool `gorm:"default:false" json:"query_log_enabled"`
	AttackLogEnabled bool `gorm:"default:true" json:"attack_log_enabled"`

	// LogRetentionLimit 是该域名安全日志的保留条数上限。事件环按 FIFO 迭代：
	// 写满后丢弃最旧的一条再写入最新的一条，因此内存占用有确定上界。
	LogRetentionLimit int `gorm:"default:10000" json:"log_retention_limit"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DomainSecurityStat 是按域名累计的拦截计数，由安全引擎在内存中累加后
// 周期性落库，使攻击统计在进程重启后不归零。
type DomainSecurityStat struct {
	ID       uint `gorm:"primaryKey" json:"id"`
	DomainID uint `gorm:"uniqueIndex;not null" json:"domain_id"`

	TotalQueries     uint64 `gorm:"default:0" json:"total_queries"`
	BlockedTotal     uint64 `gorm:"default:0" json:"blocked_total"`
	BlockedRateLimit uint64 `gorm:"default:0" json:"blocked_rate_limit"`
	BlockedFlood     uint64 `gorm:"default:0" json:"blocked_flood"`
	BlockedRRL       uint64 `gorm:"default:0" json:"blocked_rrl"`
	BlockedBlacklist uint64 `gorm:"default:0" json:"blocked_blacklist"`
	BlockedACL       uint64 `gorm:"default:0" json:"blocked_acl"`
	BlockedAXFR      uint64 `gorm:"default:0" json:"blocked_axfr"`
	BlockedQType     uint64 `gorm:"default:0" json:"blocked_qtype"`
	BlockedANY       uint64 `gorm:"default:0" json:"blocked_any"`
	BlockedZoneQPS   uint64 `gorm:"default:0" json:"blocked_zone_qps"`
	BlockedNXAbuse   uint64 `gorm:"default:0" json:"blocked_nx_abuse"`
	NXDomainCount    uint64 `gorm:"default:0" json:"nxdomain_count"`

	UpdatedAt time.Time `json:"updated_at"`
}

// DefaultSecurityPolicy 返回某域名的出厂安全策略。
// 默认只启用无副作用的防护（速率限制 / Flood / RRL / 异常 QTYPE / ANY 极简 /
// NXDOMAIN 防护 / 攻击日志），黑名单与 ACL 这类需要人工配置的默认关闭，
// 避免刚建域名就把正常解析拦掉。
func DefaultSecurityPolicy(domainID uint) DomainSecurityPolicy {
	return DomainSecurityPolicy{
		DomainID: domainID,
		Enabled:  true,

		RateLimitEnabled: true,
		RateLimitQPS:     50,
		RateLimitBurst:   100,

		FloodProtectionEnabled: true,
		FloodThresholdQPS:      200,
		FloodBanSeconds:        300,

		RRLEnabled:         true,
		RRLResponsesPerSec: 100,
		RRLSlipRatio:       2,

		BlacklistEnabled: false,

		AXFRPolicy: AXFRPolicyDeny,

		QTypeFilterEnabled: true,
		// 默认拦截已废弃或几乎只用于放大攻击的类型；ANY 单独由 AnyPolicy 控制。
		BlockedQTypes: "MAILA,MAILB,MD,MF,NULL,SPF,WKS",

		AnyPolicy: AnyPolicyMinimal,

		ZoneQPSLimitEnabled: false,
		ZoneQPSLimit:        2000,

		ACLEnabled: false,

		NXProtectionEnabled: true,
		NXThresholdPerMin:   60,
		NXBanSeconds:        300,

		QueryLogEnabled:   false,
		AttackLogEnabled:  true,
		LogRetentionLimit: DefaultLogRetentionLimit,
	}
}
