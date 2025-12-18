package handler

import (
	"net/http"
	"strconv"
	"strings"

	"mem-test/internal/db"
	"mem-test/internal/model"
	"mem-test/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
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
	// 新版：按轮次输出曲线数据（用于前端图表），支持 mode=low/high/none
	mode := c.Query("mode")
	if mode == "" {
		mode = "none"
	}
	runIDStr := strings.TrimSpace(c.Query("run_id"))
	var run model.ExperimentRun
	q := db.DB.Model(&model.ExperimentRun{})
	if runIDStr != "" {
		if rid, err := strconv.ParseUint(runIDStr, 10, 64); err == nil && rid > 0 {
			q = q.Where("id = ?", uint(rid))
		}
	} else {
		q = q.Where("rule_mode = ?", mode).Order("id DESC")
	}
	if err := q.First(&run).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "未找到对应实验 run"})
		return
	}

	rounds := run.RunsPerGroup
	if rounds <= 0 {
		rounds = 0
	}

	groups := []string{"A", "B", "C"}
	curves := map[string]service.Curves{}
	var thresholds []int

	for _, g := range groups {
		var tasks []model.Task
		if err := db.DB.Where("run_id = ? AND group_type = ?", run.ID, g).Find(&tasks).Error; err != nil {
			continue
		}
		flags, ths, _ := service.ExtractRoundFlags(tasks, rounds)
		if thresholds == nil || len(thresholds) == 0 {
			thresholds = ths
		}
		curves[g] = service.BuildCumulativeCurves(flags, rounds)
	}

	c.JSON(http.StatusOK, gin.H{
		"run_id":     run.ID,
		"rule_mode":  run.RuleMode,
		"rounds":     rounds,
		"thresholds": thresholds,
		"curves":     curves,
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

// CompareGroupsByModes 区分高频/低频规则变更两种模式，输出组间对比 + 正确率变化曲线 + 试错次数
func (h *ExperimentHandler) CompareGroupsByModes(c *gin.Context) {
	modes := []string{"low", "high"}
	resp := map[string]interface{}{}

	for _, mode := range modes {
		var run model.ExperimentRun
		if err := db.DB.Where("rule_mode = ?", mode).Order("id DESC").First(&run).Error; err != nil {
			continue
		}
		rounds := run.RunsPerGroup
		groups := []string{"A", "B", "C"}

		modeCurve := service.ModeCurve{
			RunID:                  run.ID,
			RuleMode:               mode,
			Rounds:                 rounds,
			Groups:                 groups,
			Overall:                map[string]service.GroupStats{},
			Curves:                 map[string]service.Curves{},
			FirstErrorRound:        map[string]int{},
			MemoryChangeStartRound: -1,
		}

		// overall stats（只统计本 run）
		for _, g := range groups {
			var tasks []model.Task
			_ = db.DB.Where("run_id = ? AND group_type = ?", run.ID, g).Find(&tasks).Error
			modeCurve.Overall[g] = calcStatsFromTasks(tasks)
		}

		// curves + thresholds/ruleVersions（取 A 组作为基准提取规则序列）
		var aTasks []model.Task
		_ = db.DB.Where("run_id = ? AND group_type = ?", run.ID, "A").Find(&aTasks).Error
		_, ths, vers := service.ExtractRoundFlags(aTasks, rounds)
		modeCurve.Threshold = ths

		var cTasks []model.Task
		_ = db.DB.Where("run_id = ? AND group_type = ?", run.ID, "C").Find(&cTasks).Error
		cFlags, _, _ := service.ExtractRoundFlags(cTasks, rounds)
		modeCurve.TrialAndError = service.ComputeTrialAndErrorC(vers, cFlags)
		modeCurve.MemoryChangesPerRound = append([]int(nil), cFlags...)
		modeCurve.MemoryChangeStartRound = service.FirstErrorRound(cFlags)

		for _, g := range groups {
			var tasks []model.Task
			_ = db.DB.Where("run_id = ? AND group_type = ?", run.ID, g).Find(&tasks).Error
			flags, _, _ := service.ExtractRoundFlags(tasks, rounds)
			modeCurve.Curves[g] = service.BuildCumulativeCurves(flags, rounds)
			modeCurve.FirstErrorRound[g] = service.FirstErrorRound(flags)
		}

		resp[mode] = modeCurve
	}

	c.JSON(http.StatusOK, gin.H{
		"modes": resp,
	})
}

func calcStatsFromTasks(tasks []model.Task) service.GroupStats {
	gs := service.GroupStats{N: len(tasks)}
	for _, t := range tasks {
		if t.IsCorrect == nil {
			continue
		}
		if *t.IsCorrect {
			gs.Correct++
		} else {
			gs.Incorrect++
		}
	}
	judged := gs.Correct + gs.Incorrect
	if judged > 0 {
		gs.ErrorRate = float64(gs.Incorrect) / float64(judged)
	}
	return gs
}

// ResetAll 重置实验数据（清空 tasks/feedbacks/memories/task_logs/experiment_runs）
func (h *ExperimentHandler) ResetAll(c *gin.Context) {
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.TaskLog{}).Error; err != nil {
			return err
		}
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.Feedback{}).Error; err != nil {
			return err
		}
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.Task{}).Error; err != nil {
			return err
		}
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.Memory{}).Error; err != nil {
			return err
		}
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.ExperimentRun{}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已重置"})
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
