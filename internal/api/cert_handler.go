package api

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"dnscat/internal/acme"
	"dnscat/internal/database"
	"dnscat/internal/model"

	"github.com/gin-gonic/gin"
)

type CertHandler struct{}

func NewCertHandler() *CertHandler {
	return &CertHandler{}
}

// resolveCertificate 按对外 UUID 定位证书，并兼容历史的纯数字 id 直链。
// preloadApplicant 为 true 时预加载申请人（续期需要）。
func resolveCertificate(idParam string, preloadApplicant bool) (*model.Certificate, error) {
	base := database.DB
	if preloadApplicant {
		base = base.Preload("Applicant")
	}
	var cert model.Certificate
	// 优先按 UUID 精确匹配。
	if err := base.Where("uuid = ?", idParam).First(&cert).Error; err == nil {
		return &cert, nil
	}
	// 兼容旧的自增 id 直链（例如历史书签 /certificates/1/download）。
	if n, convErr := strconv.Atoi(idParam); convErr == nil {
		if err := base.First(&cert, n).Error; err == nil {
			return &cert, nil
		}
	}
	return nil, fmt.Errorf("certificate not found")
}

type IssueCertReq struct {
	DomainID    uint     `json:"domain_id" binding:"required"`
	ApplicantID *uint    `json:"applicant_id"`
	Domains     []string `json:"domains"`
	KeyType     string   `json:"key_type"` // ECDSAP256, ECDSAP384, ECDSAP521, ED25519, RSA2048, RSA3072, RSA4096
	Provider    string   `json:"provider"` // Let's Encrypt / ZeroSSL / Google PKI / Buypass
}

type UpdateCertAutoRenewReq struct {
	// 指针用于区分缺少字段与显式关闭（false）。
	AutoRenew *bool `json:"auto_renew" binding:"required"`
}

// ListProviders 暴露受支持的 ACME 服务商及其 EAB 要求，
// 使前端不必硬编码 CA 列表与「哪些服务商必须填 EAB」的规则。
func (h *CertHandler) ListProviders(c *gin.Context) {
	type providerDTO struct {
		Name         string `json:"name"`
		RequiresEAB  bool   `json:"requires_eab"`
		HasStaging   bool   `json:"has_staging"`
		DirectoryURL string `json:"directory_url"`
	}

	providers := acme.ListProviders()
	out := make([]providerDTO, 0, len(providers))
	for _, p := range providers {
		out = append(out, providerDTO{
			Name:         p.Name,
			RequiresEAB:  p.RequiresEAB,
			HasStaging:   p.StagingURL != "",
			DirectoryURL: p.DirectoryURL,
		})
	}

	c.JSON(http.StatusOK, gin.H{"providers": out})
}

func (h *CertHandler) ListCertificates(c *gin.Context) {
	domainIDStr := c.Query("domain_id")
	query := database.DB.Preload("Applicant").Model(&model.Certificate{})
	if domainIDStr != "" {
		domainID, _ := strconv.Atoi(domainIDStr)
		query = query.Where("domain_id = ?", domainID)
	}

	var certs []model.Certificate
	if err := query.Order("id desc").Find(&certs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch certificates"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"certificates": certs, "total": len(certs)})
}

// IssueCertificate 受理签发请求。
//
// 真实 ACME 签发需要等待 DNS 传播与 CA 校验，耗时以分钟计，
// 因此这里只做同步校验并落一条 issuing 记录，随后在后台完成签发，
// 立即以 202 返回该记录。前端据此渲染「签发中」，刷新即可看到最终状态。
func (h *CertHandler) IssueCertificate(c *gin.Context) {
	var req IssueCertReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request parameters"})
		return
	}

	var domain model.Domain
	if err := database.DB.First(&domain, req.DomainID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Domain not found"})
		return
	}

	applicant := h.resolveApplicant(req.ApplicantID, req.Provider)

	cert, domainList, err := acme.GlobalIssuer.PrepareCertificate(
		&domain, req.Domains, req.KeyType, applicant)
	if err != nil {
		// 参数与配置类错误（域名不属于本区域、缺 EAB 等）属于客户端问题，
		// 用 400 让前端能直接把原因展示在表单上。
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	go func() {
		if err := acme.GlobalIssuer.CompleteIssuance(cert, &domain, domainList, applicant); err != nil {
			log.Printf("[API] 后台签发证书 #%d 失败: %v", cert.ID, err)
		}
	}()

	database.DB.Preload("Applicant").First(cert, cert.ID)
	c.JSON(http.StatusAccepted, cert)
}

// RenewCertificate 手动触发续期。同样走异步流程。
func (h *CertHandler) RenewCertificate(c *gin.Context) {
	oldCertPtr, err := resolveCertificate(c.Param("id"), true)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Certificate not found"})
		return
	}
	oldCert := *oldCertPtr

	if oldCert.Status == model.CertStatusIssuing {
		c.JSON(http.StatusConflict, gin.H{"error": "该证书正在签发中，请稍后再试"})
		return
	}

	var domain model.Domain
	if err := database.DB.First(&domain, oldCert.DomainID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Associated domain not found"})
		return
	}

	domainList := strings.Split(oldCert.Domains, ",")
	newCert, normalized, err := acme.GlobalIssuer.PrepareCertificate(
		&domain, domainList, oldCert.KeyType, oldCert.Applicant)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// 手动续期是替换同一张逻辑证书，必须继承原证书的自动续期开关；
	// PrepareCertificate 的新签发默认值为 true，不能在这里把用户显式关闭的设置悄悄打开。
	if newCert.AutoRenew != oldCert.AutoRenew {
		newCert.AutoRenew = oldCert.AutoRenew
		if err := database.DB.Model(newCert).Update("auto_renew", oldCert.AutoRenew).Error; err != nil {
			_ = database.DB.Unscoped().Delete(newCert).Error
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to preserve automatic renewal setting"})
			return
		}
	}

	oldID := oldCert.ID
	applicant := oldCert.Applicant
	go func() {
		if err := acme.GlobalIssuer.CompleteIssuance(newCert, &domain, normalized, applicant); err != nil {
			log.Printf("[API] 后台续期证书 #%d 失败: %v", newCert.ID, err)
			// 续期失败时把原因同步回旧证书，使用户在原行上就能看到问题；
			// 旧证书保持可用，不做删除。
			database.DB.Model(&model.Certificate{}).Where("id = ?", oldID).
				Update("last_error", "续期失败: "+err.Error())
			return
		}
		// 新证书就绪后才移除旧记录，确保任何时刻都有一份可用证书。
		if err := database.DB.Unscoped().
			Where("id = ?", oldID).Delete(&model.Certificate{}).Error; err != nil {
			log.Printf("[API] 删除旧证书 #%d 失败: %v", oldID, err)
		}
	}()

	database.DB.Preload("Applicant").First(newCert, newCert.ID)
	c.JSON(http.StatusAccepted, newCert)
}

