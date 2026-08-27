package geo

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/oschwald/geoip2-golang"
)

type LineType string

const (
	LineDefault LineType = "default"
	LineCN      LineType = "cn"
	LineUS      LineType = "us"
	LineHK      LineType = "hk"
	LineJP      LineType = "jp"
	LineSG      LineType = "sg"
	LineDE      LineType = "de"
	LineGB      LineType = "gb"
	LineKR      LineType = "kr"
	LineAU      LineType = "au"
	LineCA      LineType = "ca"
	LineFR      LineType = "fr"
	LineRU      LineType = "ru"
	LineBR      LineType = "br"
	LineEU      LineType = "eu"
)

type LocationInfo struct {
	IP          string   `json:"ip"`
	CountryCode string   `json:"country_code"`
	CountryName string   `json:"country_name"`
	Continent   string   `json:"continent"` // 大洲码：AS/EU/NA/SA/AF/OC/AN，未知为空
	City        string   `json:"city,omitempty"`
	ISP         string   `json:"isp"`
	ASN         uint     `json:"asn"` // 自治域号，0 表示未知
	Line        LineType `json:"line"`
	IsPrivate   bool     `json:"is_private"`
	Source      string   `json:"source"` // "mmdb" or "embedded_db"
}

type IPRange struct {
	CIDR        *net.IPNet
	CountryCode string
	Line        LineType
	ISP         string
	ASN         uint
}

var (
	mmdbReader   *geoip2.Reader
	asnReader    *geoip2.Reader
	mmdbMu       sync.RWMutex
	mmdbLoaded   bool
	asnLoaded    bool
	cachedRanges []IPRange
)

func init() {
	initEmbeddedDatabase()
	// Auto-probe common local MaxMind MMDB paths (Country / City)
	probePaths := []string{
		"data/GeoLite2-Country.mmdb",
		"data/GeoLite2-City.mmdb",
		"GeoLite2-Country.mmdb",
		"GeoLite2-City.mmdb",
		"/etc/dnscat/GeoLite2-Country.mmdb",
	}
	for _, p := range probePaths {
		if _, err := os.Stat(p); err == nil {
			_ = LoadMMDB(p)
			break
		}
	}
	// Auto-probe MaxMind GeoLite2-ASN database for precise ASN resolution
	asnProbePaths := []string{
		"data/GeoLite2-ASN.mmdb",
		"GeoLite2-ASN.mmdb",
		"/etc/dnscat/GeoLite2-ASN.mmdb",
	}
	for _, p := range asnProbePaths {
		if _, err := os.Stat(p); err == nil {
			_ = LoadASNMMDB(p)
			break
		}
	}
}

// LoadASNMMDB initializes the optional MaxMind GeoLite2-ASN database for
// precise ASN lookups. When present, it takes precedence over the ASN values
// embedded in the built-in CIDR table.
func LoadASNMMDB(path string) error {
	mmdbMu.Lock()
	defer mmdbMu.Unlock()

	if asnReader != nil {
		_ = asnReader.Close()
		asnReader = nil
		asnLoaded = false
	}

	db, err := geoip2.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open ASN mmdb at %s: %w", path, err)
	}
	asnReader = db
	asnLoaded = true
	return nil
}

// LoadMMDB initializes the high-performance offline MaxMind database (.mmdb)
func LoadMMDB(path string) error {
	mmdbMu.Lock()
	defer mmdbMu.Unlock()

	if mmdbReader != nil {
		_ = mmdbReader.Close()
		mmdbReader = nil
		mmdbLoaded = false
	}

	db, err := geoip2.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open mmdb at %s: %w", path, err)
	}

	mmdbReader = db
	mmdbLoaded = true
	return nil
}

// CloseMMDB closes the active MMDB reader if open
func CloseMMDB() {
	mmdbMu.Lock()
	defer mmdbMu.Unlock()
	if mmdbReader != nil {
		_ = mmdbReader.Close()
		mmdbReader = nil
		mmdbLoaded = false
	}
}

