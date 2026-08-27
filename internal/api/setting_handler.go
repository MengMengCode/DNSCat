package api

import (
	"net/http"
	"time"

	"dnscat/internal/database"
	"dnscat/internal/model"

	"github.com/gin-gonic/gin"
)

type SettingHandler struct{}

func NewSettingHandler() *SettingHandler {
	return &SettingHandler{}
}

func (h *SettingHandler) GetSettings(c *gin.Context) {
	var settings []model.SystemSetting
	if err := database.DB.Find(&settings).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch settings"})
		return
	}

	settingMap := make(map[string]string)
	for _, s := range settings {
		settingMap[s.Key] = s.Value
	}

	c.JSON(http.StatusOK, gin.H{"settings": settingMap, "list": settings})
}

func (h *SettingHandler) UpdateSettings(c *gin.Context) {
	var req map[string]string
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid settings payload"})
		return
	}

	for k, v := range req {
		var s model.SystemSetting
		if err := database.DB.Where("`key` = ?", k).First(&s).Error; err == nil {
			database.DB.Model(&s).Updates(map[string]interface{}{
				"value":      v,
				"updated_at": time.Now(),
			})
		} else {
			database.DB.Create(&model.SystemSetting{
				Key:       k,
				Value:     v,
				UpdatedAt: time.Now(),
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Settings updated successfully"})
}
