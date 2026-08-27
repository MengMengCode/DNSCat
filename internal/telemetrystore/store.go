// Package telemetrystore 负责把内存态 DNS 遥测（TelemetryCollector）持久化：
// 高频写 Redis（实时、靠 AOF 落盘），低频写数据库（长期归档、Redis 丢失时兜底），
// 进程启动时从 Redis（空则数据库）恢复，优雅退出时做最后一次刷写，
// 从而保证程序重启不丢统计，且不会因未及时同步而丢数据。
package telemetrystore

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"gorm.io/gorm"

	"dnscat/internal/cache"
	"dnscat/internal/dnsengine"
	"dnscat/internal/model"
)

// redisSnapshotKey 是遥测快照在 Redis 中的键；单键覆盖写。
const redisSnapshotKey = "dnscat:telemetry:snapshot"

// dbSnapshotID 为快照单行主键：数据库中始终只保留最新一行遥测快照。
const dbSnapshotID = 1

// Store 驱动遥测的持久化与恢复。
type Store struct {
	db           *gorm.DB
	redisEnabled bool

	flushInterval time.Duration // 内存 -> Redis 的高频刷写间隔
	dbInterval    time.Duration // 内存 -> 数据库 的低频归档间隔

	stopOnce sync.Once
	stopCh   chan struct{}
}

// New 构造持久化器。redisEnabled 来自 cfg.Redis.Enabled，为 false 时仅用数据库。
func New(db *gorm.DB, redisEnabled bool) *Store {
	return &Store{
		db:            db,
		redisEnabled:  redisEnabled,
		flushInterval: 15 * time.Second,
		dbInterval:    5 * time.Minute,
		stopCh:        make(chan struct{}),
	}
}

func (s *Store) useRedis() bool {
	return s.redisEnabled && cache.GlobalCache != nil
}

// Restore 在启动时把最近一次快照恢复到内存遥测。优先 Redis（最新），
// 失败或为空则回退数据库；两者都没有则保持空白（首次启动）。
// 成功从数据库恢复后会顺带回填 Redis，保证后续读路径一致。
func (s *Store) Restore() {
	if state, ok := s.loadFromRedis(); ok {
		dnsengine.GlobalTelemetry.ImportState(state)
		log.Printf("[telemetry] 已从 Redis 恢复遥测快照（saved_at=%d）", state.SavedAt)
		return
	}
	if state, ok := s.loadFromDB(); ok {
		dnsengine.GlobalTelemetry.ImportState(state)
		log.Printf("[telemetry] 已从数据库恢复遥测快照（saved_at=%d）", state.SavedAt)
		if s.useRedis() {
			_ = cache.GlobalCache.Set(context.Background(), redisSnapshotKey, state, 0)
		}
		return
	}
	log.Printf("[telemetry] 无历史快照可恢复，从空白开始")
}

// Start 启动后台刷写与归档循环。非阻塞。
func (s *Store) Start() {
	go s.loop()
}

// Stop 停止后台循环（不做最后刷写；优雅退出请显式调用 FlushNow）。
func (s *Store) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
}

func (s *Store) loop() {
	flushTicker := time.NewTicker(s.flushInterval)
	dbTicker := time.NewTicker(s.dbInterval)
	defer flushTicker.Stop()
	defer dbTicker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-flushTicker.C:
			s.flushToRedis()
		case <-dbTicker.C:
			s.syncToDB()
		}
	}
}

// FlushNow 立即把当前内存快照同时刷到 Redis 与数据库，供优雅退出时兜底调用，
// 确保重启前最后一个刷写周期内的增量不会因未同步而丢失。
func (s *Store) FlushNow() {
	state := dnsengine.GlobalTelemetry.ExportState()
	if s.useRedis() {
		if err := cache.GlobalCache.Set(context.Background(), redisSnapshotKey, state, 0); err != nil {
			log.Printf("[telemetry] 退出刷写 Redis 失败: %v", err)
		}
	}
	s.saveToDB(state)
}

func (s *Store) flushToRedis() {
	if !s.useRedis() {
		return
	}
	state := dnsengine.GlobalTelemetry.ExportState()
	if err := cache.GlobalCache.Set(context.Background(), redisSnapshotKey, state, 0); err != nil {
		log.Printf("[telemetry] 刷写 Redis 失败: %v", err)
	}
}

func (s *Store) syncToDB() {
	state := dnsengine.GlobalTelemetry.ExportState()
	s.saveToDB(state)
}

func (s *Store) saveToDB(state dnsengine.TelemetryPersistState) {
	if s.db == nil {
		return
	}
	data, err := json.Marshal(state)
	if err != nil {
		log.Printf("[telemetry] 归档序列化失败: %v", err)
		return
	}
	snap := model.TelemetrySnapshot{
		ID:        dbSnapshotID,
		Data:      string(data),
		SavedAt:   state.SavedAt,
		UpdatedAt: time.Now(),
	}
	// 主键固定，Save 存在则更新、不存在则插入，始终只保留最新一行。
	if err := s.db.Save(&snap).Error; err != nil {
		log.Printf("[telemetry] 归档数据库失败: %v", err)
	}
}

func (s *Store) loadFromRedis() (dnsengine.TelemetryPersistState, bool) {
	var state dnsengine.TelemetryPersistState
	if !s.useRedis() {
		return state, false
	}
	if err := cache.GlobalCache.Get(context.Background(), redisSnapshotKey, &state); err != nil {
		return state, false
	}
	if state.Version == 0 {
		return state, false
	}
	return state, true
}

func (s *Store) loadFromDB() (dnsengine.TelemetryPersistState, bool) {
	var state dnsengine.TelemetryPersistState
	if s.db == nil {
		return state, false
	}
	var snap model.TelemetrySnapshot
	if err := s.db.First(&snap, dbSnapshotID).Error; err != nil || snap.Data == "" {
		return state, false
	}
	if err := json.Unmarshal([]byte(snap.Data), &state); err != nil {
		log.Printf("[telemetry] 解析数据库快照失败: %v", err)
		return state, false
	}
	if state.Version == 0 {
		return state, false
	}
	return state, true
}
