package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"dnscat/internal/buildinfo"
	"dnscat/internal/config"
	"dnscat/internal/database"
	"dnscat/internal/model"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var (
	configFile string
)

func main() {
	if len(os.Args) > 1 {
		subcommand := os.Args[1]
		switch subcommand {
		case "reset-admin":
			handleResetAdminCmd(os.Args[2:])
			return
		case "start":
			runServiceAction("start")
			return
		case "restart":
			runServiceAction("restart")
			return
		case "stop", "pause":
			runServiceAction("stop")
			return
		case "status":
			runServiceAction("status")
			return
		case "update":
			runServiceAction("update")
			return
		case "menu":
			interactiveMenu()
			return
		case "--help", "-h", "help":
			printHelp()
			return
		case "--version", "-v", "version":
			fmt.Println(buildinfo.Full("dnscat"))
			return
		}
	}

	// Default: interactive menu
	interactiveMenu()
}

func printHelp() {
	fmt.Print(`DnsCat Authoritative DNS Engine - CLI Management Tool

Usage:
  dnscat                   Open interactive management menu
  dnscat menu              Open interactive management menu
  dnscat reset-admin       Reset administrator credentials (no old password required)
  dnscat start             Start DnsCat service cluster
  dnscat restart           Restart DnsCat service cluster
  dnscat stop              Pause/Stop DnsCat service cluster
  dnscat status            Check DnsCat cluster running status
  dnscat update            Update & rebuild DnsCat service cluster
  dnscat version           Print version and exit
  dnscat help              Show this help message
`)
}

func interactiveMenu() {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Println()
		fmt.Println("==================================================================")
		fmt.Printf("  🐱 DnsCat 权威 DNS & 智能 Anycast 集群 CLI 控制台管理工具 %s\n", buildinfo.Short())
		fmt.Println("==================================================================")
		fmt.Println("  [1] 重置 / 修改管理员登录账号与密码 (无需原密码)")
		fmt.Println("  [2] 启动 DnsCat 服务集群 (Start)")
		fmt.Println("  [3] 重启 DnsCat 服务集群 (Restart)")
		fmt.Println("  [4] 暂停 / 停止 DnsCat 服务集群 (Stop/Pause)")
		fmt.Println("  [5] 查看服务运行状态 (Status & Ports)")
		fmt.Println("  [6] 检查并更新程序容器 (Update & Rebuild)")
		fmt.Println("  [7] 查看当前管理员账号信息")
		fmt.Println("  [0] 退出管理菜单 (Exit)")
		fmt.Println("==================================================================")
		fmt.Print("请输入操作序号 [0-7]: ")

		input, _ := reader.ReadString('\n')
		choice := strings.TrimSpace(input)

		switch choice {
		case "1":
			interactiveResetAdmin(reader)
		case "2":
			fmt.Println("\n[*] 正在启动 DnsCat 服务...")
			runServiceAction("start")
		case "3":
			fmt.Println("\n[*] 正在重启 DnsCat 服务...")
			runServiceAction("restart")
		case "4":
			fmt.Println("\n[*] 正在停止 DnsCat 服务...")
			runServiceAction("stop")
		case "5":
			fmt.Println("\n[*] 查看 DnsCat 运行状态:")
			runServiceAction("status")
		case "6":
			fmt.Println("\n[*] 正在检查并更新 DnsCat 程序...")
			runServiceAction("update")
		case "7":
			showAdminInfo()
		case "0", "exit", "q":
			fmt.Println("\n已退出 DnsCat CLI 管理控制台。")
			return
		default:
			fmt.Println("\n❌ 无效的输入序号，请重新选择。")
		}
	}
}

func interactiveResetAdmin(reader *bufio.Reader) {
	fmt.Println("\n--------------------------------------------------")
	fmt.Println("  重置管理员登录信息 (无需原密码验证)")
	fmt.Println("--------------------------------------------------")

	fmt.Print("请输入新的管理员用户名 [直接回车默认: admin]: ")
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)
	if username == "" {
		username = "admin"
	}

	fmt.Print("请输入新的管理员登录密码 [至少6位，直接回车自动生成随机强密码]: ")
	password, _ := reader.ReadString('\n')
	password = strings.TrimSpace(password)
	if password == "" {
		password = generateRandomPassword(12)
		fmt.Printf("已为您自动生成随机强密码: %s\n", password)
	} else if len(password) < 6 {
		fmt.Println("❌ 密码长度不能少于 6 位，重置失败。")
		return
	}

	db, err := getDBConnection()
	if err != nil {
		fmt.Printf("❌ 无法连接数据库: %v\n", err)
		return
	}
	noticeIfAdminJustCreated()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fmt.Printf("❌ 密码哈希生成失败: %v\n", err)
		return
	}

	var admin model.User
	if err := db.Where("role = ? OR username = ?", model.RoleAdmin, "admin").First(&admin).Error; err != nil {
		// Admin user not found, create new
		admin = model.User{
			Username:     username,
			Email:        username + "@dnscat.local",
			PasswordHash: string(hash),
			Role:         model.RoleAdmin,
			APIKey:       "dnscat_" + generateRandomPassword(16),
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
		if err := db.Create(&admin).Error; err != nil {
			fmt.Printf("❌ 创建管理员失败: %v\n", err)
			return
		}
	} else {
		// Update existing admin
		admin.Username = username
		admin.PasswordHash = string(hash)
		admin.UpdatedAt = time.Now()
		if err := db.Save(&admin).Error; err != nil {
			fmt.Printf("❌ 更新管理员密码失败: %v\n", err)
			return
		}
	}

	fmt.Println("\n==================================================")
	fmt.Println("  ✅ 管理员登录信息已成功重置！")
	fmt.Println("==================================================")
	fmt.Printf("  [Web 控制台地址] : http://<服务器公网IP>:8080\n")
	fmt.Printf("  [管理员用户名]   : %s\n", username)
	fmt.Printf("  [管理员新密码]   : %s\n", password)
	fmt.Println("==================================================")
}

