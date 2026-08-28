package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"dnscat/internal/acme"
	"dnscat/internal/api"
	"dnscat/internal/buildinfo"
	"dnscat/internal/cache"
	"dnscat/internal/cluster"
	"dnscat/internal/config"
	"dnscat/internal/database"
	"dnscat/internal/dnsengine"
	"dnscat/internal/healthcheck"
	"dnscat/internal/model"
	"dnscat/internal/seclogstore"
	"dnscat/internal/securitystore"
	"dnscat/internal/telemetrystore"
	"dnscat/internal/webui"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// loadACMESettings 从系统设置表读取 ACME 相关配置。
//
// acme_ca_url 语义为「覆盖」：留空时按申请人选择的服务商自动挑选 Directory，
// 填写时强制使用该地址（用于私有 CA 或本地测试服务）。
// 因此当它仍是出厂默认的 Let's Encrypt 生产地址时，视为「未覆盖」，
// 否则会把 ZeroSSL、Google 等服务商的选择一并劫持掉。
func loadACMESettings() (email string, caURL string, staging bool) {
	// 不设出厂默认邮箱：证书到期通知会真实发到这个地址，
	// 留空时由使用者在「系统设置」里填写，签发流程再校验。
	email = ""

	if database.DB == nil {
		return email, "", false
	}

	var settings []model.SystemSetting
	if err := database.DB.Where("`key` IN ?",
		[]string{"acme_email", "acme_ca_url", "acme_staging"}).Find(&settings).Error; err != nil {
		log.Printf("[Warning] 读取 ACME 系统设置失败，使用默认值: %v", err)
		return email, "", false
	}

	const letsEncryptProd = "https://acme-v02.api.letsencrypt.org/directory"
	for _, s := range settings {
		value := strings.TrimSpace(s.Value)
		switch s.Key {
		case "acme_email":
			if value != "" {
				email = value
			}
		case "acme_ca_url":
			if value != "" && value != letsEncryptProd {
				caURL = value
			}
		case "acme_staging":
			staging = strings.EqualFold(value, "true") || value == "1"
		}
	}

	return email, caURL, staging
}

// loadLogRetentionLimits 读取各域名的安全日志保留上限，供启动时按上限恢复日志。
// 上限存在每个域名自己的安全策略里，缺省或非法值回退到出厂默认。
func loadLogRetentionLimits() map[uint]int {
	limits := make(map[uint]int)
	if database.DB == nil {
		return limits
	}

	var rows []model.DomainSecurityPolicy
	if err := database.DB.Select("domain_id", "log_retention_limit").Find(&rows).Error; err != nil {
		log.Printf("[Warning] 读取安全日志保留上限失败，全部按默认值恢复: %v", err)
		return limits
	}
	for _, r := range rows {
		limit := r.LogRetentionLimit
		if limit <= 0 {
			limit = model.DefaultLogRetentionLimit
		}
		limits[r.DomainID] = limit
	}
	return limits
}

