package api

import (
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"dnscat/internal/cluster"
	"dnscat/internal/database"
	"dnscat/internal/dnsengine"
	"dnscat/internal/model"

	"github.com/gin-gonic/gin"
)

// dyndns2 协议状态码。客户端（ddclient / inadyn / 路由器固件）解析的是响应体的
// 这些字面量而不是 HTTP 状态码，因此文本必须严格照标准书写。
const (
	ddnsStatusGood        = "good"    // 更新成功，后跟生效的 IP
	ddnsStatusNoChange    = "nochg"   // IP 未变化，后跟当前 IP
	ddnsStatusNoHost      = "nohost"  // 主机名不存在或不在本密钥授权范围内
	ddnsStatusBadAuth     = "badauth" // 认证失败
	ddnsStatusNotYours    = "!yours"  // 主机名存在但不属于该密钥所属域名
	ddnsStatusNotFQDN     = "notfqdn" // 主机名格式非法
	ddnsStatusAbuse       = "abuse"   // 触发滥用保护（此处用于失败限流）
	ddnsStatusServerError = "dnserr"  // 服务端错误

	// 内部状态：仅写入密钥的 LastStatus 供控制台展示，绝不回给客户端。
	// 对外统一回 badauth，避免攻击者借此区分「密钥有效但来源被拒」。
	ddnsStatusNotAllowedIP = "denied_ip"
)

type DDNSHandler struct{}

func NewDDNSHandler() *DDNSHandler {
	return &DDNSHandler{}
}

// hostnameAuthorized 判断主机名（相对名）是否在密钥的授权列表内。
// "*" 放行该域名下任意主机名；其余按不区分大小写的精确匹配。
func hostnameAuthorized(allowed string, relativeName string) bool {
	entries := splitDDNSList(allowed)
	if len(entries) == 0 {
		return false
	}
	target := strings.ToLower(strings.TrimSpace(relativeName))
	for _, entry := range entries {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "*" {
			return true
		}
		if entry == target {
			return true
		}
	}
	return false
}

// NormalizeDDNSHostnames 归一化主机名授权列表：去掉可能带上的 zone 后缀、
// 空名与根点统一成 "@"，并去重。写入前调用，让库里存的一律是相对名。
func NormalizeDDNSHostnames(raw string, zone string) string {
	zone = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(zone), "."))
	entries := splitDDNSList(raw)
	seen := make(map[string]struct{}, len(entries))
	out := make([]string, 0, len(entries))

	for _, entry := range entries {
		name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(entry), "."))
		if name == "" || name == "@" {
			name = "@"
		} else if name != "*" && zone != "" {
			// 用户可能直接填了完整 FQDN，剥掉 zone 后缀存相对名。
			if name == zone {
				name = "@"
			} else if strings.HasSuffix(name, "."+zone) {
				name = strings.TrimSuffix(name, "."+zone)
			}
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return strings.Join(out, ",")
}

// relativeNameFor 把客户端提交的主机名换算成区域内的相对名。
// 返回的 ok 为 false 表示该主机名不属于本密钥绑定的域名。
func relativeNameFor(hostname string, zone string) (string, bool) {
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(hostname), "."))
	zone = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(zone), "."))
	if host == "" || zone == "" {
		return "", false
	}
	if host == zone {
		return "@", true
	}
	if strings.HasSuffix(host, "."+zone) {
		return strings.TrimSuffix(host, "."+zone), true
	}
	return "", false
}

// parseDDNSAddresses 解析客户端提交的地址。
//
// dyndns2 的 myip 允许逗号分隔并同时携带 v4 与 v6；部分客户端改用 myipv6 或 ip。
// 全部缺省时回退到请求的来源地址，这是路由器固件最常见的用法（自己不知道公网 IP）。
func parseDDNSAddresses(c *gin.Context, clientIP string) (v4 string, v6 string, invalid []string) {
	raw := ""
	for _, param := range []string{"myip", "ip", "myipv6", "ipv6", "addr"} {
		if val := strings.TrimSpace(c.Query(param)); val != "" {
			if raw == "" {
				raw = val
			} else {
				raw += "," + val
			}
		}
	}

	if raw == "" {
		raw = clientIP
	}

	for _, candidate := range splitDDNSList(raw) {
		ip := net.ParseIP(candidate)
		if ip == nil {
			invalid = append(invalid, candidate)
			continue
		}
		if ip.To4() != nil {
			if v4 == "" {
				v4 = ip.To4().String()
			}
			continue
		}
		if v6 == "" {
			v6 = ip.String()
		}
	}
	return v4, v6, invalid
}

