package model

import (
	"crypto/rand"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// NewUUID 生成 RFC 4122 v4 UUID（仅用 crypto/rand，无需引入外部依赖）。
// 用于给对外暴露的资源标识分配不可枚举的 ID，避免 URL 里出现自增主键。
func NewUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

type User struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	Username     string         `gorm:"size:64;uniqueIndex;not null" json:"username"`
	Email        string         `gorm:"size:128;uniqueIndex;not null" json:"email"`
	PasswordHash string         `gorm:"size:255;not null" json:"-"`
	Role         Role           `gorm:"size:16;default:'user'" json:"role"`
	APIKey       string         `gorm:"size:128;uniqueIndex" json:"api_key"`
	// Language / Theme 是跟随账号的界面偏好：登录后以账号里的值为准，
	// 这样同一个账号在任意浏览器登录都能保持一致的语言与主题。
	// 未登录时前端只用浏览器本地存储，不涉及这两个字段。
	// 空值表示该账号尚未保存过偏好，此时沿用前端当前的本地设置。
	Language     string         `gorm:"size:16" json:"language"`
	Theme        string         `gorm:"size:16" json:"theme"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

type DomainStatus string

const (
	DomainStatusActive  DomainStatus = "active"
	DomainStatusPending DomainStatus = "pending"
	DomainStatusPaused  DomainStatus = "paused"
)

type Domain struct {
	ID            uint           `gorm:"primaryKey" json:"id"`
	UserID        uint           `gorm:"index;not null" json:"user_id"`
	Name          string         `gorm:"size:255;uniqueIndex;not null" json:"name"` // example.com
	Status        DomainStatus   `gorm:"size:32;default:'active'" json:"status"`
	NSStatus      string         `gorm:"size:32;default:'pending'" json:"ns_status"` // verified, pending
	PrimaryNS     string         `gorm:"size:255" json:"primary_ns"`
	AdminEmail    string         `gorm:"size:255" json:"admin_email"`
	SOARefresh    uint32         `gorm:"default:10000" json:"soa_refresh"`
	SOARetry      uint32         `gorm:"default:2400" json:"soa_retry"`
	SOAExpire     uint32         `gorm:"default:604800" json:"soa_expire"`
	SOAMinimum    uint32         `gorm:"default:300" json:"soa_minimum"`
	SOASerial     uint32         `gorm:"default:1" json:"soa_serial"`
	DNSSECEnabled bool           `gorm:"default:false" json:"dnssec_enabled"`
	RecordCount   int64          `gorm:"-" json:"record_count"`
	// NSRecords 是该域名 apex 上实际配置的全部权威 NS（列表接口填充，不落库）。
	// 管理员可能配置多台权威 NS，列表页需要完整展示而不是只显示 SOA 的主 NS。
	NSRecords []string `gorm:"-" json:"ns_records,omitempty"`
	Records       []Record       `gorm:"foreignKey:DomainID;constraint:OnDelete:CASCADE" json:"records,omitempty"`
	DNSSECKeys    []DNSSECKey    `gorm:"foreignKey:DomainID;constraint:OnDelete:CASCADE" json:"dnssec_keys,omitempty"`
	// SecurityPolicy 是本区域的安全防护策略。随集群区域快照一起下发，
	// 使边缘节点也能在解析入口执行同一套规则。
	SecurityPolicy *DomainSecurityPolicy `gorm:"foreignKey:DomainID;constraint:OnDelete:CASCADE" json:"security_policy,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

type RecordType string

const (
	RecordTypeA      RecordType = "A"
	RecordTypeAAAA   RecordType = "AAAA"
	RecordTypeCNAME  RecordType = "CNAME"
	RecordTypeTXT    RecordType = "TXT"
	RecordTypeMX     RecordType = "MX"
	RecordTypeNS     RecordType = "NS"
	RecordTypeSRV    RecordType = "SRV"
	RecordTypeCAA    RecordType = "CAA"
	RecordTypePTR    RecordType = "PTR"
	RecordTypeSOA    RecordType = "SOA"
	RecordTypeALIAS  RecordType = "ALIAS"
	RecordTypeHTTPS  RecordType = "HTTPS"
	RecordTypeSVCB   RecordType = "SVCB"
	RecordTypeTLSA   RecordType = "TLSA"
	RecordTypeSSHFP  RecordType = "SSHFP"
	RecordTypeDS     RecordType = "DS"
	RecordTypeDNSKEY RecordType = "DNSKEY"
)

type Record struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	DomainID  uint           `gorm:"index;not null" json:"domain_id"`
	Name      string         `gorm:"size:255;index;not null" json:"name"`       // "@", "www", "sub", "*"
	Type      RecordType     `gorm:"size:16;index;not null" json:"type"`        // "A", "CNAME", etc.
	Value     string         `gorm:"type:text;not null" json:"value"`           // IP, target, TXT content
	TTL       uint32         `gorm:"default:300" json:"ttl"`                    // seconds, 1=auto
	Priority  uint16         `gorm:"default:0" json:"priority"`                 // MX, SRV
	Weight    uint16         `gorm:"default:100" json:"weight"`                 // Load balancing weight (1-100)
	Port      uint16         `gorm:"default:0" json:"port"`                     // SRV
	GeoLine   string         `gorm:"size:32;default:'default'" json:"geo_line"` // default, cn, us, jp, de, gb, eu, etc. (ISO 3166-1 alpha-2 codes)
	Enabled   bool           `gorm:"default:true" json:"enabled"`
	Comment   string         `gorm:"size:255" json:"comment"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

type DNSSECKeyType string

const (
	KeyTypeKSK DNSSECKeyType = "KSK" // Key Signing Key (Flags: 257)
	KeyTypeZSK DNSSECKeyType = "ZSK" // Zone Signing Key (Flags: 256)
)

type DNSSECKey struct {
	ID         uint          `gorm:"primaryKey" json:"id"`
	DomainID   uint          `gorm:"index;not null" json:"domain_id"`
	KeyType    DNSSECKeyType `gorm:"size:16;not null" json:"key_type"` // KSK / ZSK
	Algorithm  uint8         `gorm:"default:13" json:"algorithm"`      // 13 = ECDSAP256SHA256, 8 = RSASHA256
	Flags      uint16        `json:"flags"`                            // 256 or 257
	KeyTag     uint16        `json:"key_tag"`
	PublicKey  string        `gorm:"type:text;not null" json:"public_key"`
	PrivateKey string        `gorm:"type:text;not null" json:"-"`  // PEM encoded
	DigestType uint8         `gorm:"default:2" json:"digest_type"` // 2 = SHA-256
	Digest     string        `gorm:"size:255" json:"digest"`       // Hex DS digest
	DSConfig   string        `gorm:"size:512" json:"ds_config"`    // Full formatted DS string
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
}

type HealthCheckProtocol string

const (
	ProtocolHTTP  HealthCheckProtocol = "HTTP"
	ProtocolHTTPS HealthCheckProtocol = "HTTPS"
	ProtocolTCP   HealthCheckProtocol = "TCP"
	ProtocolPING  HealthCheckProtocol = "PING"
)

type HealthStatus string

const (
	HealthStatusHealthy  HealthStatus = "healthy"
	HealthStatusDegraded HealthStatus = "degraded"
	HealthStatusDown     HealthStatus = "down"
)

type HealthCheck struct {
	ID               uint                `gorm:"primaryKey" json:"id"`
	DomainID         uint                `gorm:"index;not null" json:"domain_id"`
	RecordID         uint                `gorm:"index;not null" json:"record_id"`
	Record           *Record             `gorm:"foreignKey:RecordID" json:"record,omitempty"`
	Name             string              `gorm:"size:128;not null" json:"name"`
	Protocol         HealthCheckProtocol `gorm:"size:16;default:'HTTP'" json:"protocol"`
	Host             string              `gorm:"size:255;not null" json:"host"`
	Port             int                 `gorm:"default:80" json:"port"`
	Path             string              `gorm:"size:255;default:'/'" json:"path"`
	ExpectedCode     int                 `gorm:"default:200" json:"expected_code"`
	CheckIntervalSec int                 `gorm:"default:15" json:"check_interval_sec"`
	TimeoutSec       int                 `gorm:"default:5" json:"timeout_sec"`
	FallbackIP       string              `gorm:"size:128" json:"fallback_ip"`
	Status           HealthStatus        `gorm:"size:16;default:'healthy'" json:"status"`
	FailoverActive   bool                `gorm:"default:false" json:"failover_active"` // 探测失败且已切换到备用IP时为 true
	ConsecutiveFails int                 `gorm:"default:0" json:"consecutive_fails"`
	LastLatencyMs    int64               `json:"last_latency_ms"`
	LastCheckedAt    *time.Time          `json:"last_checked_at"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
	DeletedAt        gorm.DeletedAt      `gorm:"index" json:"-"`
}