func (h *CertHandler) DownloadCertificate(c *gin.Context) {
	fileType := c.Query("type") // "cert" or "key"

	cert, err := resolveCertificate(c.Param("id"), false)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Certificate not found"})
		return
	}

	if strings.TrimSpace(cert.CertPEM) == "" {
		c.JSON(http.StatusConflict, gin.H{"error": "该证书尚未签发完成，暂无可下载内容"})
		return
	}

	firstDomain := strings.Split(cert.Domains, ",")[0]
	firstDomain = strings.ReplaceAll(firstDomain, "*.", "wildcard.")

	if fileType == "key" {
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.key", firstDomain))
		c.Data(http.StatusOK, "application/x-pem-file", []byte(cert.KeyPEM))
		return
	}

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.crt", firstDomain))
	c.Data(http.StatusOK, "application/x-pem-file", []byte(cert.CertPEM))
}

// UpdateAutoRenew 针对单张证书开启或关闭自动续期。
// 自动续期 worker 本身已经按 certificates.auto_renew 过滤，这里只需原子更新该字段。
func (h *CertHandler) UpdateAutoRenew(c *gin.Context) {
	cert, err := resolveCertificate(c.Param("id"), false)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Certificate not found"})
		return
	}

	var req UpdateCertAutoRenewReq
	if err := c.ShouldBindJSON(&req); err != nil || req.AutoRenew == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "auto_renew must be true or false"})
		return
	}

	if err := database.DB.Model(&model.Certificate{}).Where("id = ?", cert.ID).
		Update("auto_renew", *req.AutoRenew).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update automatic renewal"})
		return
	}

	cert.AutoRenew = *req.AutoRenew
	c.JSON(http.StatusOK, gin.H{
		"message":    "Certificate automatic renewal updated",
		"auto_renew": cert.AutoRenew,
	})
}

func (h *CertHandler) DeleteCertificate(c *gin.Context) {
	cert, err := resolveCertificate(c.Param("id"), false)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Certificate not found"})
		return
	}
	database.DB.Delete(&model.Certificate{}, cert.ID)
	c.JSON(http.StatusOK, gin.H{"message": "Certificate deleted"})
}

// resolveApplicant 确定本次签发使用的申请人 Profile。
// 优先用显式指定的 ID；未指定时回退到默认申请人，
// 使「不选申请人直接签发」也能复用已注册的 ACME 账户。
func (h *CertHandler) resolveApplicant(applicantID *uint, providerOverride string) *model.CertApplicant {
	var app model.CertApplicant

	if applicantID != nil && *applicantID > 0 {
		if err := database.DB.First(&app, *applicantID).Error; err == nil {
			h.applyProviderOverride(&app, providerOverride)
			return &app
		}
	}

	if err := database.DB.Where("is_default = ?", true).First(&app).Error; err == nil {
		h.applyProviderOverride(&app, providerOverride)
		return &app
	}

	return nil
}

// applyProviderOverride 处理「本次签发临时换 CA」的情况。
//
// 只在内存副本上改 Provider，不写库：申请人的默认服务商是其长期配置，
// 不应被一次性的签发选择覆盖。但要注意换 CA 后原账户不可复用
// （账户与 Directory 绑定），account 层会自动重新注册。
func (h *CertHandler) applyProviderOverride(app *model.CertApplicant, providerOverride string) {
	override := strings.TrimSpace(providerOverride)
	if override == "" || strings.EqualFold(override, app.Provider) {
		return
	}
	app.Provider = override
}