// MatchLocation performs an offline lookup of the IP address and returns complete geographical & ISP data
func MatchLocation(ip net.IP) LocationInfo {
	if ip == nil {
		return LocationInfo{
			IP:          "",
			CountryCode: "default",
			CountryName: "Global Default",
			ISP:         "Unknown",
			Line:        LineDefault,
			IsPrivate:   false,
			Source:      "none",
		}
	}

	ipStr := ip.String()
	if ip.IsLoopback() || ip.IsPrivate() {
		return LocationInfo{
			IP:          ipStr,
			CountryCode: "cn",
			CountryName: "Local / Private Network",
			Continent:   "AS",
			ISP:         "Private LAN",
			Line:        LineDefault,
			IsPrivate:   true,
			Source:      "internal",
		}
	}

	// Optional precise ASN lookup (independent from the country data source).
	asn := lookupASN(ip)

	// 1. Try MaxMind MMDB Offline Database (if loaded)
	mmdbMu.RLock()
	if mmdbLoaded && mmdbReader != nil {
		record, err := mmdbReader.Country(ip)
		mmdbMu.RUnlock()
		if err == nil && record != nil && record.Country.IsoCode != "" {
			cc := strings.ToLower(record.Country.IsoCode)
			cname := record.Country.Names["en"]
			if zhName, ok := record.Country.Names["zh-CN"]; ok && zhName != "" {
				cname = zhName
			}
			continent := strings.ToUpper(record.Continent.Code)
			if continent == "" {
				continent = CountryToContinent(cc)
			}
			return LocationInfo{
				IP:          ipStr,
				CountryCode: cc,
				CountryName: cname,
				Continent:   continent,
				ISP:         "Regional Autonomous System",
				ASN:         asn,
				Line:        LineType(cc),
				IsPrivate:   false,
				Source:      "mmdb",
			}
		}
	} else {
		mmdbMu.RUnlock()
	}

	// 2. Fallback to Embedded Global IP Database
	for _, r := range cachedRanges {
		if r.CIDR.Contains(ip) {
			effASN := asn
			if effASN == 0 {
				effASN = r.ASN
			}
			return LocationInfo{
				IP:          ipStr,
				CountryCode: r.CountryCode,
				CountryName: strings.ToUpper(r.CountryCode),
				Continent:   CountryToContinent(r.CountryCode),
				ISP:         r.ISP,
				ASN:         effASN,
				Line:        r.Line,
				IsPrivate:   false,
				Source:      "embedded_db",
			}
		}
	}

	return LocationInfo{
		IP:          ipStr,
		CountryCode: "default",
		CountryName: "Global Default",
		ISP:         "Default Gateway",
		ASN:         asn,
		Line:        LineDefault,
		IsPrivate:   false,
		Source:      "fallback",
	}
}

// MatchCountry returns the 2-letter ISO 3166-1 alpha-2 lowercase country code (e.g. "cn", "us", "jp", "de")
func MatchCountry(ip net.IP) string {
	loc := MatchLocation(ip)
	return loc.CountryCode
}

// MatchLine determines which line best matches the given client/ECS IP
func MatchLine(clientIP net.IP) LineType {
	loc := MatchLocation(clientIP)
	return loc.Line
}

// IsLineMatch checks if a record's line (or comma-separated multi-lines) matches the client's detected line
func IsLineMatch(recordLine string, clientLine LineType) bool {
	rLine := strings.ToLower(strings.TrimSpace(recordLine))
	cLine := strings.ToLower(string(clientLine))

	if rLine == "" || rLine == "default" {
		return true
	}

	// Support multi-select (comma-separated values e.g. "us,ca,mx" or "jp,kr" or "eu")
	parts := strings.Split(rLine, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == cLine || p == "default" {
			return true
		}
		// EU Region grouping
		if isEuropeanCountry(cLine) && p == "eu" {
			return true
		}
	}

	return false
}