type Node struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	NodeID        string    `gorm:"size:64;uniqueIndex;not null" json:"node_id"`
	Name          string    `gorm:"size:128;not null" json:"name"`
	IP            string    `gorm:"size:64;not null" json:"ip"`
	Region        string    `gorm:"size:64;default:'Global'" json:"region"`
	SecretToken   string    `gorm:"size:128;not null" json:"secret_token"`
	IsOnline      bool      `gorm:"default:false" json:"is_online"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	Version       string    `gorm:"size:32" json:"version"`
	CPUUsage      float64   `gorm:"default:0" json:"cpu_usage"`
	MemoryUsage   float64   `gorm:"default:0" json:"memory_usage"`
	DiskUsage     float64   `gorm:"default:0" json:"disk_usage"`
	NetRxBps      int64     `gorm:"default:0" json:"net_rx_bps"`
	NetTxBps      int64     `gorm:"default:0" json:"net_tx_bps"`
	QPS           int64     `gorm:"default:0" json:"qps"`            // 最近 60s 滚动窗口的真实瞬时 QPS
	TotalQueries  int64     `gorm:"default:0" json:"total_queries"`  // 自节点启动以来处理的累计查询数
	LatencyMs     int64     `gorm:"default:0" json:"latency_ms"`
	GeoStats      string    `gorm:"type:text" json:"-"`
	DomainStats   string    `gorm:"type:text" json:"-"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// NodeStatusSample 是边缘节点在线状态的时序采样点，用于绘制 Uptime 风格的历史在线率状态条。
