package api

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dnscat/internal/cluster"
	"dnscat/internal/database"
	"dnscat/internal/dnsengine"
	"dnscat/internal/model"

	"github.com/gin-gonic/gin"
	"github.com/miekg/dns"
	"gorm.io/gorm"
)

type RecordHandler struct{}

func NewRecordHandler() *RecordHandler {
	return &RecordHandler{}
}

type RecordReq struct {
	Name     string           `json:"name" binding:"required"`
	Type     model.RecordType `json:"type" binding:"required"`
	Value    string           `json:"value" binding:"required"`
	TTL      uint32           `json:"ttl"`
	Priority uint16           `json:"priority"`
	Weight   uint16           `json:"weight"`
	Port     uint16           `json:"port"`
	GeoLine  string           `json:"geo_line"`
	Enabled  *bool            `json:"enabled"`
	Comment  string           `json:"comment"`
}

type BatchRecordsReq struct {
	DeleteIDs []uint      `json:"delete_ids"`
	// EnableIDs / DisableIDs 支持在解析记录列表里批量启停选中的记录，
	// 与删除、新增共用同一个事务，避免多次往返造成中间态。
	EnableIDs  []uint      `json:"enable_ids"`
	DisableIDs []uint      `json:"disable_ids"`
	Create     []RecordReq `json:"create"`
}

func (h *RecordHandler) ListRecords(c *gin.Context) {
	domainID, _ := strconv.Atoi(c.Param("id"))
	recordType := c.Query("type")
	search := strings.TrimSpace(c.Query("search"))
	geoLine := c.Query("geo_line")

	query := database.DB.Model(&model.Record{}).Where("domain_id = ?", domainID)
	// 隐藏 apex 权威 NS 记录：它们由系统按权威 NS 配置自动维护，用户是在域名
	// 注册商处做 NS 委派指向本系统，不应在解析记录列表里被编辑或删除。
	// DNS 引擎从 zone 快照读取这些记录用于应答，过滤只作用于此展示接口。
	query = query.Where("NOT (type = ? AND (name = ? OR name = ?))", model.RecordTypeNS, "@", "")
	if recordType != "" {
		query = query.Where("type = ?", recordType)
	}
	if search != "" {
		query = query.Where("name LIKE ? OR value LIKE ? OR comment LIKE ?", "%"+search+"%", "%"+search+"%", "%"+search+"%")
	}
	if geoLine != "" {
		query = query.Where("geo_line = ?", geoLine)
	}

	var records []model.Record
	if err := query.Order("name asc, type asc, id desc").Find(&records).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch records"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"records": records, "total": len(records)})
}

func (h *RecordHandler) CreateRecord(c *gin.Context) {
	domainID, _ := strconv.Atoi(c.Param("id"))
	var req RecordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid record parameters"})
		return
	}

	var domain model.Domain
	if err := database.DB.First(&domain, domainID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Domain not found"})
		return
	}

	// Validate & normalize
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "@"
	}
	val := strings.TrimSpace(req.Value)
	ttl := req.TTL
	if ttl == 0 {
		ttl = 300
	}
	weight := req.Weight
	if weight == 0 {
		weight = 100
	}
	geoLine := strings.ToLower(strings.TrimSpace(req.GeoLine))
	if geoLine == "" {
		geoLine = "default"
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	if err := validateRecordValue(req.Type, val); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	rec := model.Record{
		DomainID:  domain.ID,
		Name:      name,
		Type:      req.Type,
		Value:     val,
		TTL:       ttl,
		Priority:  req.Priority,
		Weight:    weight,
		Port:      req.Port,
		GeoLine:   geoLine,
		Enabled:   enabled,
		Comment:   req.Comment,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := database.DB.Create(&rec).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create record"})
		return
	}

	// Invalidate zone in memory & broadcast
	dnsengine.GlobalZoneStore.InvalidateZone(domain.ID)
	if cluster.GlobalCluster != nil {
		cluster.GlobalCluster.BroadcastZoneChange(domain.ID)
	}

	c.JSON(http.StatusCreated, rec)
}