func main() {
	configPath := flag.String("config", "config.yaml", "Path to config.yaml")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	// --version 要能在没有配置文件、没有数据库的机器上直接跑通，
	// 所以放在加载配置之前返回。
	if *showVersion {
		fmt.Println(buildinfo.Full("dnscat-server"))
		return
	}

	log.Printf("==================================================")
	log.Printf("  DnsCat Authoritative DNS Master Server %s", buildinfo.Short())
	log.Printf("==================================================")

	// 1. Load Config
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 2. Initialize Database & Migration
	_, err = database.InitDB(cfg)
	if err != nil {
		log.Fatalf("Failed to init database: %v", err)
	}

	// 3. Initialize Cache
	cache.InitCache(cfg)

	// 遥测持久化：启动时从 Redis/DB 恢复历史统计，并启动定时刷写(Redis)+归档(DB)。
	telemetryStore := telemetrystore.New(database.DB, cfg.Redis.Enabled)
	telemetryStore.Restore()
	telemetryStore.Start()

	// 域名安全防护：先补齐默认策略并装载历史攻击统计，再加载 DNS 区域，
	// 这样区域首次载入时就能编译出安全运行时规则。
	securityStore := securitystore.New(database.DB)
	securityStore.Restore()
	securityStore.Start()

	// 安全日志持久化：事件明细以 Redis LIST 承载（LPUSH + LTRIM 天然实现
	// 「写满保留上限即丢弃最旧」），启动时恢复到内存事件环，之后异步批量刷盘。
	// 刻意不入库：日志是可滚动丢弃的短期数据且条数有上界，走 Redis 可避免
	// 攻击洪峰下的 SQL 写放大。Redis 不可用时自动退化为仅内存。
	seclogStore := seclogstore.New()
	seclogStore.Restore(loadLogRetentionLimits())
	seclogStore.Start()

	// 4. Initialize DNS In-Memory Zone Store & Load DB Zones
	zoneStore := dnsengine.GlobalZoneStore
	if err := zoneStore.LoadFromDB(); err != nil {
		log.Printf("[Warning] Failed to preload zones from DB: %v", err)
	} else {
		log.Printf("[ZoneStore] Preloaded authoritative zones successfully")
	}
	if err := dnsengine.GlobalLineStore.LoadFromDB(); err != nil {
		log.Printf("[Warning] Failed to preload routing lines from DB: %v", err)
	} else {
		log.Printf("[LineStore] Preloaded smart routing lines successfully")
	}

	// 5. Initialize Cluster Manager
	clusterMgr := cluster.InitClusterManager(cfg.Cluster.SecretToken, time.Duration(cfg.Cluster.SyncInterval)*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clusterMgr.Start(ctx)

	// 6. Initialize Health Checker Prober
	if cfg.Prober.Enabled {
		prober := healthcheck.NewHealthChecker(cfg.Prober.CheckInterval, cfg.Prober.TimeoutSec)
		prober.Start(ctx)
	}

	// 7. Initialize ACME Certificate Issuer & Auto-Renewal Worker
	// 配置来自系统设置表，使控制台里的修改真正生效（此前为硬编码）。
	acmeEmail, acmeCAURL, acmeStaging := loadACMESettings()
	acme.InitIssuer(acmeEmail, acmeCAURL, acmeStaging)
	acme.MarkExpiredCertificates()
	acme.NewRenewer(acme.GlobalIssuer).Start(ctx)

	// 7b. NS 指向状态定时扫描：每 30 分钟自动校验所有域名的公网 NS 指向，
	// 让「NS 指向状态」无需用户手动点击也能保持最新（手动刷新仍可即时触发）。
	api.StartNSScanner(ctx, cfg, 30*time.Minute)

	// 8. Start DNS Server (UDP, TCP, TLS)
	dnsServer := dnsengine.NewServer(cfg, zoneStore)
	if err := dnsServer.Start(); err != nil {
		log.Fatalf("Failed to start DNS nameserver: %v", err)
	}

	// 9. Setup Gin HTTP & DoH API Server
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()

	// 可信代理白名单：gin 默认信任所有代理，会无条件采信 X-Forwarded-For，
	// 使 c.ClientIP() 可被任意伪造——DDNS 的来源 IP 白名单就形同虚设。
	// 默认收紧到「不信任任何代理」，客户端 IP 取 TCP 源地址；
	// 确实部署在反代后面时再通过 server.trusted_proxies 显式放行。
	if err := r.SetTrustedProxies(cfg.Server.TrustedProxies); err != nil {
		log.Fatalf("Invalid server.trusted_proxies: %v", err)
	}
	if len(cfg.Server.TrustedProxies) == 0 {
		log.Printf("[HTTP] 未配置可信代理，客户端 IP 取 TCP 源地址（忽略 X-Forwarded-For）")
	} else {
		log.Printf("[HTTP] 可信代理: %v（将解析 X-Forwarded-For）", cfg.Server.TrustedProxies)
	}

	// CORS Setup
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Length", "Content-Type", "Authorization", "X-API-Key", "X-Node-Token", "X-DDNS-Key", "Accept"},
		ExposeHeaders:    []string{"Content-Length", "Content-Disposition"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// DoH (DNS over HTTPS) RFC 8484 endpoint
	r.GET("/dns-query", dnsServer.DoHHandler)
	r.POST("/dns-query", dnsServer.DoHHandler)

	// Web 控制台静态资源：优先用编译期嵌入的副本，回退到磁盘 web/dist。
	// 嵌入后二进制自带控制台，不再依赖进程工作目录——systemd 单元的
	// WorkingDirectory 指向数据目录，那里没有前端产物，靠相对路径是找不到的。
	if uiFS, uiSrc := webui.FS(); uiFS != nil {
		log.Printf("[Web UI] 控制台静态资源来源: %s", uiSrc)

		if assets, err := fs.Sub(uiFS, "assets"); err == nil {
			r.StaticFS("/assets", http.FS(assets))
		} else {
			log.Printf("[Web UI] 未找到 assets 子目录: %v", err)
		}

		serveUIFile := func(name, contentType string) gin.HandlerFunc {
			return func(c *gin.Context) {
				data, err := fs.ReadFile(uiFS, name)
				if err != nil {
					c.Status(http.StatusNotFound)
					return
				}
				c.Data(http.StatusOK, contentType, data)
			}
		}
		r.GET("/favicon.svg", serveUIFile("favicon.svg", "image/svg+xml"))
		r.GET("/countries-110m.json", serveUIFile("countries-110m.json", "application/json"))

		r.NoRoute(func(c *gin.Context) {
			if strings.HasPrefix(c.Request.URL.Path, "/api/") || c.Request.URL.Path == "/api" {
				c.JSON(http.StatusNotFound, gin.H{"error": "API route not found"})
				return
			}
			data, err := fs.ReadFile(uiFS, "index.html")
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "Web console assets not found"})
				return
			}
			// index.html 必须禁用缓存：它引用的是带内容 hash 的 JS/CSS 文件名，
			// 一旦被浏览器缓存住，前端发布新版本后仍会去加载旧 hash 的资源，
			// 表现为「改动明明部署了却不生效」，只能靠用户手动硬刷新解决。
			// /assets 下的文件名自带 hash，内容不可变，继续走默认强缓存即可。
			c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
			c.Data(http.StatusOK, "text/html; charset=utf-8", data)
		})
	} else {
		log.Printf("[Web UI] 未找到控制台静态资源（无嵌入副本，也无 %s 目录），本进程仅提供 API",
			webui.DiskDir)
	}

	// API Routes Group
	authHandler := api.NewAuthHandler(cfg)
	domainHandler := api.NewDomainHandler(cfg)
	recordHandler := api.NewRecordHandler()
	dnssecHandler := api.NewDNSSECHandler()
	securityHandler := api.NewSecurityHandler()
	healthHandler := api.NewHealthHandler()
	nodeHandler := api.NewNodeHandler(clusterMgr)
	statsHandler := api.NewStatsHandler()
	certHandler := api.NewCertHandler()
	settingHandler := api.NewSettingHandler()
	nameserverHandler := api.NewNameserverHandler()
	applicantHandler := api.NewApplicantHandler()
	routingHandler := api.NewRoutingHandler()
	ddnsHandler := api.NewDDNSHandler()

	// DDNS 动态更新端点（dyndns2 协议）。
	// 必须挂在公开路径上：DDNS 客户端持有的是自己的独立密钥，不是控制台的 JWT。
	// 鉴权、来源 IP 白名单与失败限流都在 handler 内部完成。
	// /nic/update 是 DynDNS 沿用至今的事实标准路径，ddclient 与多数路由器固件
	// 会把它硬编码在协议实现里，用户只需填服务器地址即可对接。
	r.GET("/nic/update", ddnsHandler.Update)
	r.POST("/nic/update", ddnsHandler.Update)
	r.GET("/api/ddns/update", ddnsHandler.Update)
	r.POST("/api/ddns/update", ddnsHandler.Update)

	apiV1 := r.Group("/api")
	{
		// Public Auth
		auth := apiV1.Group("/auth")
		{
			auth.POST("/login", authHandler.Login)
			auth.POST("/register", authHandler.Register)
			auth.GET("/me", api.AuthMiddleware(cfg), authHandler.GetMe)
			auth.POST("/regenerate-api-key", api.AuthMiddleware(cfg), authHandler.RegenerateAPIKey)
			auth.POST("/change-password", api.AuthMiddleware(cfg), authHandler.ChangePassword)
			// 界面偏好（语言 / 主题）跟随账号保存，登录后覆盖浏览器本地设置。
			auth.PUT("/preferences", api.AuthMiddleware(cfg), authHandler.UpdatePreferences)
		}

		// Node Synchronization & Heartbeat (Requires Node Token)
		nodeAuth := apiV1.Group("/node")
		nodeAuth.Use(api.NodeTokenRequired(clusterMgr))
		{
			nodeAuth.GET("/sync", nodeHandler.SyncSnapshot)
			nodeAuth.POST("/heartbeat", nodeHandler.Heartbeat)
		}

		// Protected User & Management APIs
		protected := apiV1.Group("")
		protected.Use(api.AuthMiddleware(cfg))
		{
			// Domains
			protected.GET("/domains", domainHandler.ListDomains)
			protected.POST("/domains", domainHandler.CreateDomain)
			protected.GET("/domains/:id", domainHandler.GetDomain)
			protected.PUT("/domains/:id", domainHandler.UpdateDomain)
			protected.DELETE("/domains/:id", domainHandler.DeleteDomain)
			protected.POST("/domains/:id/verify-ns", domainHandler.VerifyNS)
			protected.POST("/domains/:id/test-routing", domainHandler.TestRouting)

			// Smart Routing Lines (global line definitions: continent / country / ASN)
			protected.GET("/routing-lines", routingHandler.ListLines)
			protected.POST("/routing-lines", routingHandler.CreateLine)
			protected.PUT("/routing-lines/:id", routingHandler.UpdateLine)
			protected.DELETE("/routing-lines/:id", routingHandler.DeleteLine)
			protected.POST("/routing-test", routingHandler.TestRouting)

			// Records
			protected.GET("/domains/:id/records", recordHandler.ListRecords)
			protected.POST("/domains/:id/records", recordHandler.CreateRecord)
			protected.PUT("/records/:id", recordHandler.UpdateRecord)
			protected.DELETE("/records/:id", recordHandler.DeleteRecord)
			protected.POST("/records/:id/toggle", recordHandler.ToggleRecord)
			protected.POST("/domains/:id/records/batch", recordHandler.BatchOperate)

			// DDNS 密钥管理（凭据的增删改查走控制台鉴权，与上面的更新端点分开）。
			// 路径刻意用 /ddns-keys/:id 而非 /ddns/:id：gin 不允许同层混用静态段
			// 与通配符，若管理接口占了 /ddns/:id 就会与 /api/ddns/update 冲突。
			protected.GET("/domains/:id/ddns-keys", ddnsHandler.ListDDNSKeys)
			protected.POST("/domains/:id/ddns-keys", ddnsHandler.CreateDDNSKey)
			protected.PUT("/ddns-keys/:id", ddnsHandler.UpdateDDNSKey)
			protected.DELETE("/ddns-keys/:id", ddnsHandler.DeleteDDNSKey)
			protected.POST("/ddns-keys/:id/rotate", ddnsHandler.RotateDDNSKey)

			// 域名安全防护（速率限制 / Flood / RRL / 黑名单 / ACL / AXFR /
			// 异常 QTYPE / ANY / 单域 QPS / NXDOMAIN 防护 / 日志与攻击统计）
			protected.GET("/domains/:id/security", securityHandler.GetSecurity)
			protected.PUT("/domains/:id/security", securityHandler.UpdateSecurity)
			protected.GET("/domains/:id/security/events", securityHandler.GetSecurityEvents)
			protected.POST("/domains/:id/security/reset-stats", securityHandler.ResetSecurityStats)
			protected.POST("/domains/:id/security/clear-bans", securityHandler.ClearSecurityBans)

			// DNSSEC
			protected.GET("/domains/:id/dnssec", dnssecHandler.GetDNSSEC)
			protected.POST("/domains/:id/dnssec/enable", dnssecHandler.EnableDNSSEC)
			protected.POST("/domains/:id/dnssec/disable", dnssecHandler.DisableDNSSEC)
			protected.POST("/domains/:id/dnssec/rotate", dnssecHandler.RotateKeys)

			// SSL Certificates
			protected.GET("/certificates", certHandler.ListCertificates)
			protected.GET("/certificates/providers", certHandler.ListProviders)
			protected.POST("/certificates/issue", certHandler.IssueCertificate)
			protected.POST("/certificates/:id/renew", certHandler.RenewCertificate)
			protected.PUT("/certificates/:id/auto-renew", certHandler.UpdateAutoRenew)
			protected.GET("/certificates/:id/download", certHandler.DownloadCertificate)
			protected.DELETE("/certificates/:id", certHandler.DeleteCertificate)

			// Certificate Applicants / Profiles
			protected.GET("/applicants", applicantHandler.ListApplicants)
			protected.POST("/applicants", applicantHandler.CreateApplicant)
			protected.PUT("/applicants/:id", applicantHandler.UpdateApplicant)
			protected.DELETE("/applicants/:id", applicantHandler.DeleteApplicant)

			// Health Checks
			protected.GET("/health-checks", healthHandler.ListHealthChecks)
			protected.POST("/health-checks", healthHandler.CreateHealthCheck)
			protected.PUT("/health-checks/:id", healthHandler.UpdateHealthCheck)
			protected.DELETE("/health-checks/:id", healthHandler.DeleteHealthCheck)
			protected.POST("/health-checks/:id/probe", healthHandler.ProbeNow)

			// Nodes Cluster Management
			protected.GET("/nodes", nodeHandler.ListNodes)
			protected.POST("/nodes", nodeHandler.CreateNode)
			protected.DELETE("/nodes/:id", nodeHandler.DeleteNode)
			protected.GET("/nodes/install-script", nodeHandler.GetInstallScript)
			protected.GET("/nodes/uptime", nodeHandler.NodeUptime)
			protected.GET("/nodes/metrics", nodeHandler.NodeMetrics)

			// Nameserver Server Management & Node Bindings
			protected.GET("/nameservers", nameserverHandler.ListNameservers)
			protected.POST("/nameservers", nameserverHandler.CreateNameserver)
			protected.PUT("/nameservers/:id", nameserverHandler.UpdateNameserver)
			protected.DELETE("/nameservers/:id", nameserverHandler.DeleteNameserver)

			// System Settings
			protected.GET("/settings", settingHandler.GetSettings)
			protected.PUT("/settings", settingHandler.UpdateSettings)

			// Stats & Analytics
			protected.GET("/stats/summary", statsHandler.GetSummary)
			protected.GET("/stats/qps-trend", statsHandler.GetQPSTrend)
			protected.GET("/stats/types", statsHandler.GetTypeBreakdown)
			protected.GET("/stats/geo", statsHandler.GetGeoBreakdown)
			protected.GET("/domains/:id/stats", statsHandler.GetDomainStats)
			protected.GET("/stats/carriers", statsHandler.GetCarrierBreakdown)
			protected.GET("/stats/audit-logs", statsHandler.GetAuditLogs)
		}
	}

	// Start HTTP API Server
	httpAddr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.HTTPPort)
	srv := &http.Server{
		Addr:    httpAddr,
		Handler: r,
	}

	go func() {
		fmt.Println()
		fmt.Println("================================================================================")
		fmt.Printf("  🐱 DnsCat Enterprise Authoritative DNS & Smart Anycast Engine %s\n", buildinfo.Short())
		fmt.Println("================================================================================")
		fmt.Printf("  [Web 控制台地址]  : http://%s\n", httpAddr)
		fmt.Printf("  [DNS 端口服务]    : :%d (UDP/TCP)\n", cfg.DNS.UDPPort)

		// 初始口令是随机生成的，只有「首次安装刚创建管理员」这一次能拿到明文，
		// 因此在安装日志里醒目打印一次；后续启动只提示如何重置，不再重复展示。
		if database.InitialAdminPassword != "" {
			fmt.Println("--------------------------------------------------------------------------------")
			fmt.Println("  首次安装已生成随机管理员口令，请立即保存（此口令仅显示这一次）：")
			fmt.Printf("  [管理员账号]      : %s\n", database.InitialAdminUsername)
			fmt.Printf("  [初始随机口令]    : %s\n", database.InitialAdminPassword)
			fmt.Println("  登录后请尽快在「系统设置」中改为自己的口令。")
			fmt.Println("--------------------------------------------------------------------------------")
		} else {
			fmt.Printf("  [管理员账号]      : 已存在（口令仅以 bcrypt 摘要存储，无法回显）\n")
			fmt.Printf("  [忘记口令]        : 在服务器终端执行 `dnscat reset-admin` 重置\n")
		}

		fmt.Printf("  [CLI 管理工具]    : 服务器终端执行 `dnscat` 可重置管理员口令/重启/更新服务\n")
		fmt.Println("================================================================================")
		fmt.Println()

		log.Printf("[API Server] Web API and Control Panel running on http://%s", httpAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[API Server] HTTP server error: %v", err)
		}
	}()

	// Graceful Shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Printf("Initiating graceful shutdown...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	_ = srv.Shutdown(shutdownCtx)
	// 优雅退出前把最后一次遥测刷入 Redis 与数据库，避免丢失最后一个刷写周期的增量。
	telemetryStore.FlushNow()
	// 同样把安全拦截计数刷入数据库，避免最后一个周期的攻击统计丢失。
	securityStore.FlushNow()
	// 安全日志同理：把最后一批事件刷进 Redis，使重启后能完整恢复。
	seclogStore.FlushNow()
	dnsServer.Stop()
	log.Printf("DnsCat Master Server gracefully stopped.")
}