// 集群管理器周期性（约每 30s，与存活检查同频）为每个节点写入一条，超过保留期后清理。
// 采样保存最近的资源指标快照，便于在监控页展示时无需额外查询。
type NodeStatusSample struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	NodeID      string    `gorm:"size:64;not null;index:idx_node_sampled,priority:1" json:"node_id"` // 关联 Node.NodeID
	IsOnline    bool      `gorm:"default:false" json:"is_online"`
	LatencyMs   int64     `gorm:"default:0" json:"latency_ms"`
	CPUUsage    float64   `gorm:"default:0" json:"cpu_usage"`
	MemoryUsage float64   `gorm:"default:0" json:"memory_usage"`
	DiskUsage   float64   `gorm:"default:0" json:"disk_usage"`
	NetRxBps    int64     `gorm:"default:0" json:"net_rx_bps"`
	NetTxBps    int64     `gorm:"default:0" json:"net_tx_bps"`
	QPS         int64     `gorm:"default:0" json:"qps"`
	SampledAt   time.Time `gorm:"index:idx_node_sampled,priority:2;index:idx_sampled_at" json:"sampled_at"`
}

type CertStatus string

const (
	CertStatusValid   CertStatus = "valid"
	CertStatusIssuing CertStatus = "issuing"
	CertStatusExpired CertStatus = "expired"
	CertStatusFailed  CertStatus = "failed"
)

