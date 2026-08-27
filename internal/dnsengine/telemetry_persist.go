package dnsengine

import (
	"sync/atomic"
	"time"
)

// telemetryPersistVersion 用于快照结构演进时的兼容判断。
const telemetryPersistVersion = 1

// TelemetryPersistState 是 TelemetryCollector 的可序列化快照，用于 Redis / 数据库持久化
// 与进程重启后的恢复。为控制体积，只导出累计量以及「非空且仍在保留窗口内」的分钟桶，
// 避免每次都序列化 43200 个（多为空的）环形槽位。
type TelemetryPersistState struct {
	Version       int                    `json:"version"`
	SavedAt       int64                  `json:"saved_at"`
	TotalQueries  uint64                 `json:"total_queries"`
	Blocked       uint64                 `json:"blocked"`
	Hourly        [24]uint64             `json:"hourly"`
	HourlySuccess [24]uint64             `json:"hourly_success"`
	HourlyBlocked [24]uint64             `json:"hourly_blocked"`
	Types         map[uint16]uint64      `json:"types"`
	Countries     map[string]uint64      `json:"countries"`
	Minutes       []MinuteBucketState    `json:"minutes"`
	Domains       map[string]DomainState `json:"domains"`
}

// MinuteBucketState 是单个分钟桶的可序列化形态。
type MinuteBucketState struct {
	Epoch     int64             `json:"e"`
	Queries   uint64            `json:"q"`
	Blocked   uint64            `json:"b"`
	Types     map[uint16]uint64 `json:"t,omitempty"`
	Countries map[string]uint64 `json:"c,omitempty"`
}

// DomainState 是单个托管区域运行时计数的可序列化形态。
type DomainState struct {
	Queries       uint64              `json:"q"`
	Blocked       uint64              `json:"b"`
	Hourly        [24]uint64          `json:"h"`
	HourlyBlocked [24]uint64          `json:"hb"`
	Types         map[uint16]uint64   `json:"t"`
	Countries     map[string]uint64   `json:"c"`
	Minutes       []MinuteBucketState `json:"m"`
}

// exportMinuteRing 导出环形缓冲中「非空且 epoch 不早于 minEpoch」的桶。
func exportMinuteRing(ring *[minuteRingSize]minuteBucket, minEpoch int64) []MinuteBucketState {
	out := make([]MinuteBucketState, 0, 128)
	for i := range ring {
		mb := &ring[i]
		if mb.epoch == 0 || mb.epoch < minEpoch || mb.queries == 0 {
			continue
		}
		st := MinuteBucketState{Epoch: mb.epoch, Queries: mb.queries, Blocked: mb.blocked}
		if len(mb.types) > 0 {
			st.Types = make(map[uint16]uint64, len(mb.types))
			for k, v := range mb.types {
				st.Types[k] = v
			}
		}
		if len(mb.countries) > 0 {
			st.Countries = make(map[string]uint64, len(mb.countries))
			for k, v := range mb.countries {
				st.Countries[k] = v
			}
		}
		out = append(out, st)
	}
	return out
}

// restoreMinuteRing 把快照中的分钟桶按 epoch 放回环形缓冲对应槽位。
func restoreMinuteRing(ring *[minuteRingSize]minuteBucket, minutes []MinuteBucketState) {
	for _, st := range minutes {
		slot := ((st.Epoch % minuteRingSize) + minuteRingSize) % minuteRingSize
		mb := &ring[slot]
		mb.epoch = st.Epoch
		mb.queries = st.Queries
		mb.blocked = st.Blocked
		mb.types = nil
		mb.countries = nil
		if len(st.Types) > 0 {
			mb.types = make(map[uint16]uint64, len(st.Types))
			for k, v := range st.Types {
				mb.types[k] = v
			}
		}
		if len(st.Countries) > 0 {
			mb.countries = make(map[string]uint64, len(st.Countries))
			for k, v := range st.Countries {
				mb.countries[k] = v
			}
		}
	}
}

