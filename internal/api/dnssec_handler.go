package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"dnscat/internal/cluster"
	"dnscat/internal/database"
	"dnscat/internal/dnsengine"
	"dnscat/internal/model"

	"github.com/gin-gonic/gin"
)

type DNSSECHandler struct{}

func NewDNSSECHandler() *DNSSECHandler {
	return &DNSSECHandler{}
}

func (h *DNSSECHandler) GetDNSSEC(c *gin.Context) {
	domainID, _ := strconv.Atoi(c.Param("id"))
	var domain model.Domain
	if err := database.DB.Preload("DNSSECKeys").First(&domain, domainID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Domain not found"})
		return
	}

	var ksk *model.DNSSECKey
	var zsk *model.DNSSECKey
	for i := range domain.DNSSECKeys {
		k := &domain.DNSSECKeys[i]
		if k.KeyType == model.KeyTypeKSK {
			ksk = k
		} else if k.KeyType == model.KeyTypeZSK {
			zsk = k
		}
	}

	guide := gin.H{
		"key_tag":     "",
		"algorithm":   13,
		"digest_type": 2,
		"digest":      "",
		"public_key":  "",
		"ds_record":   "",
	}
	if ksk != nil {
		guide["key_tag"] = fmt.Sprintf("%d", ksk.KeyTag)
		guide["digest"] = ksk.Digest
		guide["public_key"] = ksk.PublicKey
		guide["ds_record"] = fmt.Sprintf("%s. IN DS %s", domain.Name, ksk.DSConfig)
	}

	c.JSON(http.StatusOK, gin.H{
		"enabled":          domain.DNSSECEnabled,
		"domain":           domain.Name,
		"ksk":              ksk,
		"zsk":              zsk,
		"algorithm":        "13 (ECDSA Curve P-256 with SHA-256)",
		"digest_type":      "2 (SHA-256)",
		"registrars_guide": guide,
	})
}

func (h *DNSSECHandler) EnableDNSSEC(c *gin.Context) {
	domainID, _ := strconv.Atoi(c.Param("id"))
	var domain model.Domain
	if err := database.DB.Preload("DNSSECKeys").First(&domain, domainID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Domain not found"})
		return
	}

	// Generate keys if not existing
	if len(domain.DNSSECKeys) == 0 {
		keys, err := dnsengine.GenerateDNSSECKeyPair(domain.ID, domain.Name)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate DNSSEC keys: " + err.Error()})
			return
		}
		if err := database.DB.Create(&keys).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save DNSSEC keys"})
			return
		}
	}

	if err := database.DB.Model(&domain).
		Select("DNSSECEnabled", "UpdatedAt").
		Updates(&model.Domain{DNSSECEnabled: true, UpdatedAt: time.Now()}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to enable DNSSEC"})
		return
	}

	dnsengine.GlobalZoneStore.InvalidateZone(domain.ID)
	if cluster.GlobalCluster != nil {
		cluster.GlobalCluster.BroadcastZoneChange(domain.ID)
	}

	c.JSON(http.StatusOK, gin.H{"message": "DNSSEC enabled successfully"})
}

func (h *DNSSECHandler) DisableDNSSEC(c *gin.Context) {
	domainID, _ := strconv.Atoi(c.Param("id"))
	var domain model.Domain
	if err := database.DB.First(&domain, domainID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Domain not found"})
		return
	}

	if err := database.DB.Model(&domain).
		Select("DNSSECEnabled", "UpdatedAt").
		Updates(&model.Domain{DNSSECEnabled: false, UpdatedAt: time.Now()}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to disable DNSSEC"})
		return
	}

	// 关闭时删除已生成的 KSK/ZSK 密钥，确保重新开启时会生成全新密钥对，避免残留旧密钥。
	database.DB.Where("domain_id = ?", domain.ID).Delete(&model.DNSSECKey{})

	dnsengine.GlobalZoneStore.InvalidateZone(domain.ID)
	if cluster.GlobalCluster != nil {
		cluster.GlobalCluster.BroadcastZoneChange(domain.ID)
	}

	c.JSON(http.StatusOK, gin.H{"message": "DNSSEC disabled successfully"})
}

func (h *DNSSECHandler) RotateKeys(c *gin.Context) {
	domainID, _ := strconv.Atoi(c.Param("id"))
	var domain model.Domain
	if err := database.DB.First(&domain, domainID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Domain not found"})
		return
	}

	// Delete old keys
	database.DB.Where("domain_id = ?", domain.ID).Delete(&model.DNSSECKey{})

	// Generate new keys
	keys, err := dnsengine.GenerateDNSSECKeyPair(domain.ID, domain.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to rotate DNSSEC keys: " + err.Error()})
		return
	}
	database.DB.Create(&keys)

	dnsengine.GlobalZoneStore.InvalidateZone(domain.ID)
	if cluster.GlobalCluster != nil {
		cluster.GlobalCluster.BroadcastZoneChange(domain.ID)
	}

	c.JSON(http.StatusOK, gin.H{"message": "DNSSEC keys rotated successfully", "keys": keys})
}
