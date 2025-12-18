package model

import (
	"time"

	"gorm.io/gorm"
)

// Memory 外挂记忆表 - 存储抽象规则
type Memory struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	// 实验Run隔离（论文级严谨：每次实验独立）
	RunID uint `gorm:"index" json:"run_id"`

	// 触发条件（模式匹配）
	// 注意：trigger是MySQL保留关键字，需要在gorm tag中指定列名
	Trigger string `gorm:"column:trigger;type:varchar(500);not null;index" json:"trigger"`

	// 学到的经验（抽象规则）
	Lesson string `gorm:"type:text;not null" json:"lesson"`

	// 来源（从哪个反馈中得出）
	DerivedFrom string `gorm:"type:varchar(200)" json:"derived_from"`

	// 适用范围
	ApplyTo string `gorm:"type:varchar(200)" json:"apply_to"`

	// 置信度
	Confidence float64 `gorm:"type:decimal(3,2);default:0.5" json:"confidence"`

	// 版本（用于演化）
	Version int `gorm:"default:1" json:"version"`

	// 使用次数（用于评估有效性）
	UseCount int `gorm:"default:0" json:"use_count"`
}

// Task 任务历史表
type Task struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	// 实验Run隔离（论文级严谨：每次实验独立）
	RunID uint `gorm:"index" json:"run_id"`

	// 任务类型
	TaskType string `gorm:"type:varchar(100);not null;index" json:"task_type"`

	// 输入
	Input string `gorm:"type:text;not null" json:"input"`

	// Agent输出
	Output string `gorm:"type:text" json:"output"`

	// 是否正确
	IsCorrect *bool `gorm:"type:boolean" json:"is_correct"`

	// 使用的记忆ID（多个用逗号分隔）
	MemoryIDs string `gorm:"type:varchar(500)" json:"memory_ids"`

	// Token消耗
	TokenCount int `json:"token_count"`

	// 实验组（A/B/C）
	GroupType string `gorm:"type:varchar(10);index" json:"group_type"`
}

// Feedback 反馈记录表
type Feedback struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	// 实验Run隔离（论文级严谨：每次实验独立）
	RunID uint `gorm:"index" json:"run_id"`

	// 关联的任务ID
	TaskID uint `gorm:"not null;index" json:"task_id"`

	// 反馈类型（correct/incorrect/better）
	Type string `gorm:"type:varchar(20);not null" json:"type"`

	// 反馈内容
	Content string `gorm:"type:text;not null" json:"content"`

	// 是否已用于生成记忆
	UsedForMemory bool `gorm:"default:false" json:"used_for_memory"`

	// 生成的记忆ID
	MemoryID *uint `json:"memory_id"`
}