// ExportState 在持读锁下导出完整遥测快照，供持久化层写入 Redis / 数据库。
func (tc *TelemetryCollector) ExportState() TelemetryPersistState {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	minEpoch := time.Now().Unix()/60 - minuteRingSize + 1

	st := TelemetryPersistState{
		Version:       telemetryPersistVersion,
		SavedAt:       time.Now().Unix(),
		TotalQueries:  atomic.LoadUint64(&tc.totalQueries),
		Blocked:       atomic.LoadUint64(&tc.blockedQ),
		Hourly:        tc.hourlyBuckets,
		HourlySuccess: tc.hourlySuccess,
		HourlyBlocked: tc.hourlyBlocked,
		Types:         make(map[uint16]uint64, len(tc.typeCounts)),
		Countries:     make(map[string]uint64, len(tc.countryCounts)),
		Minutes:       exportMinuteRing(&tc.minuteRing, minEpoch),
		Domains:       make(map[string]DomainState, len(tc.domains)),
	}
	for qt, ptr := range tc.typeCounts {
		if ptr != nil {
			st.Types[qt] = atomic.LoadUint64(ptr)
		}
	}
	for code, ptr := range tc.countryCounts {
		if ptr != nil {
			st.Countries[code] = atomic.LoadUint64(ptr)
		}
	}
	for zone, dm := range tc.domains {
		ds := DomainState{
			Queries:       dm.queries,
			Blocked:       dm.blocked,
			Hourly:        dm.hourly,
			HourlyBlocked: dm.hourlyBlocked,
			Types:         make(map[uint16]uint64, len(dm.types)),
			Countries:     make(map[string]uint64, len(dm.countries)),
			Minutes:       exportMinuteRing(&dm.minuteRing, minEpoch),
		}
		for k, v := range dm.types {
			ds.Types[k] = v
		}
		for k, v := range dm.countries {
			ds.Countries[k] = v
		}
		st.Domains[zone] = ds
	}
	return st
}

// ImportState 在持写锁下用快照覆盖当前内存计数。仅应在进程启动恢复阶段调用，
// 此时尚无查询流量，覆盖不会与实时累加竞争。
func (tc *TelemetryCollector) ImportState(st TelemetryPersistState) {
	if st.Version == 0 {
		return
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()

	atomic.StoreUint64(&tc.totalQueries, st.TotalQueries)
	atomic.StoreUint64(&tc.blockedQ, st.Blocked)
	tc.hourlyBuckets = st.Hourly
	tc.hourlySuccess = st.HourlySuccess
	tc.hourlyBlocked = st.HourlyBlocked
	for qt, v := range st.Types {
		ptr := tc.typeCounts[qt]
		if ptr == nil {
			ptr = new(uint64)
			tc.typeCounts[qt] = ptr
		}
		atomic.StoreUint64(ptr, v)
	}
	for code, v := range st.Countries {
		ptr := tc.countryCounts[code]
		if ptr == nil {
			ptr = new(uint64)
			tc.countryCounts[code] = ptr
		}
		atomic.StoreUint64(ptr, v)
	}
	restoreMinuteRing(&tc.minuteRing, st.Minutes)

	for zone, ds := range st.Domains {
		dm := tc.domains[zone]
		if dm == nil {
			dm = &domainMetrics{
				types:     make(map[uint16]uint64),
				countries: make(map[string]uint64),
			}
			tc.domains[zone] = dm
		}
		dm.queries = ds.Queries
		dm.blocked = ds.Blocked
		dm.hourly = ds.Hourly
		dm.hourlyBlocked = ds.HourlyBlocked
		if dm.types == nil {
			dm.types = make(map[uint16]uint64)
		}
		for k, v := range ds.Types {
			dm.types[k] = v
		}
		if dm.countries == nil {
			dm.countries = make(map[string]uint64)
		}
		for k, v := range ds.Countries {
			dm.countries[k] = v
		}
		restoreMinuteRing(&dm.minuteRing, ds.Minutes)
	}
}
