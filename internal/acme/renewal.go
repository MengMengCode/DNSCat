package acme

import (
	"context"
	"log"
	"time"

	"dnscat/internal/database"
	"dnscat/internal/model"
)

const (
	// renewalCheckInterval 续期巡检周期。证书有效期以月计，每 6 小时扫一次足够，
	// 同时也保证服务重启后能较快补上错过的续期窗口。
	renewalCheckInterval = 6 * time.Hour
	// renewalThreshold 剩余有效期低于该值时触发续期。
	// 取 30 天是行业惯例：Let's Encrypt 官方建议在到期前 30 天续期，
	// 留出足够窗口应对校验失败后的重试。
	renewalThreshold = 30 * 24 * time.Hour
	// renewalStartupDelay 启动后延迟首次巡检，避免与开机时的 zone 预加载、
	// 集群同步争抢资源。
	renewalStartupDelay = 2 * time.Minute
)

// Renewer 周期性扫描即将到期的证书并自动续期。
type Renewer struct {
	issuer    *Issuer
	stopChan  chan struct{}
	isRunning bool
}

var GlobalRenewer *Renewer

func NewRenewer(issuer *Issuer) *Renewer {
	if issuer == nil {
		issuer = GlobalIssuer
	}
	r := &Renewer{
		issuer:   issuer,
		stopChan: make(chan struct{}),
	}
	GlobalRenewer = r
	return r
}

func (r *Renewer) Start(ctx context.Context) {
	if r.isRunning {
		return
	}
	r.isRunning = true

	go func() {
		log.Printf("[ACME Renewer] 自动续期巡检已启动（周期 %v，阈值 剩余不足 %v）",
			renewalCheckInterval, renewalThreshold)

		select {
		case <-ctx.Done():
			return
		case <-r.stopChan:
			return
		case <-time.After(renewalStartupDelay):
		}

		r.runOnce()

		ticker := time.NewTicker(renewalCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-r.stopChan:
				return
			case <-ticker.C:
				r.runOnce()
			}
		}
	}()
}

func (r *Renewer) Stop() {
	if r.isRunning {
		close(r.stopChan)
		r.isRunning = false
	}
}

// runOnce 执行一轮续期巡检。
func (r *Renewer) runOnce() {
	if database.DB == nil {
		return
	}

	deadline := time.Now().Add(renewalThreshold)

	var candidates []model.Certificate
	// 仅处理开启了自动续期、且已成功签发过的证书。
	// issuing 状态的不碰（可能正有另一轮签发在跑）；
	// failed 状态的也纳入：失败的证书应当被重试，否则永远卡死。
	err := database.DB.Preload("Applicant").
		Where("auto_renew = ?", true).
		Where("status IN ?", []model.CertStatus{model.CertStatusValid, model.CertStatusExpired, model.CertStatusFailed}).
		Where("valid_to IS NULL OR valid_to < ?", deadline).
		Find(&candidates).Error
	if err != nil {
		log.Printf("[ACME Renewer] 查询待续期证书失败: %v", err)
		return
	}

	if len(candidates) == 0 {
		return
	}

	log.Printf("[ACME Renewer] 本轮发现 %d 张待续期证书", len(candidates))
	for i := range candidates {
		r.renewOne(&candidates[i])
	}
}

// renewOne 续期单张证书：重新走一次完整签发，成功后删除旧记录。
//
// 之所以「新签 + 删旧」而不是原地更新：签发是长流程且可能失败，
// 保留旧证书直到新证书就绪，可确保任何时刻都有一份可用的证书在库里。
func (r *Renewer) renewOne(old *model.Certificate) {
	var domain model.Domain
	if err := database.DB.First(&domain, old.DomainID).Error; err != nil {
		log.Printf("[ACME Renewer] 证书 #%d 关联的域名已不存在，跳过", old.ID)
		return
	}

	remaining := "未知"
	if old.ValidTo != nil {
		remaining = time.Until(*old.ValidTo).Round(time.Hour).String()
	}
	log.Printf("[ACME Renewer] 开始续期证书 #%d (%s)，剩余有效期 %s",
		old.ID, old.Domains, remaining)

	domainList := splitDomains(old.Domains)
	newCert, err := r.issuer.IssueCertificate(&domain, domainList, old.KeyType, old.Applicant)
	if err != nil {
		// 失败原因已由 IssueCertificate 写入新建的失败记录；
		// 这里只把原因同步回旧记录，便于用户在原证书行上看到续期失败。
		database.DB.Model(old).Updates(map[string]interface{}{
			"last_error": "自动续期失败: " + err.Error(),
			"updated_at": time.Now(),
		})
		log.Printf("[ACME Renewer] 证书 #%d 续期失败: %v", old.ID, err)
		return
	}

	// 新证书已就绪，硬删除旧记录避免列表里堆积历史证书。
	if err := database.DB.Unscoped().Delete(old).Error; err != nil {
		log.Printf("[ACME Renewer] 删除旧证书 #%d 失败: %v", old.ID, err)
	}
	log.Printf("[ACME Renewer] 证书 #%d 已续期为 #%d", old.ID, newCert.ID)
}

// MarkExpiredCertificates 把已过期证书的状态同步为 expired。
// 供巡检与启动时调用，使前端状态与真实有效期一致。
func MarkExpiredCertificates() {
	if database.DB == nil {
		return
	}
	database.DB.Model(&model.Certificate{}).
		Where("status = ?", model.CertStatusValid).
		Where("valid_to IS NOT NULL AND valid_to < ?", time.Now()).
		Updates(map[string]interface{}{
			"status":     model.CertStatusExpired,
			"updated_at": time.Now(),
		})
}
