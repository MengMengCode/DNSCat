// 安全日志的持久化层：把内存事件环异步落到 Redis LIST，进程重启后再恢复回内存。
//
// 为什么用 Redis LIST 而不是 SQL 表：
//   - 保留上限本身就是「写满即丢弃最旧」的 FIFO 语义，LPUSH + LTRIM 是它的
//     O(1) 原生实现，一次往返即可完成写入与裁剪；用 SQL 则要 INSERT 之后再
//     count/排序/DELETE，攻击洪峰下这类写放大很容易把库拖垮。
//   - 日志是可滚动丢弃的短期数据，且条数有确定上界（单域名最多 20 万条），
//     不需要长期归档，也就不需要占用关系库的写入预算。
//
// 分工：解析热路径只往内存追加（零 IO），本层每隔数秒批量刷一次；
// 查询仍然全部走内存，因此筛选与分页的性能与之前完全一致。
// Redis 不可用时自动退化为「仅内存、重启清空」，即改造前的行为。
package seclogstore

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"dnscat/internal/cache"
	"dnscat/internal/dnsengine"
	"dnscat/internal/model"
)

// eventKey 返回某域名安全日志在 Redis 中的键。表头为最新事件。
func eventKey(domainID uint) string {
	return fmt.Sprintf("dnscat:sec:events:%d", domainID)
}

// Store 驱动安全日志的刷盘与恢复。
type Store struct {
	flushInterval time.Duration

	stopOnce sync.Once
	stopCh   chan struct{}
}

// New 构造持久化器。是否真正启用取决于 Redis 是否可用，无需额外传配置：
// cache.InitCache 只有在 redis.enabled 且 PING 成功时才把 Redis 标记为可用。
func New() *Store {
	return &Store{
		flushInterval: 3 * time.Second,
		stopCh:        make(chan struct{}),
	}
}

// available 表示 Redis 持久化是否可用；不可用时全部操作降级为空操作，
// 安全日志退回「仅内存、重启清空」的行为。
func available() bool {
	return cache.GlobalCache != nil && cache.GlobalCache.RedisAvailable()
}

func (s *Store) useRedis() bool {
	return available()
}

// Restore 在启动时把 Redis 里的历史日志恢复到内存事件环。
// limits 为各域名的保留上限（来自各自的安全策略），缺省用兜底值。
func (s *Store) Restore(limits map[uint]int) {
	if !s.useRedis() {
		log.Printf("[seclog] Redis 不可用，安全日志仅保存在内存中（重启后清空）")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var restoredDomains, restoredEvents int
	for domainID, limit := range limits {
		if limit <= 0 {
			limit = model.DefaultLogRetentionLimit
		}
		raw, err := cache.GlobalCache.ListRange(ctx, eventKey(domainID), 0, int64(limit-1))
		if err != nil || len(raw) == 0 {
			if err != nil {
				log.Printf("[seclog] 读取域名 %d 的历史日志失败: %v", domainID, err)
			}
			continue
		}

		// Redis 中表头是最新事件（降序）；内存事件环要求时间升序，故倒序还原。
		events := make([]dnsengine.SecurityEvent, 0, len(raw))
		for i := len(raw) - 1; i >= 0; i-- {
			var ev dnsengine.SecurityEvent
			if err := json.Unmarshal([]byte(raw[i]), &ev); err != nil {
				continue // 跳过损坏条目，不影响其余日志恢复
			}
			events = append(events, ev)
		}
		if len(events) == 0 {
			continue
		}
		dnsengine.GlobalSecurity.RestoreEvents(domainID, events, limit)
		restoredDomains++
		restoredEvents += len(events)
	}

	if restoredEvents > 0 {
		log.Printf("[seclog] 已从 Redis 恢复 %d 个域名共 %d 条安全日志", restoredDomains, restoredEvents)
	} else {
		log.Printf("[seclog] Redis 中无历史安全日志可恢复")
	}
}

// Start 启动后台刷盘循环。非阻塞。
func (s *Store) Start() {
	if !s.useRedis() {
		return
	}
	go s.loop()
}

// Stop 停止后台循环（不做最后刷盘；优雅退出请显式调用 FlushNow）。
func (s *Store) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
}

func (s *Store) loop() {
	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.flush()
		}
	}
}

// FlushNow 立即刷一次，供优雅退出时调用，避免丢掉最后一个刷盘周期的日志。
func (s *Store) FlushNow() {
	if !s.useRedis() {
		return
	}
	s.flush()
}

// flush 把待刷盘事件批量写入 Redis。每个域名一次 pipeline（LPUSH + LTRIM）。
func (s *Store) flush() {
	pending := dnsengine.GlobalSecurity.DrainPendingEvents()
	if len(pending) == 0 {
		return
	}

	limits := dnsengine.GlobalSecurity.EventLimits()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	for domainID, events := range pending {
		if len(events) == 0 {
			continue
		}
		items := make([]string, 0, len(events))
		for i := range events {
			data, err := json.Marshal(events[i])
			if err != nil {
				continue
			}
			items = append(items, string(data))
		}
		if len(items) == 0 {
			continue
		}

		limit := limits[domainID]
		if limit <= 0 {
			limit = model.DefaultLogRetentionLimit
		}
		if err := cache.GlobalCache.ListPushCapped(ctx, eventKey(domainID), items, limit); err != nil {
			// 刷盘失败只记日志：事件仍在内存里可查，下一轮新事件会继续尝试。
			// 这里不把 events 放回 pending，否则 Redis 长期故障会导致内存堆积。
			log.Printf("[seclog] 写入域名 %d 的安全日志失败: %v", domainID, err)
		}
	}
}

// Trim 在保留上限被调小后立即收敛 Redis 中已存储的条数，与内存侧同步。
func Trim(domainID uint, limit int) {
	if !available() || limit <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := cache.GlobalCache.ListTrim(ctx, eventKey(domainID), limit); err != nil {
		log.Printf("[seclog] 裁剪域名 %d 的安全日志失败: %v", domainID, err)
	}
}

// Drop 删除某域名的全部持久化日志，供「清零统计」与删除域名时调用。
func Drop(domainID uint) {
	if !available() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := cache.GlobalCache.Delete(ctx, eventKey(domainID)); err != nil {
		log.Printf("[seclog] 删除域名 %d 的安全日志失败: %v", domainID, err)
	}
}
