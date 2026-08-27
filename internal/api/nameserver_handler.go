package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"dnscat/internal/database"
	"dnscat/internal/model"

	"github.com/gin-gonic/gin"
)

type NameserverHandler struct{}

func NewNameserverHandler() *NameserverHandler {
	return &NameserverHandler{}
}

type CreateNameserverReq struct {
	Hostname string `json:"hostname" binding:"required"`
	NodeID   *uint  `json:"node_id"`
	IPv4     string `json:"ipv4"`
	IPv6     string `json:"ipv6"`
	IsActive bool   `json:"is_active"`
}

type UpdateNameserverReq struct {
	Hostname string `json:"hostname"`
	NodeID   *uint  `json:"node_id"`
	IPv4     string `json:"ipv4"`
	IPv6     string `json:"ipv6"`
	IsActive bool   `json:"is_active"`
}

func (h *NameserverHandler) ListNameservers(c *gin.Context) {
	var nameservers []model.Nameserver
	if err := database.DB.Preload("Node").Order("id asc").Find(&nameservers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch nameservers"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"nameservers": nameservers})
}

func (h *NameserverHandler) CreateNameserver(c *gin.Context) {
	var req CreateNameserverReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid nameserver parameters"})
		return
	}

	hostname := strings.ToLower(strings.TrimSpace(req.Hostname))
	hostname = strings.TrimSuffix(hostname, ".")
	if hostname == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Hostname cannot be empty"})
		return
	}

	var existing model.Nameserver
	if err := database.DB.Where("hostname = ?", hostname).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Nameserver with this hostname already exists"})
		return
	}

	ns := model.Nameserver{
		Hostname:      hostname,
		ClusterNodeID: req.NodeID,
		IPv4:          strings.TrimSpace(req.IPv4),
		IPv6:          strings.TrimSpace(req.IPv6),
		IsActive:      req.IsActive,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := database.DB.Create(&ns).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create nameserver"})
		return
	}

	database.DB.Preload("Node").First(&ns, ns.ID)
	c.JSON(http.StatusCreated, ns)
}

func (h *NameserverHandler) UpdateNameserver(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var ns model.Nameserver
	if err := database.DB.First(&ns, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Nameserver not found"})
		return
	}

	var req UpdateNameserverReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid update payload"})
		return
	}

	updates := map[string]interface{}{
		"updated_at":      time.Now(),
		"is_active":       req.IsActive,
		"ipv4":            strings.TrimSpace(req.IPv4),
		"ipv6":            strings.TrimSpace(req.IPv6),
		"cluster_node_id": req.NodeID,
	}

	if req.Hostname != "" {
		hostname := strings.ToLower(strings.TrimSpace(req.Hostname))
		hostname = strings.TrimSuffix(hostname, ".")
		updates["hostname"] = hostname
	}

	if err := database.DB.Model(&ns).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update nameserver"})
		return
	}

	database.DB.Preload("Node").First(&ns, ns.ID)
	c.JSON(http.StatusOK, ns)
}

func (h *NameserverHandler) DeleteNameserver(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var ns model.Nameserver
	if err := database.DB.First(&ns, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Nameserver not found"})
		return
	}

	if err := database.DB.Delete(&ns).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete nameserver"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Nameserver deleted successfully"})
}