// ddnsUpdateOutcome 记录单条记录的处理结果。
type ddnsUpdateOutcome struct {
	Status  string // good / nochg / nohost / dnserr
	IP      string
	Changed bool
}

// Update 实现 dyndns2 的 /nic/update。
//
// 挂在公开路径上，凭密钥自证身份；密钥的作用域、来源白名单与失败限流
// 都在 authenticateDDNS 里完成。
func (h *DDNSHandler) Update(c *gin.Context) {
	auth := authenticateDDNS(c)
	if auth.Status != "" {
		writeDDNSResponse(c, auth.Status)
		return
	}

	key := auth.Key
	domain := auth.Domain
	userAgent := c.Request.UserAgent()

	// 主机名缺省时按密钥授权列表推断：只授权了一个具体主机名的话无需客户端再传，
	// 这让不支持自定义 hostname 参数的简易客户端也能用。
	hostnameParam := strings.TrimSpace(c.Query("hostname"))
	if hostnameParam == "" {
		hostnameParam = strings.TrimSpace(c.Query("host"))
	}
	if hostnameParam == "" {
		allowed := splitDDNSList(key.Hostnames)
		if len(allowed) == 1 && allowed[0] != "*" {
			if allowed[0] == "@" {
				hostnameParam = domain.Name
			} else {
				hostnameParam = allowed[0] + "." + domain.Name
			}
		} else {
			h.finishUpdate(c, key, auth.ClientIP, userAgent, ddnsStatusNotFQDN, "", "", false)
			return
		}
	}

	v4, v6, invalidAddrs := parseDDNSAddresses(c, auth.ClientIP)
	if len(invalidAddrs) > 0 && v4 == "" && v6 == "" {
		h.finishUpdate(c, key, auth.ClientIP, userAgent, ddnsStatusServerError, "", "", false)
		return
	}
	if v4 == "" && v6 == "" {
		h.finishUpdate(c, key, auth.ClientIP, userAgent, ddnsStatusServerError, "", "", false)
		return
	}

	hostnames := splitDDNSList(hostnameParam)
	if len(hostnames) == 0 {
		h.finishUpdate(c, key, auth.ClientIP, userAgent, ddnsStatusNotFQDN, "", "", false)
		return
	}

	responses := make([]string, 0, len(hostnames))
	anyChanged := false
	appliedV4, appliedV6 := "", ""

	for _, hostname := range hostnames {
		relative, belongs := relativeNameFor(hostname, domain.Name)
		if !belongs {
			// 主机名不在本密钥绑定的域名下：这是明确的越权尝试，按协议回 !yours。
			responses = append(responses, ddnsStatusNotYours)
			continue
		}
		if !hostnameAuthorized(key.Hostnames, relative) {
			responses = append(responses, ddnsStatusNoHost)
			continue
		}

		// 一次调用可能同时维护 A 与 AAAA（双栈客户端），逐族处理。
		perHost := make([]ddnsUpdateOutcome, 0, 2)
		if v4 != "" && key.AllowIPv4 {
			perHost = append(perHost, h.applyRecord(domain, key, relative, model.RecordTypeA, v4))
		}
		if v6 != "" && key.AllowIPv6 {
			perHost = append(perHost, h.applyRecord(domain, key, relative, model.RecordTypeAAAA, v6))
		}

		if len(perHost) == 0 {
			// 客户端提交的地址族被密钥禁用（例如只允许 IPv4 却只送来 IPv6）。
			responses = append(responses, ddnsStatusNoHost)
			continue
		}

		// 同一主机名下任一地址族发生变化即视为 good，全部未变才是 nochg。
		hostStatus := ddnsStatusNoChange
		hostIP := ""
		for _, outcome := range perHost {
			if outcome.Status == ddnsStatusServerError {
				hostStatus = ddnsStatusServerError
				hostIP = ""
				break
			}
			if outcome.Status == ddnsStatusNoHost {
				hostStatus = ddnsStatusNoHost
				hostIP = ""
				break
			}
			if outcome.Changed {
				anyChanged = true
				hostStatus = ddnsStatusGood
			}
			if hostIP == "" {
				hostIP = outcome.IP
			}
		}

		if hostStatus == ddnsStatusGood || hostStatus == ddnsStatusNoChange {
			if v4 != "" && key.AllowIPv4 {
				appliedV4 = v4
			}
			if v6 != "" && key.AllowIPv6 {
				appliedV6 = v6
			}
			responses = append(responses, hostStatus+" "+hostIP)
		} else {
			responses = append(responses, hostStatus)
		}
	}

	if anyChanged {
		// 与所有记录写入路径一致的收尾：重建本机内存区域，并通知其它主控副本。
		// 边缘节点通过 10s 周期的全量快照同步跟进。
		dnsengine.GlobalZoneStore.InvalidateZone(domain.ID)
		if cluster.GlobalCluster != nil {
			cluster.GlobalCluster.BroadcastZoneChange(domain.ID)
		}
		// SOA serial 递增，让下游辅助 DNS 与缓存能感知区域已变更。
		_ = database.DB.Model(&model.Domain{}).Where("id = ?", domain.ID).
			Update("soa_serial", domain.SOASerial+1).Error
	}

	finalStatus := ddnsStatusNoChange
	if anyChanged {
		finalStatus = ddnsStatusGood
	}
	h.recordKeyUsage(key, auth.ClientIP, userAgent, finalStatus, appliedV4, appliedV6, anyChanged)

	// dyndns2 规定多主机名时每行一个结果，顺序与请求一致。
	c.String(200, strings.Join(responses, "\n"))
}

