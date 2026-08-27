package database

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"dnscat/internal/config"
	"dnscat/internal/model"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

// InitialAdminUsername / InitialAdminPassword 仅在「本次启动刚创建了管理员账号」时被赋值，
// 供 cmd/server 的启动横幅打印一次，让首次安装的使用者能从安装日志里拿到登录凭据。
// 已存在管理员时保持为空字符串——数据库里只有 bcrypt 摘要，无法也不应该反推出明文。
var (
	InitialAdminUsername string
	InitialAdminPassword string
)

func InitDB(cfg *config.Config) (*gorm.DB, error) {
	var dialector gorm.Dialector

	if cfg.Database.Driver == "mysql" {
		dialector = mysql.Open(cfg.Database.DSN)
	} else {
		dialector = sqlite.Open(cfg.Database.DSN)
	}

	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	}

	db, err := gorm.Open(dialector, gormConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}

	sqlDB, err := db.DB()
	if err == nil {
		sqlDB.SetMaxIdleConns(10)
		sqlDB.SetMaxOpenConns(100)
		sqlDB.SetConnMaxLifetime(time.Hour)
	}

	// Auto Migrate
	err = db.AutoMigrate(
		&model.User{},
		&model.Domain{},
		&model.Record{},
		&model.DDNSKey{},
		&model.DNSSECKey{},
		&model.HealthCheck{},
		&model.Node{},
		&model.NodeStatusSample{},
		&model.Certificate{},
		&model.CertApplicant{},
		&model.SystemSetting{},
		&model.Nameserver{},
		&model.AuditLog{},
		&model.RoutingLine{},
		&model.TelemetrySnapshot{},
		&model.DomainSecurityPolicy{},
		&model.DomainSecurityStat{},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to auto-migrate tables: %w", err)
	}

	DB = db

	// 初始化必要的系统数据（管理员账号、系统设置、内置线路）。不含任何演示业务数据。
	seedData(db)

	return db, nil
}

func seedData(db *gorm.DB) {
	var admin model.User
	if err := db.Where("username = ?", "admin").First(&admin).Error; err != nil {
		// 首次安装才会走到这里：口令随机生成，不使用任何出厂默认值。
		// 出厂默认口令等于把控制台交给任何知道这个项目的人，对公网暴露的服务不可接受。
		password := generateRandomPassword(20)
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if hashErr != nil {
			log.Printf("[Database ERROR] 生成管理员口令摘要失败: %v", hashErr)
			return
		}
		admin = model.User{
			Username:     "admin",
			Email:        "admin@dnscat.local",
			PasswordHash: string(hash),
			Role:         model.RoleAdmin,
			APIKey:       "dnscat_" + generateRandomKey(16),
		}
		if err := db.Create(&admin).Error; err != nil {
			log.Printf("[Database ERROR] 创建初始管理员失败: %v", err)
		} else {
			// 明文只在本次进程内存里存活，用于启动横幅打印一次；不写库、不写日志文件之外的任何位置。
			InitialAdminUsername = admin.Username
			InitialAdminPassword = password
			log.Printf("[Database] 已创建初始管理员账号 %q（随机口令见启动横幅）", admin.Username)
		}
	}

	// 说明：这里刻意不再播种任何演示数据（示例域名 example.com、假边缘节点、
	// 假权威 NS）。原因：
	//   1. 假节点带着伪造的 CPU/内存/QPS 指标，在集群页里与真实节点无法区分；
	//   2. 示例域名会随集群快照同步到所有边缘节点，导致本系统对外权威应答一个
	//      并不属于使用者的域名（example.com），这是真实的 DNS 卫生问题；
	//   3. 假 NS 记录指向固定内网 IP，会污染域名的 NS 校验期望列表。
	// 全新安装保持干净，由使用者自行添加域名、节点与 NS。

	// Seed default system settings
	var settingCount int64
	db.Model(&model.SystemSetting{}).Count(&settingCount)
	if settingCount == 0 {
		settings := []model.SystemSetting{
			// 留空：权威 NS 主机名与 ACME 联系邮箱因部署而异，任何出厂值都是错的。
			// 留空时新建域名的 NS 由使用者在「权威 NS 服务器」页配置后生成。
			{Key: "default_ns", Value: "", Description: "Default authoritative nameservers for new domains (comma separated, e.g. ns1.example.com.,ns2.example.com.)"},
			{Key: "default_ttl", Value: "300", Description: "Default record TTL (seconds)"},
			{Key: "acme_email", Value: "", Description: "Contact email for ACME Let's Encrypt certificates"},
			{Key: "acme_ca_url", Value: "", Description: "ACME Directory 覆盖地址（留空则按服务商自动选择；填写后强制使用，用于私有 CA）"},
			{Key: "acme_staging", Value: "false", Description: "启用 ACME 测试环境（避免消耗生产环境速率配额，签发的证书不受浏览器信任）"},
			{Key: "rate_limit", Value: "1000", Description: "RRL max queries per second per client IP"},
			{Key: "prober_interval", Value: "15", Description: "Health check prober interval (seconds)"},
			{Key: "allow_register", Value: "true", Description: "Allow public user registration"},
		}
		db.Create(&settings)
	}

	// 说明：不播种证书申请人。早期版本会插入两条示例申请人（带虚构的邮箱与组织名），
	// 但申请人邮箱会真实提交给 ACME 服务商用于证书到期通知，填错等于收不到通知；
	// 组织名也会出现在申请记录里。这类信息只能由使用者自己填，出厂值有害无益。

	// 规范化内置智能分线线路：仅锁定「全球默认 + 七大洲」共 8 条，
	// 其余历史内置线路（具体国家 / 运营商 ASN 等）解锁为可自由增删改的普通线路。
	// 幂等，每次启动执行。
	normalizeBuiltinRoutingLines(db)

	// 为存量证书回填对外 UUID（新证书由 model.Certificate.BeforeCreate 钩子生成）。
	backfillCertificateUUIDs(db)
}

