package model

import (
	"time"

	"gorm.io/gorm"
)

// ExperimentRun 每次实验执行的元数据（用于论文级隔离与可复现）
type ExperimentRun struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	TaskType     string `gorm:"type:varchar(100);not null;index" json:"task_type"`
	RunsPerGroup int    `json:"runs_per_group"`
	Seed         int64  `gorm:"index" json:"seed"`
	GroupsJSON   string `gorm:"type:text" json:"groups_json"`
	// 规则变更模式：none/low/high
	RuleMode string `gorm:"type:varchar(20);index" json:"rule_mode"`
	// 备注/结论文件路径
	ResultPath     string `gorm:"type:varchar(500)" json:"result_path"`
	ConclusionPath string `gorm:"type:varchar(500)" json:"conclusion_path"`
}
