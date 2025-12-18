package handler

import (
	"net/http"

	"mem-test/internal/db"
	"mem-test/internal/model"
	"mem-test/internal/service"

	"github.com/gin-gonic/gin"
)

type ExperimentHandler struct {
	runner *service.ExperimentRunner
}

func NewExperimentHandler(runner *service.ExperimentRunner) *ExperimentHandler {
	return &ExperimentHandler{runner: runner}
}

// GetExperimentStats 获取实验统计数据
func (h *ExperimentHandler) GetExperimentStats(c *gin.Context) {
	groupType := c.Query("group_type") // A/B/C

	var tasks []model.Task
	query := db.DB.Model(&model.Task{})

	if groupType != "" {
		query = query.Where("group_type = ?", groupType)
	}

	if err := query.Find(&tasks).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 计算统计
	stats := h.calculateStats(tasks)

	c.JSON(http.StatusOK, gin.H{
		"group_type": groupType,
		"stats":      stats,
		"total":      len(tasks),
	})
}

// CompareGroups 对比A/B/C组
func (h *ExperimentHandler) CompareGroups(c *gin.Context) {
	groups := []string{"A", "B", "C"}
	comparison := make(map[string]interface{})

	for _, group := range groups {
		var tasks []model.Task
		if err := db.DB.Where("group_type = ?", group).Find(&tasks).Error; err != nil {
			continue
		}
		comparison[group] = h.calculateStats(tasks)
	}

	c.JSON(http.StatusOK, gin.H{
		"comparison": comparison,
	})
}

// GetErrorTrend 获取错误趋势（随时间变化）
func (h *ExperimentHandler) GetErrorTrend(c *gin.Context) {
	groupType := c.Query("group_type")

	var results []struct {
		Date      string `gorm:"column:date"`
		Total     int    `gorm:"column:total"`
		Correct   int    `gorm:"column:correct"`
		Incorrect int    `gorm:"column:incorrect"`
	}

	query := `
		SELECT 
			DATE(created_at) as date,
			COUNT(*) as total,
			SUM(CASE WHEN is_correct = 1 THEN 1 ELSE 0 END) as correct,
			SUM(CASE WHEN is_correct = 0 THEN 1 ELSE 0 END) as incorrect
		FROM tasks
		WHERE deleted_at IS NULL
	`

	if groupType != "" {
		query += " AND group_type = '" + groupType + "'"
	}

	query += " GROUP BY DATE(created_at) ORDER BY date"

	if err := db.DB.Raw(query).Scan(&results).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"trend": results,
	})
}

// RunExperiment 自动跑一批实验：生成样本 -> 执行 A/B/C -> 自动判题 -> C组自动反思写记忆
func (h *ExperimentHandler) RunExperiment(c *gin.Context) {
	if h.runner == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "experiment runner not initialized"})
		return
	}

	var req service.ExperimentRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.runner.Run(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"result":          result,
		"result_path":     result.ResultPath,
		"conclusion_path": result.ConclusionPath,
	})
}

func (h *ExperimentHandler) calculateStats(tasks []model.Task) map[string]interface{} {
	stats := map[string]interface{}{
		"total":        len(tasks),
		"correct":      0,
		"incorrect":    0,
		"unknown":      0,
		"total_tokens": 0,
		"avg_tokens":   0,
		"error_rate":   0.0,
	}

	if len(tasks) == 0 {
		return stats
	}

	for _, task := range tasks {
		stats["total_tokens"] = stats["total_tokens"].(int) + task.TokenCount

		if task.IsCorrect == nil {
			stats["unknown"] = stats["unknown"].(int) + 1
		} else if *task.IsCorrect {
			stats["correct"] = stats["correct"].(int) + 1
		} else {
			stats["incorrect"] = stats["incorrect"].(int) + 1
		}
	}

	total := len(tasks)
	correct := stats["correct"].(int)
	incorrect := stats["incorrect"].(int)
	judged := correct + incorrect

	// 错误率只针对已判定的任务计算（排除 unknown）
	if judged > 0 {
		stats["error_rate"] = float64(incorrect) / float64(judged)
	} else {
		stats["error_rate"] = 0.0
	}

	if total > 0 {
		stats["avg_tokens"] = stats["total_tokens"].(int) / total
	}

	return stats
}