// ACMEAccountStatus 描述申请人对应的 ACME 账户在 CA 侧的注册状态。
type ACMEAccountStatus string

const (
	ACMEAccountUnregistered ACMEAccountStatus = "unregistered" // 尚未向 CA 注册
	ACMEAccountValid        ACMEAccountStatus = "valid"        // 已注册且可用
	ACMEAccountFailed       ACMEAccountStatus = "failed"       // 注册失败（EAB 错误、CA 拒绝等）
)

type CertApplicant struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	UserID       uint      `gorm:"index;not null" json:"user_id"`
	Name         string    `gorm:"size:128;not null" json:"name"` // e.g. "默认运维联系人"
	Email        string    `gorm:"size:128;not null" json:"email"`
	Organization string    `gorm:"size:128" json:"organization"`
	// 默认值由业务层（applicant handler / acme.DefaultProviderName）填充。
	// 此处不写 gorm default：SQL 默认值里的单引号会让 struct tag 变成非法转义，
	// 导致 GORM 解析不到整个 gorm 标签（size 约束也会一并失效）。
	Provider     string    `gorm:"size:64" json:"provider"` // Let's Encrypt / ZeroSSL / Google PKI / Buypass
	EABKID       string    `gorm:"size:255" json:"eab_kid"`
	EABHMACKey   string    `gorm:"size:255" json:"eab_hmac_key"`
	IsDefault    bool      `gorm:"default:false" json:"is_default"`

	// --- 真实 ACME 账户状态（RFC 8555）---
	// ACME 账户由「账户密钥对」唯一标识，注册后 CA 返回账户 URI（即 kid），
	// 后续所有请求都用该密钥签名并携带 kid。因此密钥必须持久化，
	// 否则每次签发都会创建新账户并迅速撞上 CA 的账户注册速率限制。
	ACMEAccountKey string `gorm:"type:text" json:"-"` // PEM 编码的账户私钥，绝不外泄给前端
	ACMEAccountURI string `gorm:"size:512" json:"acme_account_uri"`
	// 账户与 Directory 绑定：同一密钥在不同 CA（或 staging/prod）下是不同账户，
	// 切换 Directory 必须重新注册，故记录注册时所用的 Directory 地址。
	ACMEDirectoryURL string            `gorm:"size:512" json:"acme_directory_url"`
	ACMEAccountState ACMEAccountStatus `gorm:"size:32;default:'unregistered'" json:"acme_account_state"`
	ACMEAccountError string            `gorm:"type:text" json:"acme_account_error"`
	ACMERegisteredAt *time.Time        `json:"acme_registered_at"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Certificate struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	UUID        string         `gorm:"size:36;uniqueIndex" json:"uuid"` // 对外标识，避免 URL / API 暴露自增主键
	DomainID    uint           `gorm:"index;not null" json:"domain_id"`
	ApplicantID *uint          `gorm:"index" json:"applicant_id"`
	Applicant   *CertApplicant `gorm:"foreignKey:ApplicantID" json:"applicant,omitempty"`
	Name        string         `gorm:"size:255;not null" json:"name"`               // e.g. "example.com and *.example.com"
	Domains     string         `gorm:"type:text;not null" json:"domains"`           // comma-separated domains
	KeyType     string         `gorm:"size:32;default:'ECDSAP256'" json:"key_type"` // ECDSAP256, ECDSAP384, ECDSAP521, ED25519, RSA2048, RSA3072, RSA4096
	CertPEM     string         `gorm:"type:text" json:"cert_pem"`
	KeyPEM      string         `gorm:"type:text" json:"key_pem"`
	// 签发机构名由签发流程写入，同样避免在 tag 里放带单引号的 SQL 默认值。
	Issuer      string         `gorm:"size:128" json:"issuer"`
	ValidFrom   *time.Time     `json:"valid_from"`
	ValidTo     *time.Time     `json:"valid_to"`
	AutoRenew   bool           `gorm:"default:true" json:"auto_renew"`
	Status      CertStatus     `gorm:"size:32;default:'valid'" json:"status"`
	LastError   string         `gorm:"type:text" json:"last_error"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// BeforeCreate 为证书分配对外 UUID，使 API 与下载 URL 不暴露自增主键。
