package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"dnscat/internal/database"
	"dnscat/internal/healthcheck"
	"dnscat/internal/model"

	"github.com/gin-gonic/gin"
)

type HealthHandler struct{}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

type HealthCheckReq struct {
	DomainID         uint                      `json:"domain_id" binding:"required"`
	RecordID         uint                      `json:"record_id" binding:"required"`
	Name             string                    `json:"name" binding:"required"`
	Protocol         model.HealthCheckProtocol `json:"protocol"`
	Host             string                    `json:"host"`
	Port             int                       `json:"port"`
	Path             string                    `json:"path"`
	ExpectedCode     int                       `json:"expected_code"`
	CheckIntervalSec int                       `json:"check_interval_sec"`
	TimeoutSec       int                       `json:"timeout_sec"`
	FallbackIP       string                    `json:"fallback_ip"`
}

// normalizeProtocol 把协议归一化为大写并校验白名单。
// 探测器一旦存了无法识别的协议就是个「僵尸探测器」：worker 现在是 fail-closed，
// 它会一直报故障并触发容灾切换，所以必须在入库前拦下。空值按 HTTP 处理。
func normalizeProtocol(raw model.HealthCheckProtocol) (model.HealthCheckProtocol, bool) {
	proto := model.HealthCheckProtocol(strings.ToUpper(strings.TrimSpace(string(raw))))
	if proto == "" {
		return model.ProtocolHTTP, true
	}
	switch proto {
	case model.ProtocolHTTP, model.ProtocolHTTPS, model.ProtocolTCP, model.ProtocolPING:
		return proto, true
	default:
		return "", false
	}
}

func (h *HealthHandler) ListHealthChecks(c *gin.Context) {
	domainIDStr := c.Query("domain_id")
	query := database.DB.Model(&model.HealthCheck{}).Preload("Record")
	if domainIDStr != "" {
		domainID, _ := strconv.Atoi(domainIDStr)
		query = query.Where("domain_id = ?", domainID)
	}

	var list []model.HealthCheck
	if err := query.Order("id desc").Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch health checks"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"health_checks": list, "total": len(list)})
}

func (h *HealthHandler) CreateHealthCheck(c *gin.Context) {
	var req HealthCheckReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid health check parameters"})
		return
	}

	var rec model.Record
	if err := database.DB.First(&rec, req.RecordID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Associated record not found"})
		return
	}

	host := req.Host
	if host == "" {
		host = rec.Value
	}
	interval := req.CheckIntervalSec
	if interval <= 0 {
		interval = 15
	}
	timeout := req.TimeoutSec
	if timeout <= 0 {
		timeout = 5
	}
	proto, ok := normalizeProtocol(req.Protocol)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Unsupported protocol: " + string(req.Protocol) + " (expected HTTP, HTTPS, TCP or PING)",
		})
		return
	}

	hc := model.HealthCheck{
		DomainID:         req.DomainID,
		RecordID:         req.RecordID,
		Name:             req.Name,
		Protocol:         proto,
		Host:             host,
		Port:             req.Port,
		Path:             req.Path,
		ExpectedCode:     req.ExpectedCode,
		CheckIntervalSec: interval,
		TimeoutSec:       timeout,
		FallbackIP:       req.FallbackIP,
		Status:           model.HealthStatusHealthy,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	if err := database.DB.Create(&hc).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create health check"})
		return
	}

	// Trigger initial immediate probe
	if healthcheck.GlobalChecker != nil {
		go healthcheck.GlobalChecker.ProbeNow(&hc)
	}

	c.JSON(http.StatusCreated, hc)
}

func (h *HealthHandler) UpdateHealthCheck(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var hc model.HealthCheck
	if err := database.DB.First(&hc, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Health check not found"})
		return
	}

	var req HealthCheckReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid update parameters"})
		return
	}

	// 更新路径必须和创建路径做同样的归一化与校验，否则可以先用合法协议建好、
	// 再 PUT 成小写或空值绕过白名单。
	proto, ok := normalizeProtocol(req.Protocol)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Unsupported protocol: " + string(req.Protocol) + " (expected HTTP, HTTPS, TCP or PING)",
		})
		return
	}

	host := req.Host
	if host == "" {
		host = hc.Host
	}
	interval := req.CheckIntervalSec
	if interval <= 0 {
		interval = hc.CheckIntervalSec
	}
	if interval <= 0 {
		interval = 15
	}
	timeout := req.TimeoutSec
	if timeout <= 0 {
		timeout = hc.TimeoutSec
	}
	if timeout <= 0 {
		timeout = 5
	}

	updates := map[string]interface{}{
		"name":               req.Name,
		"protocol":           proto,
		"host":               host,
		"port":               req.Port,
		"path":               req.Path,
		"expected_code":      req.ExpectedCode,
		"check_interval_sec": interval,
		"timeout_sec":        timeout,
		"fallback_ip":        req.FallbackIP,
		"updated_at":         time.Now(),
	}

	database.DB.Model(&hc).Updates(updates)
	c.JSON(http.StatusOK, gin.H{"message": "Health check updated"})
}

func (h *HealthHandler) DeleteHealthCheck(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	database.DB.Where("id = ?", id).Delete(&model.HealthCheck{})
	c.JSON(http.StatusOK, gin.H{"message": "Health check deleted"})
}

func (h *HealthHandler) ProbeNow(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var hc model.HealthCheck
	if err := database.DB.Preload("Record").First(&hc, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Health check not found"})
		return
	}

	if healthcheck.GlobalChecker == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Health checker worker not initialized"})
		return
	}

	success, latency, msg := healthcheck.GlobalChecker.ProbeNow(&hc)
	c.JSON(http.StatusOK, gin.H{
		"success":    success,
		"latency_ms": latency,
		"message":    msg,
	})
}