func (h *RecordHandler) UpdateRecord(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var rec model.Record
	if err := database.DB.First(&rec, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
		return
	}

	var req RecordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid update parameters"})
		return
	}

	if err := validateRecordValue(req.Type, req.Value); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "@"
	}
	ttl := req.TTL
	if ttl == 0 {
		ttl = 300
	}
	weight := req.Weight
	if weight == 0 {
		weight = 100
	}
	geoLine := strings.ToLower(strings.TrimSpace(req.GeoLine))
	if geoLine == "" {
		geoLine = "default"
	}

	updates := map[string]interface{}{
		"name":       name,
		"type":       req.Type,
		"value":      strings.TrimSpace(req.Value),
		"ttl":        ttl,
		"priority":   req.Priority,
		"weight":     weight,
		"port":       req.Port,
		"geo_line":   geoLine,
		"comment":    req.Comment,
		"updated_at": time.Now(),
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	database.DB.Model(&rec).Updates(updates)

	dnsengine.GlobalZoneStore.InvalidateZone(rec.DomainID)
	if cluster.GlobalCluster != nil {
		cluster.GlobalCluster.BroadcastZoneChange(rec.DomainID)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Record updated successfully"})
}

func (h *RecordHandler) ToggleRecord(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var rec model.Record
	if err := database.DB.First(&rec, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
		return
	}

	newState := !rec.Enabled
	database.DB.Model(&rec).Update("enabled", newState)

	dnsengine.GlobalZoneStore.InvalidateZone(rec.DomainID)
	if cluster.GlobalCluster != nil {
		cluster.GlobalCluster.BroadcastZoneChange(rec.DomainID)
	}

	c.JSON(http.StatusOK, gin.H{"enabled": newState})
}

func (h *RecordHandler) DeleteRecord(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var rec model.Record
	if err := database.DB.First(&rec, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
		return
	}

	domainID := rec.DomainID
	database.DB.Delete(&rec)

	dnsengine.GlobalZoneStore.InvalidateZone(domainID)
	if cluster.GlobalCluster != nil {
		cluster.GlobalCluster.BroadcastZoneChange(domainID)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Record deleted successfully"})
}

func (h *RecordHandler) BatchOperate(c *gin.Context) {
	domainID, _ := strconv.Atoi(c.Param("id"))
	var req BatchRecordsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid batch payload"})
		return
	}

	var domain model.Domain
	if err := database.DB.First(&domain, domainID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Domain not found"})
		return
	}

	// Validate the entire payload before changing anything. A bad item must not
	// leave a partially applied DNS zone behind.
	for i, item := range req.Create {
		if err := validateRecordValue(item.Type, item.Value); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid record at index %d: %v", i, err)})
			return
		}
	}

	var toInsert []model.Record
	if len(req.Create) > 0 {
		for _, item := range req.Create {
			name := strings.TrimSpace(item.Name)
			if name == "" {
				name = "@"
			}
			ttl := item.TTL
			if ttl == 0 {
				ttl = 300
			}
			weight := item.Weight
			if weight == 0 {
				weight = 100
			}
			geoLine := strings.ToLower(strings.TrimSpace(item.GeoLine))
			if geoLine == "" {
				geoLine = "default"
			}
			enabled := true
			if item.Enabled != nil {
				enabled = *item.Enabled
			}
			toInsert = append(toInsert, model.Record{
				DomainID:  uint(domainID),
				Name:      name,
				Type:      item.Type,
				Value:     strings.TrimSpace(item.Value),
				TTL:       ttl,
				Priority:  item.Priority,
				Weight:    weight,
				Port:      item.Port,
				GeoLine:   geoLine,
				Enabled:   enabled,
				Comment:   item.Comment,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			})
		}
	}

	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if len(req.DeleteIDs) > 0 {
			if err := tx.Where("domain_id = ? AND id IN ?", domainID, req.DeleteIDs).Delete(&model.Record{}).Error; err != nil {
				return err
			}
		}
		// 批量启停：同样限定 domain_id，避免请求体里夹带其它域名的记录 ID 被误改。
		if len(req.EnableIDs) > 0 {
			if err := tx.Model(&model.Record{}).
				Where("domain_id = ? AND id IN ?", domainID, req.EnableIDs).
				Updates(map[string]interface{}{"enabled": true, "updated_at": time.Now()}).Error; err != nil {
				return err
			}
		}
		if len(req.DisableIDs) > 0 {
			if err := tx.Model(&model.Record{}).
				Where("domain_id = ? AND id IN ?", domainID, req.DisableIDs).
				Updates(map[string]interface{}{"enabled": false, "updated_at": time.Now()}).Error; err != nil {
				return err
			}
		}
		if len(toInsert) > 0 {
			if err := tx.Create(&toInsert).Error; err != nil {
				return err
			}
		}
		return tx.Model(&model.Domain{}).Where("id = ?", domainID).Updates(map[string]interface{}{
			"soa_serial": gorm.Expr("soa_serial + 1"),
			"updated_at": time.Now(),
		}).Error
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Batch operation failed"})
		return
	}

	dnsengine.GlobalZoneStore.InvalidateZone(uint(domainID))
	if cluster.GlobalCluster != nil {
		cluster.GlobalCluster.BroadcastZoneChange(uint(domainID))
	}

	c.JSON(http.StatusOK, gin.H{"message": "Batch operation completed"})
}

func validateRecordValue(rt model.RecordType, val string) error {
	val = strings.TrimSpace(val)
	if val == "" {
		return fmt.Errorf("record value cannot be empty")
	}

	switch rt {
	case model.RecordTypeA:
		ip := net.ParseIP(val)
		if ip == nil || ip.To4() == nil {
			return fmt.Errorf("invalid IPv4 address '%s' for A record", val)
		}
	case model.RecordTypeAAAA:
		ip := net.ParseIP(val)
		if ip == nil || ip.To16() == nil || ip.To4() != nil {
			return fmt.Errorf("invalid IPv6 address '%s' for AAAA record", val)
		}
	case model.RecordTypeCNAME, model.RecordTypeMX, model.RecordTypeNS, model.RecordTypePTR, model.RecordTypeALIAS:
		if _, ok := dns.IsDomainName(val); !ok {
			return fmt.Errorf("invalid domain name '%s' for %s record", val, rt)
		}
	case model.RecordTypeSRV:
		if _, ok := dns.IsDomainName(val); !ok {
			return fmt.Errorf("invalid SRV target '%s'", val)
		}
	case model.RecordTypeTXT:
		return nil
	case model.RecordTypeCAA, model.RecordTypeSOA, model.RecordTypeHTTPS, model.RecordTypeSVCB,
		model.RecordTypeTLSA, model.RecordTypeSSHFP, model.RecordTypeDS, model.RecordTypeDNSKEY:
		_, err := dnsengine.ConvertRecordToRR("validation.invalid", &model.Record{
			Name: "@", Type: rt, Value: val, TTL: 300, Priority: 10, Weight: 10, Port: 443,
		})
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported DNS record type '%s'", rt)
	}
	return nil
}