// builtinRoutingLines 返回唯一一组「内置且锁定」的智能分线线路：
// 全球默认（兜底）+ 七大洲。除此之外的线路一律视为用户可自由增删改的普通线路。
func builtinRoutingLines() []model.RoutingLine {
	return []model.RoutingLine{
		{Key: "default", Name: "全球默认 (Global Default)", Description: "兜底线路，覆盖所有其他规则未命中的国家与地区", Priority: 1000, Enabled: true, IsBuiltin: true},
		{Key: "asia", Name: "亚洲 (Asia)", Description: "亚洲大洲线路", Continents: "AS", Priority: 200, Enabled: true, IsBuiltin: true},
		{Key: "europe", Name: "欧洲 (Europe)", Description: "欧洲大洲线路", Continents: "EU", Priority: 200, Enabled: true, IsBuiltin: true},
		{Key: "north-america", Name: "北美洲 (North America)", Description: "北美洲大洲线路", Continents: "NA", Priority: 200, Enabled: true, IsBuiltin: true},
		{Key: "south-america", Name: "南美洲 (South America)", Description: "南美洲大洲线路", Continents: "SA", Priority: 200, Enabled: true, IsBuiltin: true},
		{Key: "africa", Name: "非洲 (Africa)", Description: "非洲大洲线路", Continents: "AF", Priority: 200, Enabled: true, IsBuiltin: true},
		{Key: "oceania", Name: "大洋洲 (Oceania)", Description: "大洋洲大洲线路", Continents: "OC", Priority: 200, Enabled: true, IsBuiltin: true},
		{Key: "antarctica", Name: "南极洲 (Antarctica)", Description: "南极洲大洲线路", Continents: "AN", Priority: 200, Enabled: true, IsBuiltin: true},
	}
}

// normalizeBuiltinRoutingLines 保证内置锁定线路恰好为「全球默认 + 七大洲」。
// 幂等，每次启动执行：
//  1. 早期版本 seed 出的其它内置线路（中国 / 运营商 / 具体国家等）解锁为普通线路，
//     数据保留，用户可自行编辑或删除；
//  2. 缺失的内置线路补建（如老库缺少南极洲线路）；
//  3. 已存在的内置线路仅确保其锁定标记，不覆盖用户对名称 / 优先级 / 启用状态 / 维度的修改。
func normalizeBuiltinRoutingLines(db *gorm.DB) {
	builtins := builtinRoutingLines()
	keys := make([]string, 0, len(builtins))
	for _, b := range builtins {
		keys = append(keys, b.Key)
	}

	// 1) 解锁不属于白名单的历史内置线路（仅去掉锁定标记，保留线路本身）。
	db.Model(&model.RoutingLine{}).
		Where("is_builtin = ? AND `key` NOT IN ?", true, keys).
		Update("is_builtin", false)

	// 2) 确保 8 条内置线路存在且锁定。
	for i := range builtins {
		b := builtins[i]
		var existing model.RoutingLine
		if err := db.Where("`key` = ?", b.Key).First(&existing).Error; err != nil {
			db.Create(&b)
			continue
		}
		if !existing.IsBuiltin {
			db.Model(&model.RoutingLine{}).Where("id = ?", existing.ID).Update("is_builtin", true)
		}
	}
}

// backfillCertificateUUIDs 为历史证书补全对外 UUID（新证书由 BeforeCreate 钩子生成）。
// 幂等：仅处理 uuid 为空的行，每次启动执行。
func backfillCertificateUUIDs(db *gorm.DB) {
	var certs []model.Certificate
	if err := db.Where("uuid = ? OR uuid IS NULL", "").Find(&certs).Error; err != nil {
		log.Printf("[Database] 查询待回填 UUID 的证书失败: %v", err)
		return
	}
	for i := range certs {
		if err := db.Model(&model.Certificate{}).
			Where("id = ?", certs[i].ID).
			Update("uuid", model.NewUUID()).Error; err != nil {
			log.Printf("[Database] 回填证书 #%d UUID 失败: %v", certs[i].ID, err)
		}
	}
	if len(certs) > 0 {
		log.Printf("[Database] 已为 %d 张历史证书回填对外 UUID", len(certs))
	}
}

func generateRandomKey(length int) string {
	b := make([]byte, length)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// passwordAlphabet 刻意剔除了容易被人眼或抄写弄混的字符（0/O、1/l/I、B/8、S/5、Z/2），
// 因为这个口令要从安装日志里被人工抄到浏览器。共 47 个字符，20 位约合 111 bit 熵。
const passwordAlphabet = "ACDEFGHJKLMNPQRTUVWXYacdefghijkmnpqrtuvwxy34679"

// generateRandomPassword 用 crypto/rand 生成初始管理员口令。
// 采用拒绝采样消除取模偏置，保证在字母表上均匀分布。
func generateRandomPassword(length int) string {
	if length <= 0 {
		length = 20
	}
	n := len(passwordAlphabet)
	limit := 256 - (256 % n) // 落在 [limit,256) 的字节一律丢弃，避免取模偏置
	out := make([]byte, 0, length)
	buf := make([]byte, length)
	for len(out) < length {
		if _, err := rand.Read(buf); err != nil {
			// crypto/rand 不可用时没有安全的降级方案：宁可返回空让上层报错，
			// 也绝不退回到可预测的伪随机口令。
			return ""
		}
		for _, c := range buf {
			if int(c) < limit {
				out = append(out, passwordAlphabet[int(c)%n])
				if len(out) == length {
					break
				}
			}
		}
	}
	return string(out)
}
