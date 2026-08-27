package config

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	DNS       DNSConfig       `yaml:"dns"`
	Database  DatabaseConfig  `yaml:"database"`
	Redis     RedisConfig     `yaml:"redis"`
	DNSSEC    DNSSECConfig    `yaml:"dnssec"`
	Cluster   ClusterConfig   `yaml:"cluster"`
	Prober    ProberConfig    `yaml:"prober"`
	JWTSecret string          `yaml:"jwt_secret"`
}

type ServerConfig struct {
	HTTPPort int    `yaml:"http_port"`
	Host     string `yaml:"host"`
	Mode     string `yaml:"mode"` // debug / release
	// TrustedProxies 是允许其设置 X-Forwarded-For 的反向代理地址（IP 或 CIDR）。
	// 留空表示不信任任何代理：此时客户端 IP 一律取 TCP 源地址，伪造 XFF 头无效。
	// 只有在本服务确实跑在 nginx / 负载均衡后面时才填，否则 DDNS 的来源 IP
	// 白名单可以被任意伪造的 X-Forwarded-For 绕过。
	TrustedProxies []string `yaml:"trusted_proxies"`
}

type DNSConfig struct {
	UDPPort     int      `yaml:"udp_port"`
	TCPPort     int      `yaml:"tcp_port"`
	TLSPort     int      `yaml:"tls_port"`
	EnableTLS   bool     `yaml:"enable_tls"`
	TLSCertFile string   `yaml:"tls_cert_file"`
	TLSKeyFile  string   `yaml:"tls_key_file"`
	EnableDoH   bool     `yaml:"enable_doh"`
	DefaultNS   []string `yaml:"default_ns"`
	DefaultTTL  uint32   `yaml:"default_ttl"`
	RateLimit   int      `yaml:"rate_limit"` // queries per sec per client IP (RRL)
}

type DatabaseConfig struct {
	Driver          string `yaml:"driver"` // mysql or sqlite
	DSN             string `yaml:"dsn"`
	MaxOpenConns    int    `yaml:"max_open_conns"`
	MaxIdleConns    int    `yaml:"max_idle_conns"`
	ConnMaxLifetime int    `yaml:"conn_max_lifetime"` // minutes
}

type RedisConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type DNSSECConfig struct {
	AutoSign  bool   `yaml:"auto_sign"`
	Algorithm string `yaml:"algorithm"` // ECDSAP256SHA256, RSA2048, etc.
}

type ClusterConfig struct {
	NodeID      string `yaml:"node_id"`
	NodeName    string `yaml:"node_name"`
	IsMaster    bool   `yaml:"is_master"`
	MasterURL   string `yaml:"master_url"`
	SecretToken string `yaml:"secret_token"`
	SyncInterval int   `yaml:"sync_interval"` // seconds
}

type ProberConfig struct {
	Enabled      bool `yaml:"enabled"`
	CheckInterval int `yaml:"check_interval"` // seconds
	TimeoutSec   int  `yaml:"timeout_sec"`
}

var GlobalConfig *Config

func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			HTTPPort: 8080,
			Host:     "0.0.0.0",
			Mode:     "release",
		},
		DNS: DNSConfig{
			UDPPort: 53,
			TCPPort: 53,
			TLSPort: 853,
			// 留空：权威 NS 主机名因部署而异，任何出厂值都会让新建域名带上
			// 不属于使用者的 NS 记录。由使用者在「权威 NS 服务器」页配置。
			DefaultNS:  nil,
			DefaultTTL: 300,
			RateLimit:  1000,
		},
		Database: DatabaseConfig{
			// 默认 sqlite 本地文件：不依赖外部数据库，裸跑二进制即可启动。
			// 需要 MySQL 时通过配置文件或 DNSCAT_DB_DSN 覆盖。
			Driver: "sqlite",
			DSN:    "data/dnscat.db",
		},
		Redis: RedisConfig{
			Enabled: false,
			Addr:    "127.0.0.1:6379",
		},
		DNSSEC: DNSSECConfig{
			AutoSign:  true,
			Algorithm: "ECDSAP256SHA256",
		},
		Cluster: ClusterConfig{
			NodeID:   "master-node-01",
			NodeName: "Master Primary",
			IsMaster: true,
			// 留空：该令牌是边缘节点拉取全量区域快照的唯一凭据，
			// 出厂默认值等于任何人都能从公网抓走使用者的全部解析数据。
			// 由 install.sh 安装时随机生成，或经 DNSCAT_CLUSTER_TOKEN 注入。
			SecretToken:  "",
			SyncInterval: 10,
		},
		Prober: ProberConfig{
			Enabled:       true,
			CheckInterval: 15,
			TimeoutSec:    5,
		},
		// 留空：出厂默认 JWT 密钥意味着任何人都能对未改配置的部署伪造管理员令牌。
		// 留空时启动阶段随机生成（见 LoadConfig），生产环境应显式配置以便重启后会话不失效。
		JWTSecret: "",
	}
}

func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, err
			}
		}
	}

	// Environment variable overrides
	if port := os.Getenv("DNSCAT_HTTP_PORT"); port != "" {
		// 这里原先是个空的 if 体，导致该环境变量从来不生效。
		if p, err := strconv.Atoi(port); err == nil && p > 0 && p < 65536 {
			cfg.Server.HTTPPort = p
		} else {
			log.Printf("[Config] 忽略非法的 DNSCAT_HTTP_PORT=%q", port)
		}
	}
	if dsn := os.Getenv("DNSCAT_DB_DSN"); dsn != "" {
		cfg.Database.DSN = dsn
		if strings.Contains(dsn, "@tcp(") || strings.HasPrefix(dsn, "mysql://") {
			cfg.Database.Driver = "mysql"
		}
	}
	if redisAddr := os.Getenv("DNSCAT_REDIS_ADDR"); redisAddr != "" {
		cfg.Redis.Addr = redisAddr
		cfg.Redis.Enabled = true
	}
	if jwtSecret := os.Getenv("DNSCAT_JWT_SECRET"); jwtSecret != "" {
		cfg.JWTSecret = jwtSecret
	}
	// 集群令牌也支持环境变量注入，这样 Docker 部署无需挂载配置文件即可用随机令牌，
	// 不必沿用镜像内置值。
	if token := os.Getenv("DNSCAT_CLUSTER_TOKEN"); token != "" {
		cfg.Cluster.SecretToken = token
	}

	// 兜底生成 JWT 密钥：宁可每次重启让会话失效，也不能回落到可预测的固定密钥。
	if strings.TrimSpace(cfg.JWTSecret) == "" {
		cfg.JWTSecret = randomHex(32)
		log.Println("[Config] 未配置 jwt_secret，已生成临时随机密钥；" +
			"进程重启后已签发的登录会话将失效。生产环境请在配置文件或 DNSCAT_JWT_SECRET 中固定该值。")
	}

	GlobalConfig = cfg
	return cfg, nil
}

// randomHex 返回 n 字节的加密随机数的十六进制表示。
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败属于系统级异常，此时继续运行等于用空密钥签发令牌。
		log.Fatalf("[Config] 无法获取加密随机数，拒绝以不安全的密钥启动: %v", err)
	}
	return hex.EncodeToString(b)
}
