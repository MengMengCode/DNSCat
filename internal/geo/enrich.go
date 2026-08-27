package geo

import (
	"net"
	"regexp"
	"strconv"
	"strings"
)

// 大洲码常量（与 MaxMind Continent.Code 及前端约定保持一致，统一大写）。
const (
	ContinentAsia         = "AS"
	ContinentEurope       = "EU"
	ContinentNorthAmerica = "NA"
	ContinentSouthAmerica = "SA"
	ContinentAfrica       = "AF"
	ContinentOceania      = "OC"
	ContinentAntarctica   = "AN"
)

// countryContinent 将 ISO 3166-1 alpha-2 国家码映射到大洲码。
// 覆盖主流国家与内置 CIDR 表中出现的全部国家；未收录的国家在离线场景返回空大洲。
var countryContinent = map[string]string{
	// Asia
	"cn": "AS", "hk": "AS", "mo": "AS", "tw": "AS", "jp": "AS", "kr": "AS", "kp": "AS",
	"mn": "AS", "sg": "AS", "my": "AS", "th": "AS", "vn": "AS", "id": "AS", "ph": "AS",
	"kh": "AS", "la": "AS", "mm": "AS", "bn": "AS", "tl": "AS", "in": "AS", "pk": "AS",
	"bd": "AS", "lk": "AS", "np": "AS", "bt": "AS", "mv": "AS", "kz": "AS", "uz": "AS",
	"tm": "AS", "kg": "AS", "tj": "AS", "af": "AS", "ae": "AS", "sa": "AS", "qa": "AS",
	"kw": "AS", "om": "AS", "bh": "AS", "il": "AS", "tr": "AS", "ir": "AS", "iq": "AS",
	"sy": "AS", "jo": "AS", "lb": "AS", "ye": "AS", "ge": "AS", "am": "AS", "az": "AS",
	// Europe
	"de": "EU", "fr": "EU", "gb": "EU", "it": "EU", "es": "EU", "nl": "EU", "se": "EU",
	"ch": "EU", "no": "EU", "fi": "EU", "dk": "EU", "be": "EU", "at": "EU", "pl": "EU",
	"ie": "EU", "pt": "EU", "gr": "EU", "cz": "EU", "ro": "EU", "hu": "EU", "bg": "EU",
	"sk": "EU", "hr": "EU", "lt": "EU", "si": "EU", "lv": "EU", "ee": "EU", "cy": "EU",
	"lu": "EU", "mt": "EU", "is": "EU", "ua": "EU", "ru": "EU", "by": "EU", "md": "EU",
	"rs": "EU", "ba": "EU", "mk": "EU", "al": "EU", "me": "EU",
	// North America
	"us": "NA", "ca": "NA", "mx": "NA", "gt": "NA", "cu": "NA", "do": "NA", "hn": "NA",
	"ni": "NA", "cr": "NA", "pa": "NA", "sv": "NA", "jm": "NA", "ht": "NA", "bs": "NA",
	"bz": "NA", "tt": "NA", "pr": "NA",
	// South America
	"br": "SA", "ar": "SA", "cl": "SA", "co": "SA", "pe": "SA", "ve": "SA", "ec": "SA",
	"bo": "SA", "py": "SA", "uy": "SA", "gy": "SA", "sr": "SA",
	// Africa
	"za": "AF", "eg": "AF", "ng": "AF", "ke": "AF", "dz": "AF", "ma": "AF", "tn": "AF",
	"et": "AF", "gh": "AF", "ci": "AF", "sn": "AF", "cm": "AF", "ug": "AF", "zm": "AF",
	"zw": "AF", "ao": "AF", "mz": "AF", "mg": "AF", "ly": "AF", "sd": "AF", "rw": "AF",
	// Oceania
	"au": "OC", "nz": "OC", "fj": "OC", "pg": "OC", "nc": "OC", "sb": "OC", "vu": "OC",
	"ws": "OC", "to": "OC", "gu": "OC",
}

// CountryToContinent 返回国家码对应的大洲码，未知时返回空字符串。
func CountryToContinent(countryCode string) string {
	return countryContinent[strings.ToLower(strings.TrimSpace(countryCode))]
}

var asnRegexp = regexp.MustCompile(`(?i)AS(\d+)`)

// extractASN 从 ISP 描述文本中提取自治域号（如 "China Network AS4134" -> 4134）。
func extractASN(isp string) uint {
	m := asnRegexp.FindStringSubmatch(isp)
	if len(m) == 2 {
		if n, err := strconv.ParseUint(m[1], 10, 32); err == nil {
			return uint(n)
		}
	}
	return 0
}

// lookupASN 在已加载 GeoLite2-ASN 库时返回精确 ASN，否则返回 0。
func lookupASN(ip net.IP) uint {
	mmdbMu.RLock()
	defer mmdbMu.RUnlock()
	if asnLoaded && asnReader != nil {
		if rec, err := asnReader.ASN(ip); err == nil && rec != nil {
			return rec.AutonomousSystemNumber
		}
	}
	return 0
}

// splitCSV 将逗号分隔字符串拆分为去空白、去空项的切片。
func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// MatchLineRules 判断客户端定位是否命中一条线路的多维度条件（大洲 / 国家 / ASN）。
// 条件之间为「或」关系：任一维度命中即视为匹配。
func MatchLineRules(loc LocationInfo, continents, countries, asns []string) bool {
	for _, c := range continents {
		if c != "" && strings.EqualFold(strings.TrimSpace(c), loc.Continent) {
			return true
		}
	}
	for _, c := range countries {
		if c != "" && strings.EqualFold(strings.TrimSpace(c), loc.CountryCode) {
			return true
		}
	}
	if loc.ASN != 0 {
		for _, a := range asns {
			a = strings.TrimSpace(a)
			a = strings.TrimPrefix(strings.ToUpper(a), "AS")
			if n, err := strconv.ParseUint(a, 10, 32); err == nil && uint(n) == loc.ASN {
				return true
			}
		}
	}
	return false
}

// MatchLineCSV 是 MatchLineRules 的便捷封装，接收逗号分隔的条件字符串。
func MatchLineCSV(loc LocationInfo, continentsCSV, countriesCSV, asnsCSV string) bool {
	return MatchLineRules(loc, splitCSV(continentsCSV), splitCSV(countriesCSV), splitCSV(asnsCSV))
}

// MatchGeoToken 判断单个 geo 令牌（国家码 / 大洲码 / default / eu）是否匹配客户端定位。
// 用于向后兼容：记录的 geo_line 中直接书写的国家码或大洲码。
func MatchGeoToken(token string, loc LocationInfo) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" || token == "default" {
		return true
	}
	if token == strings.ToLower(loc.CountryCode) {
		return true
	}
	if strings.EqualFold(token, loc.Continent) {
		return true
	}
	if token == "eu" && isEuropeanCountry(strings.ToLower(loc.CountryCode)) {
		return true
	}
	return false
}