func (c *Certificate) BeforeCreate(tx *gorm.DB) error {
	if c.UUID == "" {
		c.UUID = NewUUID()
	}
	return nil
}

type SystemSetting struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Key         string    `gorm:"size:128;uniqueIndex;not null" json:"key"`
	Value       string    `gorm:"type:text" json:"value"`
	Description string    `gorm:"size:255" json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Nameserver struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	Hostname      string    `gorm:"size:255;uniqueIndex;not null" json:"hostname"` // e.g. "ns1.example.com"
	ClusterNodeID *uint     `gorm:"index" json:"node_id"`                          // bound cluster node ID
	Node          *Node     `gorm:"foreignKey:ClusterNodeID;references:ID" json:"node,omitempty"`
	IPv4          string    `gorm:"size:64" json:"ipv4"` // Glue A record
	IPv6          string    `gorm:"size:64" json:"ipv6"` // Glue AAAA record
	IsActive      bool      `gorm:"default:true" json:"is_active"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type AuditLog struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	UserID     uint      `gorm:"index" json:"user_id"`
	Username   string    `gorm:"size:64" json:"username"`
	Action     string    `gorm:"size:64;not null" json:"action"`
	TargetType string    `gorm:"size:64" json:"target_type"`
	TargetID   string    `gorm:"size:64" json:"target_id"`
	Detail     string    `gorm:"type:text" json:"detail"`
	IP         string    `gorm:"size:64" json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
}

// RoutingLine 表示一条全局智能分线线路。一条线路由多维度条件（大洲 / 国家 / ASN）组成，
// 条件之间为「或」关系：客户端的大洲、国家或 ASN 命中任一维度即视为匹配该线路。
// DNS 记录通过 Record.GeoLine 引用线路的 Key（或向后兼容地直接填写国家码/大洲码）。
type RoutingLine struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	Key         string         `gorm:"size:64;uniqueIndex;not null" json:"key"` // 线路唯一标识，如 asia / china-telecom / custom-1
	Name        string         `gorm:"size:128;not null" json:"name"`           // 显示名称
	Description string         `gorm:"size:255" json:"description"`
	Continents  string         `gorm:"size:255" json:"continents"` // 逗号分隔大洲码：AS,EU,NA,SA,AF,OC
	Countries   string         `gorm:"type:text" json:"countries"` // 逗号分隔 ISO 3166-1 alpha-2 国家码：cn,jp,kr
	ASNs        string         `gorm:"type:text" json:"asns"`      // 逗号分隔自治域号：4134,4837,9808
	Priority    int            `gorm:"default:100" json:"priority"` // 匹配优先级，数字越小越优先（default 线路最大）
	Enabled     bool           `gorm:"default:true" json:"enabled"`
	IsBuiltin   bool           `gorm:"default:false" json:"is_builtin"` // 内置线路不可删除
	// RefCount 是当前引用该线路的解析记录数（gorm:"-" 不落库，仅列表接口按需计算填充），
	// 供前端判断线路是否在用、能否删除。
	RefCount    int64          `gorm:"-" json:"ref_count"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// DDNSKey 是一条独立的动态 DNS 更新凭据，供 ddclient / 路由器固件 / NAS 等
