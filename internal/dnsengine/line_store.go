package dnsengine

import (
	"strings"
	"sync"

	"dnscat/internal/database"
	"dnscat/internal/geo"
	"dnscat/internal/model"
)

// LineStore 缓存全局智能分线线路定义，供 DNS 解析时按线路 key 匹配客户端定位。
// 主节点从数据库加载；边缘节点通过集群快照的 RoutingLines 字段获得。
type LineStore struct {
	mu    sync.RWMutex
	lines map[string]model.RoutingLine // key(小写) -> line
}

var GlobalLineStore = &LineStore{lines: make(map[string]model.RoutingLine)}

// LoadFromDB 从数据库加载全部启用的线路到内存（仅主节点持有 DB）。
func (ls *LineStore) LoadFromDB() error {
	if database.DB == nil {
		return nil
	}
	var lines []model.RoutingLine
	if err := database.DB.Where("enabled = ?", true).Find(&lines).Error; err != nil {
		return err
	}
	ls.ReplaceLines(lines)
	return nil
}

// ReplaceLines 用给定线路集合整体替换内存缓存（集群快照同步时使用）。
func (ls *LineStore) ReplaceLines(lines []model.RoutingLine) {
	idx := make(map[string]model.RoutingLine, len(lines))
	for _, l := range lines {
		if !l.Enabled {
			continue
		}
		idx[strings.ToLower(strings.TrimSpace(l.Key))] = l
	}
	ls.mu.Lock()
	ls.lines = idx
	ls.mu.Unlock()
}

// Get 按 key 返回线路定义。
func (ls *LineStore) Get(key string) (model.RoutingLine, bool) {
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	l, ok := ls.lines[strings.ToLower(strings.TrimSpace(key))]
	return l, ok
}

// isDefaultGeoLine 判断记录是否属于默认（兜底）线路。
func isDefaultGeoLine(geoLine string) bool {
	gl := strings.ToLower(strings.TrimSpace(geoLine))
	return gl == "" || gl == "default"
}

// lineMatchesClient 判断记录的 geo_line 是否匹配客户端定位。
// geo_line 可为线路 key、国家码、大洲码，或逗号分隔的多值；空或 default 恒匹配。
// 任一分段命中即视为匹配。
func lineMatchesClient(geoLine string, loc geo.LocationInfo) bool {
	gl := strings.TrimSpace(geoLine)
	if gl == "" || strings.EqualFold(gl, "default") {
		return true
	}
	for _, token := range strings.Split(gl, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if line, ok := GlobalLineStore.Get(token); ok {
			// 线路 key：按线路的多维度条件（大洲/国家/ASN）匹配
			if strings.EqualFold(line.Key, "default") {
				return true
			}
			if geo.MatchLineCSV(loc, line.Continents, line.Countries, line.ASNs) {
				return true
			}
			continue
		}
		// 向后兼容：直接书写的国家码 / 大洲码
		if geo.MatchGeoToken(token, loc) {
			return true
		}
	}
	return false
}

// MatchRecordLine 是 lineMatchesClient 的导出版本，供 API 层（路由检测器）复用同一套线路匹配逻辑，
// 确保检测结果与线上真实解析一致。
func MatchRecordLine(geoLine string, loc geo.LocationInfo) bool {
	return lineMatchesClient(geoLine, loc)
}