func handleResetAdminCmd(args []string) {
	fs := flag.NewFlagSet("reset-admin", flag.ExitOnError)
	userFlag := fs.String("username", "admin", "New admin username")
	passFlag := fs.String("password", "", "New admin password")
	fs.Parse(args)

	username := *userFlag
	password := *passFlag
	if password == "" {
		password = generateRandomPassword(12)
	}

	db, err := getDBConnection()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot connect to database: %v\n", err)
		os.Exit(1)
	}
	noticeIfAdminJustCreated()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to hash password: %v\n", err)
		os.Exit(1)
	}

	var admin model.User
	if err := db.Where("role = ? OR username = ?", model.RoleAdmin, "admin").First(&admin).Error; err != nil {
		admin = model.User{
			Username:     username,
			Email:        username + "@dnscat.local",
			PasswordHash: string(hash),
			Role:         model.RoleAdmin,
			APIKey:       "dnscat_" + generateRandomPassword(16),
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
		if err := db.Create(&admin).Error; err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to create admin: %v\n", err)
			os.Exit(1)
		}
	} else {
		admin.Username = username
		admin.PasswordHash = string(hash)
		admin.UpdatedAt = time.Now()
		if err := db.Save(&admin).Error; err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to update admin: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Println("Admin credentials reset successfully.")
	fmt.Printf("Username: %s\n", username)
	fmt.Printf("Password: %s\n", password)
}

func showAdminInfo() {
	db, err := getDBConnection()
	if err != nil {
		fmt.Printf("❌ 无法连接数据库: %v\n", err)
		return
	}
	noticeIfAdminJustCreated()

	var admins []model.User
	if err := db.Where("role = ?", model.RoleAdmin).Find(&admins).Error; err != nil || len(admins) == 0 {
		fmt.Println("⚠️ 数据库中暂未查询到管理员账号。")
		return
	}

	fmt.Println("\n--- [ 当前系统管理员列表 ] ---")
	for _, a := range admins {
		fmt.Printf("  ID: %d | 用户名: %s | 邮箱: %s | 创建时间: %s\n",
			a.ID, a.Username, a.Email, a.CreatedAt.Format("2006-01-02 15:04:05"))
	}
}

// noticeIfAdminJustCreated 处理一个容易锁死的边界情况：
// 连接数据库会触发 database.InitDB 里的初始化，若此前没有管理员账号，
// 本次连接就会顺带创建一个带随机口令的 admin。如果不在这里打印出来，
// 这个口令就永久丢失了（库里只有 bcrypt 摘要），使用者只能再去 reset-admin。
func noticeIfAdminJustCreated() {
	if database.InitialAdminPassword == "" {
		return
	}
	fmt.Println()
	fmt.Println("==================================================")
	fmt.Println("  检测到数据库中尚无管理员，已创建初始账号")
	fmt.Println("==================================================")
	fmt.Printf("  [管理员账号]   : %s\n", database.InitialAdminUsername)
	fmt.Printf("  [初始随机口令] : %s\n", database.InitialAdminPassword)
	fmt.Println("  此口令仅显示这一次，请立即保存。")
	fmt.Println("==================================================")
}

func getDBConnection() (*gorm.DB, error) {
	if database.DB != nil {
		return database.DB, nil
	}

	// 1. Check environment variable
	if envDSN := os.Getenv("DNSCAT_DB_DSN"); envDSN != "" {
		hostDSN := strings.ReplaceAll(envDSN, "mysql:3306", "127.0.0.1:3306")
		cfg := config.DefaultConfig()
		cfg.Database.Driver = "mysql"
		cfg.Database.DSN = hostDSN
		if db, err := database.InitDB(cfg); err == nil {
			return db, nil
		}
	}

	// 2. Candidate configuration file paths
	candidates := []string{
		"/etc/dnscat/config.yaml", // 二进制安装（install.sh）的默认位置
		"/app/config.yaml",        // 容器内位置
		"./deploy/config.yaml",
		"./config.yaml",
		"config.yaml",
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			if c, err := config.LoadConfig(path); err == nil {
				// If DSN contains container name 'mysql:3306', replace with '127.0.0.1:3306' when running on host
				c.Database.DSN = strings.ReplaceAll(c.Database.DSN, "@tcp(mysql:3306)", "@tcp(127.0.0.1:3306)")
				if db, err := database.InitDB(c); err == nil {
					return db, nil
				}
			}
		}
	}

	// 3. 最后兜底：按内置默认配置连接（sqlite 本地库）。
	// 这里刻意不再尝试任何内置的 MySQL 账号口令组合——猜口令既连不上正确的库，
	// 又可能连到同机上另一个无关的数据库。连不上就明确报错，让使用者指定配置。
	cfg := config.DefaultConfig()
	return database.InitDB(cfg)
}

