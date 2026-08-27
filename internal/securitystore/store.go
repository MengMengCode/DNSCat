// Package securitystore 负责域名安全防护的持久化：
//   - 启动时把数据库中的历史拦截计数装载回内存引擎（统计跨重启累计，不归零）
//   - 为尚无策略的域名补齐默认策略，保证新老域名都有防护基线
//   - 周期性把内存中的拦截计数落库，并在优雅退出时做最后一次刷写
package securitystore

import (
	"log"
	"sync"
	"time"

	"gorm.io/gorm"

	"dnscat/internal/dnsengine"
	"dnscat/internal/model"
)

type Store struct {
	db       *gorm.DB
	interval time.Duration
	stopOnce sync.Once
	stopCh   chan struct{}
}

func New(db *gorm.DB) *Store {
	return &Store{
		db:       db,
		interval: 30 * time.Second,
		stopCh:   make(chan struct{}),
	}
}

// EnsurePolicies 为所有还没有安全策略的域名写入默认策略。
// 返回新建的条数，便于启动日志观察。
func (s *Store) EnsurePolicies() int {
	if s.db == nil {
		return 0
	}
	var domains []model.Domain
	if err := s.db.Select("id").Find(&domains).Error; err != nil {
		log.Printf("[security] 读取域名列表失败: %v", err)
		return 0
	}
	if len(domains) == 0 {
		return 0
	}

	var existing []model.DomainSecurityPolicy
	if err := s.db.Select("domain_id").Find(&existing).Error; err != nil {
		log.Printf("[security] 读取安全策略失败: %v", err)
		return 0
	}
	have := make(map[uint]struct{}, len(existing))
	for _, p := range existing {
		have[p.DomainID] = struct{}{}
	}

	created := 0
	for _, d := range domains {
		if _, ok := have[d.ID]; ok {
			continue
		}
		policy := model.DefaultSecurityPolicy(d.ID)
		if err := s.db.Create(&policy).Error; err != nil {
			log.Printf("[security] 为域名 %d 创建默认策略失败: %v", d.ID, err)
			continue
		}
		created++
	}
	return created
}

// Restore 装载历史统计并补齐默认策略。应在 DNS 区域加载之前调用，
// 这样首次载入区域时就能拿到策略并编译成运行时规则。
func (s *Store) Restore() {
	if s.db == nil {
		return
	}
	if n := s.EnsurePolicies(); n > 0 {
		log.Printf("[security] 已为 %d 个域名补齐默认安全策略", n)
	}

	var stats []model.DomainSecurityStat
	if err := s.db.Find(&stats).Error; err != nil {
		log.Printf("[security] 装载历史攻击统计失败: %v", err)
		return
	}
	if len(stats) > 0 {
		dnsengine.GlobalSecurity.LoadStats(stats)
		log.Printf("[security] 已装载 %d 个域名的历史攻击统计", len(stats))
	}
}

func (s *Store) Start() {
	go s.loop()
}

func (s *Store) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
}

func (s *Store) loop() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.FlushNow()
		}
	}
}

// FlushNow 把自上次刷写以来有变化的拦截计数写回数据库。
func (s *Store) FlushNow() {
	if s.db == nil {
		return
	}
	rows := dnsengine.GlobalSecurity.DirtyStats()
	for i := range rows {
		row := rows[i]
		var existing model.DomainSecurityStat
		err := s.db.Where("domain_id = ?", row.DomainID).First(&existing).Error
		if err == nil {
			// 用 Save 全字段覆盖：Updates 对结构体会跳过零值，计数归零时会写不进去。
			row.ID = existing.ID
			if err := s.db.Save(&row).Error; err != nil {
				log.Printf("[security] 更新域名 %d 攻击统计失败: %v", row.DomainID, err)
			}
			continue
		}
		if err := s.db.Create(&row).Error; err != nil {
			log.Printf("[security] 写入域名 %d 攻击统计失败: %v", row.DomainID, err)
		}
	}
}