func isEuropeanCountry(code string) bool {
	euCodes := map[string]bool{
		"de": true, "fr": true, "gb": true, "it": true, "es": true, "nl": true,
		"se": true, "ch": true, "no": true, "fi": true, "dk": true, "be": true,
		"at": true, "pl": true, "ie": true, "pt": true, "gr": true, "cz": true,
		"ro": true, "hu": true, "bg": true, "sk": true, "hr": true, "lt": true,
		"si": true, "lv": true, "ee": true, "cy": true, "lu": true, "mt": true,
	}
	return euCodes[code]
}

func initEmbeddedDatabase() {
	cidrs := []struct {
		cidr        string
		countryCode string
		line        LineType
		isp         string
	}{
		// China Region
		{"14.0.0.0/8", "cn", LineCN, "China Network AS4134"},
		{"58.32.0.0/11", "cn", LineCN, "China Network AS4134"},
		{"59.32.0.0/11", "cn", LineCN, "China Network AS4134"},
		{"61.128.0.0/10", "cn", LineCN, "China Network AS4134"},
		{"101.80.0.0/12", "cn", LineCN, "China Network AS4134"},
		{"114.80.0.0/12", "cn", LineCN, "China Network AS4134"},
		{"114.114.114.0/24", "cn", LineCN, "China Network AS4134"},
		{"116.224.0.0/12", "cn", LineCN, "China Network AS4134"},
		{"121.8.0.0/13", "cn", LineCN, "China Network AS4134"},
		{"180.96.0.0/11", "cn", LineCN, "China Network AS4134"},
		{"183.0.0.0/10", "cn", LineCN, "China Network AS4134"},
		{"218.1.0.0/16", "cn", LineCN, "China Network AS4134"},
		{"222.64.0.0/11", "cn", LineCN, "China Network AS4134"},
		{"42.48.0.0/12", "cn", LineCN, "China Network AS4837"},
		{"58.16.0.0/12", "cn", LineCN, "China Network AS4837"},
		{"60.0.0.0/11", "cn", LineCN, "China Network AS4837"},
		{"112.96.0.0/12", "cn", LineCN, "China Network AS4837"},
		{"113.12.0.0/14", "cn", LineCN, "China Network AS4837"},
		{"123.112.0.0/12", "cn", LineCN, "China Network AS4837"},
		{"124.64.0.0/13", "cn", LineCN, "China Network AS4837"},
		{"210.12.0.0/14", "cn", LineCN, "China Network AS4837"},
		{"221.192.0.0/11", "cn", LineCN, "China Network AS4837"},
		{"223.5.5.0/24", "cn", LineCN, "China Network AS4837"},
		{"36.128.0.0/10", "cn", LineCN, "China Network AS9808"},
		{"39.128.0.0/10", "cn", LineCN, "China Network AS9808"},
		{"111.0.0.0/10", "cn", LineCN, "China Network AS9808"},
		{"117.128.0.0/10", "cn", LineCN, "China Network AS9808"},
		{"120.192.0.0/10", "cn", LineCN, "China Network AS9808"},
		{"183.192.0.0/10", "cn", LineCN, "China Network AS9808"},
		{"221.130.0.0/15", "cn", LineCN, "China Network AS9808"},
		{"223.64.0.0/10", "cn", LineCN, "China Network AS9808"},
		{"202.112.0.0/14", "cn", LineCN, "CERNET Academic AS4538"},
		{"202.116.0.0/14", "cn", LineCN, "CERNET Academic AS4538"},
		{"202.120.0.0/14", "cn", LineCN, "CERNET Academic AS4538"},
		{"166.111.0.0/16", "cn", LineCN, "CERNET Academic AS4538"},

		// North America (US, CA, MX)
		{"3.0.0.0/8", "us", LineUS, "AWS US"},
		{"4.0.0.0/8", "us", LineUS, "Level3 / Lumen US"},
		{"8.8.8.0/24", "us", LineUS, "Google DNS US"},
		{"8.8.4.0/24", "us", LineUS, "Google DNS US"},
		{"1.1.1.0/24", "us", LineUS, "Cloudflare US"},
		{"12.0.0.0/8", "us", LineUS, "AT&T US"},
		{"13.0.0.0/8", "us", LineUS, "Microsoft Azure US"},
		{"23.0.0.0/8", "us", LineUS, "Akamai US"},
		{"34.0.0.0/8", "us", LineUS, "Google Cloud US"},
		{"52.0.0.0/8", "us", LineUS, "AWS Cloud US"},
		{"54.0.0.0/8", "us", LineUS, "AWS US"},
		{"64.0.0.0/10", "us", LineUS, "US Broadband"},
		{"65.0.0.0/8", "us", LineUS, "Microsoft US"},
		{"66.0.0.0/8", "us", LineUS, "US Carriers"},
		{"73.0.0.0/8", "us", LineUS, "Comcast US"},
		{"74.125.0.0/16", "us", LineUS, "Google LLC US"},
		{"98.0.0.0/8", "us", LineUS, "US Telecom"},
		{"104.16.0.0/12", "us", LineUS, "Cloudflare Edge US"},
		{"142.0.0.0/10", "ca", LineCA, "Canada Networks"},
		{"173.0.0.0/8", "us", LineUS, "US Networks"},
		{"187.128.0.0/10", "mx", LineType("mx"), "Telmex Mexico"},
		{"189.128.0.0/10", "mx", LineType("mx"), "Totalplay Mexico"},
		{"192.0.2.0/24", "us", LineUS, "Documentation US"},
		{"198.41.0.0/16", "us", LineUS, "Cloudflare Anycast"},
		{"204.0.0.0/8", "us", LineUS, "ARIN Allocation"},
		{"208.67.222.0/24", "us", LineUS, "Cisco OpenDNS US"},

		// East Asia (JP, KR, HK, TW)
		{"133.0.0.0/8", "jp", LineJP, "Japan Academic & ISP"},
		{"210.140.0.0/14", "jp", LineJP, "NTT Communications Japan"},
		{"118.238.0.0/15", "jp", LineJP, "SoftBank Japan"},
		{"121.80.0.0/13", "jp", LineJP, "KDDI Japan"},
		{"122.130.0.0/15", "jp", LineJP, "OCN Japan"},
		{"118.32.0.0/12", "kr", LineKR, "KT Korea"},
		{"121.128.0.0/11", "kr", LineKR, "SK Telecom Korea"},
		{"211.0.0.0/12", "kr", LineKR, "LG Uplus Korea"},
		{"203.80.0.0/14", "hk", LineHK, "HKT Hong Kong"},
		{"218.102.0.0/15", "hk", LineHK, "HKBN Hong Kong"},
		{"61.216.0.0/13", "tw", LineType("tw"), "Chunghwa Telecom Taiwan"},
		{"114.32.0.0/12", "tw", LineType("tw"), "HiNet Taiwan"},

		// Southeast Asia & Oceania (SG, MY, ID, VN, TH, PH, AU, NZ)
		{"165.225.0.0/16", "sg", LineSG, "Singtel Singapore"},
		{"203.116.0.0/14", "sg", LineSG, "StarHub Singapore"},
		{"139.130.0.0/16", "au", LineAU, "Telstra Australia"},
		{"144.130.0.0/15", "au", LineAU, "Optus Australia"},
		{"203.109.128.0/17", "nz", LineType("nz"), "Spark New Zealand"},
		{"175.136.0.0/13", "my", LineType("my"), "TM Net Malaysia"},
		{"180.240.0.0/12", "id", LineType("id"), "Telkom Indonesia"},
		{"171.224.0.0/11", "vn", LineType("vn"), "VNPT Vietnam"},
		{"124.120.0.0/13", "th", LineType("th"), "True Internet Thailand"},
		{"112.198.0.0/15", "ph", LineType("ph"), "Globe Telecom Philippines"},

		// Europe (DE, GB, FR, IT, ES, NL, SE, CH, RU, UA, PL)
		{"193.0.0.0/16", "de", LineDE, "Deutsche Telekom Germany"},
		{"194.0.0.0/16", "gb", LineGB, "British Telecom UK"},
		{"195.0.0.0/16", "fr", LineFR, "Orange France"},
		{"151.0.0.0/12", "it", LineType("it"), "Telecom Italia"},
		{"80.24.0.0/13", "es", LineType("es"), "Telefonica Spain"},
		{"84.116.0.0/14", "nl", LineType("nl"), "Ziggo Netherlands"},
		{"83.250.0.0/15", "se", LineType("se"), "Telia Sweden"},
		{"85.0.0.0/11", "ch", LineType("ch"), "Swisscom Switzerland"},
		{"194.85.0.0/16", "ru", LineRU, "Rostelecom Russia"},
		{"178.64.0.0/11", "ru", LineRU, "MegaFon Russia"},
		{"176.104.0.0/13", "ua", LineType("ua"), "Kyivstar Ukraine"},
		{"83.0.0.0/11", "pl", LineType("pl"), "Orange Poland"},

		// South America (BR, AR, CL, CO, PE)
		{"200.0.0.0/16", "br", LineBR, "Claro Brazil"},
		{"177.0.0.0/11", "br", LineBR, "Vivo Brazil"},
		{"181.16.0.0/12", "ar", LineType("ar"), "Telecom Argentina"},
		{"190.160.0.0/13", "cl", LineType("cl"), "Entel Chile"},
		{"190.24.0.0/13", "co", LineType("co"), "Claro Colombia"},
		{"190.232.0.0/13", "pe", LineType("pe"), "Movistar Peru"},

		// South Asia & Middle East (IN, PK, BD, SA, AE, IL, TR)
		{"103.0.0.0/10", "in", LineType("in"), "Reliance Jio India"},
		{"106.192.0.0/10", "in", LineType("in"), "Bharti Airtel India"},
		{"182.176.0.0/12", "pk", LineType("pk"), "PTCL Pakistan"},
		{"103.220.0.0/14", "bd", LineType("bd"), "Grameenphone Bangladesh"},
		{"212.118.128.0/17", "sa", LineType("sa"), "STC Saudi Arabia"},
		{"94.200.0.0/14", "ae", LineType("ae"), "Etisalat UAE"},
		{"212.179.0.0/16", "il", LineType("il"), "Bezeq Israel"},
		{"88.224.0.0/11", "tr", LineType("tr"), "Turk Telekom Turkey"},

		// Africa (ZA, EG, NG, KE, DZ, MA)
		{"105.0.0.0/10", "za", LineType("za"), "Vodacom South Africa"},
		{"197.32.0.0/11", "eg", LineType("eg"), "Telecom Egypt"},
		{"105.112.0.0/12", "ng", LineType("ng"), "MTN Nigeria"},
		{"197.232.0.0/13", "ke", LineType("ke"), "Safaricom Kenya"},
		{"197.200.0.0/13", "dz", LineType("dz"), "Algerie Telecom"},
		{"196.200.0.0/13", "ma", LineType("ma"), "Maroc Telecom"},
	}

	for _, item := range cidrs {
		_, ipNet, err := net.ParseCIDR(item.cidr)
		if err == nil {
			cachedRanges = append(cachedRanges, IPRange{
				CIDR:        ipNet,
				CountryCode: item.countryCode,
				Line:        item.line,
				ISP:         item.isp,
				ASN:         extractASN(item.isp),
			})
		}
	}
}
