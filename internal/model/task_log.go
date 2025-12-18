package model

import (
	"time"

	"gorm.io/gorm"
)

// TaskLog 用于记录“可检索的历史案例/日志”（主要用于 B 组：检索日志而非抽象规则）
type TaskLog struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	TaskID    uint   `gorm:"not null;index" json:"task_id"`
	RunID     uint   `gorm:"index" json:"run_id"`
	TaskType  string `gorm:"type:varchar(100);not null;index" json:"task_type"`
	GroupType string `gorm:"type:varchar(10);index" json:"group_type"`

	// 供调试/可解释性：workflow 的 system/query 以及检索到的记忆/日志引用
	SystemPrompt string `gorm:"type:longtext" json:"system_prompt"`
	QueryInput   string `gorm:"type:longtext" json:"query_input"`
	MemoryIDs    string `gorm:"type:varchar(500)" json:"memory_ids"`
}