// 标准 DDNS 客户端调用 dyndns2 端点，把指定主机名的 A/AAAA 记录改成客户端当前 IP。
//
// 安全设计上与系统里既有的 User.APIKey、Node.SecretToken 明文入库的做法不同：
//  1. 只存 SHA-256 摘要，明文仅在创建与轮换时返回一次，库被读走也无法还原密钥；
//  2. 作用域收窄到「单个域名 + 指定主机名列表」，泄露后的影响面不会扩散到其它域名；
//  3. 支持来源 IP/CIDR 白名单，密钥泄露后在白名单外依然不可用。
type DDNSKey struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	UUID     string `gorm:"size:64;uniqueIndex" json:"uuid"`
	DomainID uint   `gorm:"index;not null" json:"domain_id"`
	UserID   uint   `gorm:"index" json:"user_id"`
	Name     string `gorm:"size:128;not null" json:"name"`

	// KeyPrefix 是密钥的可见前缀（形如 ddns_a1b2c3d4），仅用于在控制台里辨认是哪一把钥匙。
	// KeyHash 是完整密钥的 SHA-256 十六进制摘要，唯一索引使认证可以 O(1) 命中而无需遍历比对。
	KeyPrefix string `gorm:"size:32;index" json:"key_prefix"`
	KeyHash   string `gorm:"size:64;uniqueIndex;not null" json:"-"`

	// Hostnames 是本密钥允许更新的主机名白名单，逗号分隔的相对名：
	// "@" 表示域名本身，"home" 表示 home.example.com，"*" 表示该域名下任意主机名。
	Hostnames string `gorm:"size:512;not null" json:"hostnames"`

	// AllowedIPs 是允许使用本密钥的来源地址白名单，逗号分隔的 IP 或 CIDR
	// （如 203.0.113.7, 198.51.100.0/24, 2001:db8::/32）。留空表示不限制来源。
	AllowedIPs string `gorm:"size:1024" json:"allowed_ips"`

	// RecordTTL 是 DDNS 维护的记录使用的 TTL。动态地址应当用短 TTL，
	// 否则 IP 变更后缓存会让访问在很长时间内落到旧地址。
	RecordTTL uint32 `gorm:"default:60" json:"record_ttl"`

	// 以下四个开关刻意不写 gorm 的 default 标签。
	// GORM 会把「带 default 标签且当前是零值」的字段从 INSERT 语句中省略，
	// 转而让数据库默认值生效——于是显式传 false 建出来的密钥会变成 true，
	// 出现「关掉自动创建却仍在建记录」「建了停用的密钥却能用」这类静默失效。
	// 默认值统一由 API 层的 boolOrDefault 决定，数据库不再参与。
	AllowIPv4 bool `json:"allow_ipv4"`
	AllowIPv6 bool `json:"allow_ipv6"`
	// AutoCreate 决定目标记录不存在时是自动建一条，还是按 dyndns2 语义返回 nohost。
	AutoCreate bool `json:"auto_create"`
	Enabled    bool `json:"enabled"`

	// 以下为最近一次调用的留痕，用于控制台排障（客户端是否真的在跑、从哪个出口 IP 来）。
	LastIPv4      string     `gorm:"size:64" json:"last_ipv4"`
	LastIPv6      string     `gorm:"size:128" json:"last_ipv6"`
	LastClientIP  string     `gorm:"size:64" json:"last_client_ip"`
	LastUserAgent string     `gorm:"size:255" json:"last_user_agent"`
	LastStatus    string     `gorm:"size:32" json:"last_status"`
	LastUsedAt    *time.Time `json:"last_used_at"`
	UpdateCount   int64      `gorm:"default:0" json:"update_count"`
	RejectCount   int64      `gorm:"default:0" json:"reject_count"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// BeforeCreate 为 DDNS 密钥分配对外 UUID，与证书一致地避免 API 暴露自增主键。
func (k *DDNSKey) BeforeCreate(tx *gorm.DB) error {
	if k.UUID == "" {
		k.UUID = NewUUID()
	}
	return nil
}
