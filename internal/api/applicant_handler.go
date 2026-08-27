package api

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dnscat/internal/acme"
	"dnscat/internal/database"
	"dnscat/internal/model"

	"github.com/gin-gonic/gin"
)

type ApplicantHandler struct{}

func NewApplicantHandler() *ApplicantHandler {
	return &ApplicantHandler{}
}

// triggerAccountRegistration 在后台为申请人注册（或复用）ACME 账户。
// 注册要与 CA 网络往返、耗时以秒计，放协程执行以免阻塞创建/更新的 HTTP 响应；
// 结果由 acme 层写回申请人记录，前端轮询申请人列表即可看到状态收敛。
func triggerAccountRegistration(app model.CertApplicant) {
	if acme.GlobalIssuer == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := acme.GlobalIssuer.RegisterApplicantAccount(ctx, &app); err != nil {
			log.Printf("[ACME] 申请人 #%d 自动注册 ACME 账户失败: %v", app.ID, err)
		}
	}()
}

type CreateApplicantReq struct {
	Name         string `json:"name" binding:"required"`
	Email        string `json:"email" binding:"required"`
	Organization string `json:"organization"`
	Provider     string `json:"provider"`
	EABKID       string `json:"eab_kid"`
	EABHMACKey   string `json:"eab_hmac_key"`
	IsDefault    bool   `json:"is_default"`
}

type UpdateApplicantReq struct {
	Name         string `json:"name"`
	Email        string `json:"email"`
	Organization string `json:"organization"`
	Provider     string `json:"provider"`
	EABKID       string `json:"eab_kid"`
	EABHMACKey   string `json:"eab_hmac_key"`
	IsDefault    bool   `json:"is_default"`
}

func (h *ApplicantHandler) ListApplicants(c *gin.Context) {
	var applicants []model.CertApplicant
	if err := database.DB.Order("is_default desc, id asc").Find(&applicants).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch applicant profiles"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"applicants": applicants})
}

func (h *ApplicantHandler) CreateApplicant(c *gin.Context) {
	userID := c.GetUint("user_id")
	var req CreateApplicantReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid applicant parameters"})
		return
	}

	name := strings.TrimSpace(req.Name)
	email := strings.TrimSpace(req.Email)
	if name == "" || email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name and Email are required"})
		return
	}

	provider := req.Provider
	if provider == "" {
		provider = acme.DefaultProviderName
	}

	eabKID := strings.TrimSpace(req.EABKID)
	eabHMAC := strings.TrimSpace(req.EABHMACKey)

	// ZeroSSL 与 Google Trust Services 强制要求 EAB，缺失时直接拒绝创建，
	// 避免用户建出一个注定无法签发的申请人。
	if p := acme.LookupProvider(provider); p.RequiresEAB && (eabKID == "" || eabHMAC == "") {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": p.Name + " 要求 External Account Binding，请填写 EAB Key ID 与 EAB HMAC Key",
		})
		return
	}

	if req.IsDefault {
		// Reset other defaults
		database.DB.Model(&model.CertApplicant{}).Where("user_id = ?", userID).Update("is_default", false)
	}

	app := model.CertApplicant{
		UserID:           userID,
		Name:             name,
		Email:            email,
		Organization:     strings.TrimSpace(req.Organization),
		Provider:         provider,
		EABKID:           eabKID,
		EABHMACKey:       eabHMAC,
		IsDefault:        req.IsDefault,
		ACMEAccountState: model.ACMEAccountUnregistered,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	if err := database.DB.Create(&app).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create applicant profile"})
		return
	}

	// 创建后自动注册 ACME 账户，让「添加申请人」即完成账户可用性配置，
	// 而不是等到首次签发才注册（那样列表会一直显示「未注册」）。
	triggerAccountRegistration(app)

	c.JSON(http.StatusCreated, app)
}

func (h *ApplicantHandler) UpdateApplicant(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	userID := c.GetUint("user_id")

	var app model.CertApplicant
	if err := database.DB.First(&app, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Applicant profile not found"})
		return
	}

	var req UpdateApplicantReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid update payload"})
		return
	}

	eabKID := strings.TrimSpace(req.EABKID)
	eabHMAC := strings.TrimSpace(req.EABHMACKey)

	targetProvider := app.Provider
	if req.Provider != "" {
		targetProvider = req.Provider
	}
	if p := acme.LookupProvider(targetProvider); p.RequiresEAB && (eabKID == "" || eabHMAC == "") {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": p.Name + " 要求 External Account Binding，请填写 EAB Key ID 与 EAB HMAC Key",
		})
		return
	}

	if req.IsDefault {
		database.DB.Model(&model.CertApplicant{}).Where("user_id = ?", userID).Update("is_default", false)
	}

	updates := map[string]interface{}{
		"updated_at":   time.Now(),
		"is_default":   req.IsDefault,
		"eab_kid":      eabKID,
		"eab_hmac_key": eabHMAC,
	}

	if req.Name != "" {
		updates["name"] = strings.TrimSpace(req.Name)
	}
	if req.Email != "" {
		updates["email"] = strings.TrimSpace(req.Email)
	}
	if req.Organization != "" {
		updates["organization"] = strings.TrimSpace(req.Organization)
	}
	if req.Provider != "" {
		updates["provider"] = req.Provider
	}

	// 服务商或 EAB 凭证一旦变化，原有 ACME 账户即不可复用：
	// 账户与 Directory 绑定，且 EAB 决定账户归属。
	// 这里清空账户状态，下次签发时会自动重新注册。
	if targetProvider != app.Provider || eabKID != app.EABKID || eabHMAC != app.EABHMACKey {
		updates["acme_account_uri"] = ""
		updates["acme_directory_url"] = ""
		updates["acme_account_state"] = model.ACMEAccountUnregistered
		updates["acme_account_error"] = ""
		updates["acme_registered_at"] = nil
	}

	if err := database.DB.Model(&app).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update applicant profile"})
		return
	}

	database.DB.First(&app, app.ID)
	c.JSON(http.StatusOK, app)
}

func (h *ApplicantHandler) DeleteApplicant(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var app model.CertApplicant
	if err := database.DB.First(&app, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Applicant profile not found"})
		return
	}

	if err := database.DB.Delete(&app).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete applicant profile"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Applicant profile deleted successfully"})
}
