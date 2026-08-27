package model

import "time"

// TelemetrySnapshot 持久化 DNS 遥测的最新快照（JSON 序列化后的 TelemetryPersistState）。
// 采用单行覆盖（固定主键 ID=1），作为 Redis 数据丢失时的兜底恢复来源；
// Redis 承担高频实时快照，数据库承担低频归档，二者共同保证进程重启不丢统计。
type TelemetrySnapshot struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Data      string    `gorm:"type:longtext" json:"-"`
	SavedAt   int64     `json:"saved_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