// applyRecord 把某个地址族的新地址落到对应记录上。
//
// 记录定位刻意只认默认线路：同名同类型可以存在多条记录（智能分线 / 加权地址池），
// 让 DDNS 去改分线记录会破坏用户精心配置的流量调度，也无法判断该改哪一条。
func (h *DDNSHandler) applyRecord(
	domain *model.Domain,
	key *model.DDNSKey,
	relativeName string,
	recordType model.RecordType,
	newIP string,
) ddnsUpdateOutcome {
	var records []model.Record
	err := database.DB.Where(
		"domain_id = ? AND name = ? AND type = ? AND (geo_line = ? OR geo_line = ? OR geo_line IS NULL)",
		domain.ID, relativeName, recordType, "default", "",
	).Order("id asc").Find(&records).Error
	if err != nil {
		log.Printf("[DDNS] 查询记录失败 zone=%s name=%s type=%s: %v", domain.Name, relativeName, recordType, err)
		return ddnsUpdateOutcome{Status: ddnsStatusServerError}
	}

	ttl := key.RecordTTL
	if ttl == 0 {
		// ConvertRecordToRR 会把 0 与 1 都归一成 300，想要短 TTL 必须写实数。
		ttl = 60
	}

	if len(records) == 0 {
		if !key.AutoCreate {
			return ddnsUpdateOutcome{Status: ddnsStatusNoHost}
		}
		rec := model.Record{
			DomainID:  domain.ID,
			Name:      relativeName,
			Type:      recordType,
			Value:     newIP,
			TTL:       ttl,
			Weight:    100,
			GeoLine:   "default",
			Enabled:   true,
			Comment:   "DDNS: " + key.Name,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := database.DB.Create(&rec).Error; err != nil {
			log.Printf("[DDNS] 创建记录失败 zone=%s name=%s: %v", domain.Name, relativeName, err)
			return ddnsUpdateOutcome{Status: ddnsStatusServerError}
		}
		writeDDNSAudit(key, &rec, "", newIP, domain.Name, relativeName)
		return ddnsUpdateOutcome{Status: ddnsStatusGood, IP: newIP, Changed: true}
	}

	if len(records) > 1 {
		// 默认线路上存在多条同类型记录（加权地址池）。自动挑一条去改会静默
		// 破坏用户的负载均衡配置，因此拒绝并让用户收敛成单条。
		log.Printf("[DDNS] %s.%s 的 %s 记录在默认线路上有 %d 条，无法确定更新目标",
			relativeName, domain.Name, recordType, len(records))
		return ddnsUpdateOutcome{Status: ddnsStatusNoHost}
	}

	rec := records[0]
	if strings.EqualFold(strings.TrimSpace(rec.Value), newIP) && rec.Enabled {
		// IP 没变是 DDNS 客户端的常态（每几分钟轮询一次）。此处必须直接返回：
		// 不写库、不重建区域、不广播、不写审计，否则正常心跳会持续放大成
		// 全区域重载与全表审计写入。
		return ddnsUpdateOutcome{Status: ddnsStatusNoChange, IP: newIP}
	}

	oldValue := rec.Value
	err = database.DB.Model(&model.Record{}).Where("id = ?", rec.ID).Updates(map[string]interface{}{
		"value":      newIP,
		"ttl":        ttl,
		"enabled":    true,
		"updated_at": time.Now(),
	}).Error
	if err != nil {
		log.Printf("[DDNS] 更新记录失败 id=%d: %v", rec.ID, err)
		return ddnsUpdateOutcome{Status: ddnsStatusServerError}
	}

	writeDDNSAudit(key, &rec, oldValue, newIP, domain.Name, relativeName)
	return ddnsUpdateOutcome{Status: ddnsStatusGood, IP: newIP, Changed: true}
}

// writeDDNSAudit 仅在地址真正变化时写审计。DDNS 客户端的常态是高频上报同一个 IP，
// 无条件记录会让审计表迅速被无意义的 nochg 填满。
func writeDDNSAudit(key *model.DDNSKey, rec *model.Record, oldValue, newValue, zone, relativeName string) {
	fqdn := zone
	if relativeName != "@" {
		fqdn = relativeName + "." + zone
	}
	detail := fmt.Sprintf("%s %s: %s -> %s (密钥 %s)", fqdn, rec.Type, oldValue, newValue, key.Name)
	if oldValue == "" {
		detail = fmt.Sprintf("%s %s: 新建为 %s (密钥 %s)", fqdn, rec.Type, newValue, key.Name)
	}

	entry := model.AuditLog{
		UserID:     key.UserID,
		Username:   "ddns:" + key.KeyPrefix,
		Action:     "ddns_update",
		TargetType: "record",
		TargetID:   fmt.Sprintf("%d", rec.ID),
		Detail:     detail,
		IP:         key.LastClientIP,
		CreatedAt:  time.Now(),
	}
	if err := database.DB.Create(&entry).Error; err != nil {
		log.Printf("[DDNS] 写入审计日志失败: %v", err)
	}
}

// recordKeyUsage 更新密钥的最近使用留痕，供控制台判断客户端是否在正常工作。
func (h *DDNSHandler) recordKeyUsage(
	key *model.DDNSKey,
	clientIP, userAgent, status, v4, v6 string,
	changed bool,
) {
	now := time.Now()
	updates := map[string]interface{}{
		"last_client_ip":  clientIP,
		"last_user_agent": truncateDDNSString(userAgent, 255),
		"last_status":     status,
		"last_used_at":    &now,
		"updated_at":      now,
	}
	if changed {
		updates["update_count"] = key.UpdateCount + 1
	}
	if v4 != "" {
		updates["last_ipv4"] = v4
	}
	if v6 != "" {
		updates["last_ipv6"] = v6
	}
	_ = database.DB.Model(&model.DDNSKey{}).Where("id = ?", key.ID).Updates(updates).Error
}

// finishUpdate 在早退场景下同时留痕并回应，避免各分支重复代码。
func (h *DDNSHandler) finishUpdate(
	c *gin.Context,
	key *model.DDNSKey,
	clientIP, userAgent, status, v4, v6 string,
	changed bool,
) {
	h.recordKeyUsage(key, clientIP, userAgent, status, v4, v6, changed)
	writeDDNSResponse(c, status)
}