// systemdUnits 是 install.sh 二进制安装模式会装上的 systemd 单元。
// 主控与被控各装一个，同机可能只有其中之一。
var systemdUnits = []string{"dnscat-server", "dnscat-node"}

// detectSystemdUnits 返回本机实际存在的 DnsCat systemd 单元。
// 返回空表示不是二进制安装（或没有 systemd），调用方应回退到 Docker Compose。
func detectSystemdUnits() []string {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return nil
	}
	var found []string
	for _, u := range systemdUnits {
		for _, dir := range []string{"/etc/systemd/system", "/lib/systemd/system", "/usr/lib/systemd/system"} {
			if _, err := os.Stat(filepath.Join(dir, u+".service")); err == nil {
				found = append(found, u)
				break
			}
		}
	}
	return found
}

// findComposeFile 定位 Docker 部署使用的 compose 文件（主控或被控）。
// 返回 (工作目录, 文件名)；找不到时文件名为空。
func findComposeFile() (string, string) {
	dirs := []string{"/opt/dnscat/deploy", "/root/dnscat/deploy", "./deploy", "."}
	files := []string{"docker-compose.master.yml", "docker-compose.node.yml"}
	for _, d := range dirs {
		for _, f := range files {
			if _, err := os.Stat(filepath.Join(d, f)); err == nil {
				return d, f
			}
		}
	}
	return "", ""
}

// composeBaseArgs 优先使用 Docker Compose v2（`docker compose`），
// 仅在只有 v1 独立二进制时回退到 `docker-compose`。
func composeBaseArgs() []string {
	if _, err := exec.LookPath("docker"); err == nil {
		probe := exec.Command("docker", "compose", "version")
		if probe.Run() == nil {
			return []string{"docker", "compose"}
		}
	}
	if _, err := exec.LookPath("docker-compose"); err == nil {
		return []string{"docker-compose"}
	}
	return nil
}

// runServiceAction 按语义动作操作服务，自动适配两种安装方式：
// 二进制安装走 systemctl，Docker 安装走 compose。
// action 取值：start / restart / stop / status / update
func runServiceAction(action string) {
	if units := detectSystemdUnits(); len(units) > 0 {
		runSystemdAction(units, action)
		return
	}
	runComposeAction(action)
}

func runSystemdAction(units []string, action string) {
	var verb string
	switch action {
	case "start":
		verb = "start"
	case "restart":
		verb = "restart"
	case "stop":
		verb = "stop"
	case "status":
		verb = "status"
	case "update":
		// 二进制安装的「更新」需要重新下载/替换二进制，超出 CLI 职责范围。
		fmt.Println("检测到二进制安装（systemd）。更新请重新执行安装脚本：")
		fmt.Println("  curl -fsSL <仓库地址>/deploy/install.sh | bash -s -- --upgrade")
		fmt.Println("随后本工具的 restart 即可载入新版本。")
		return
	default:
		fmt.Printf("未知操作: %s\n", action)
		return
	}

	for _, u := range units {
		args := []string{verb, u}
		if verb == "status" {
			args = []string{"status", u, "--no-pager"}
		}
		fmt.Printf("[Command] systemctl %s\n", strings.Join(args, " "))
		cmd := exec.Command("systemctl", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
	}
}

func runComposeAction(action string) {
	base := composeBaseArgs()
	if base == nil {
		fmt.Println("未检测到 systemd 单元，也未找到 docker compose。")
		fmt.Println("请确认 DnsCat 已通过 deploy/install.sh 安装。")
		return
	}

	dir, file := findComposeFile()
	if file == "" {
		fmt.Println("未找到 docker-compose.master.yml 或 docker-compose.node.yml。")
		fmt.Println("请在部署目录内执行本命令，或改用 systemd 方式安装。")
		return
	}

	var tail []string
	switch action {
	case "start":
		tail = []string{"up", "-d"}
	case "restart":
		tail = []string{"restart"}
	case "stop":
		tail = []string{"stop"}
	case "status":
		tail = []string{"ps"}
	case "update":
		tail = []string{"up", "-d", "--build"}
	default:
		fmt.Printf("未知操作: %s\n", action)
		return
	}

	args := append(append([]string{}, base[1:]...), "-f", file)
	args = append(args, tail...)
	fmt.Printf("[Command] cd %s && %s %s\n", dir, base[0], strings.Join(args, " "))

	cmd := exec.Command(base[0], args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

func generateRandomPassword(length int) string {
	b := make([]byte, length/2)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
