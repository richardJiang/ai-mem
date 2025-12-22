package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"mem-test/internal/db"
	"mem-test/internal/model"

	"gorm.io/gorm"
)

type ExperimentRunRequest struct {
	TaskType     string   `json:"task_type"`
	RunsPerGroup int      `json:"runs_per_group"`
	Seed         int64    `json:"seed"`
	Groups       []string `json:"groups"`
	Action       string   `json:"action"`
	// 规则变更模式：none/low/high
	RuleMode string `json:"rule_mode"`
}

type ExperimentRunResult struct {
	RunID              uint                   `json:"run_id"`
	Seed               int64                  `json:"seed"`
	TaskType           string                 `json:"task_type"`
	RunsPerGroup       int                    `json:"runs_per_group"`
	Groups             []string               `json:"groups"`
	Stats              map[string]GroupStats  `json:"stats"`
	Tests              map[string]interface{} `json:"tests"`
	Trend              map[string][]int       `json:"trend"` // group -> incorrect flags by round
	Conclusion         map[string]interface{} `json:"conclusion"`
	ResultPath         string                 `json:"result_path"`
	ConclusionPath     string                 `json:"conclusion_path"`
	ConclusionMarkdown string                 `json:"conclusion_markdown"`
	Errors             []string               `json:"errors"`
}

type ExperimentRunner struct {
	agent      *AgentService
	coach      *CoachService
	reflection *ReflectionService
}

func NewExperimentRunner(agent *AgentService, coach *CoachService, reflection *ReflectionService) *ExperimentRunner {
	return &ExperimentRunner{
		agent:      agent,
		coach:      coach,
		reflection: reflection,
	}
}

func (r *ExperimentRunner) Run(ctx context.Context, req ExperimentRunRequest) (*ExperimentRunResult, error) {
	if req.TaskType == "" {
		req.TaskType = "lottery"
	}
	if req.Action == "" {
		req.Action = "lottery"
	}
	if req.RunsPerGroup <= 0 {
		req.RunsPerGroup = 30
	}
	if req.Seed == 0 {
		req.Seed = time.Now().UnixNano()
	}
	if len(req.Groups) == 0 {
		req.Groups = []string{"A", "B", "C", "D", "E", "F"}
	}
	if req.RuleMode == "" {
		req.RuleMode = "none"
	}

	groupsJSON, _ := json.Marshal(req.Groups)
	run := &model.ExperimentRun{
		TaskType:     req.TaskType,
		RunsPerGroup: req.RunsPerGroup,
		Seed:         req.Seed,
		GroupsJSON:   string(groupsJSON),
		RuleMode:     req.RuleMode,
	}
	if err := db.DB.Create(run).Error; err != nil {
		return nil, fmt.Errorf("创建实验run失败: %w", err)
	}

	var lotteryInputs []map[string]interface{}
	var thresholds []int
	var ruleVersions []int
	switch req.TaskType {
	case "lottery_v2":
		lotteryInputs = buildLotteryV2Inputs(req.RunsPerGroup, req.Seed, req.Action)
	case "lottery_multi":
		lotteryInputs = buildLotteryMultiPointsInputs(req.RunsPerGroup, req.Seed, req.Action)
		thresholds, ruleVersions = buildLotteryThresholdSchedule(req.RunsPerGroup, req.RuleMode)
	default:
		pointsSeq := buildLotteryPoints(req.RunsPerGroup, req.Seed)
		lotteryInputs = make([]map[string]interface{}, 0, len(pointsSeq))
		for i := range pointsSeq {
			lotteryInputs = append(lotteryInputs, map[string]interface{}{
				"points": pointsSeq[i],
				"action": req.Action,
			})
		}
		thresholds, ruleVersions = buildLotteryThresholdSchedule(req.RunsPerGroup, req.RuleMode)
	}

	result := &ExperimentRunResult{
		RunID:        run.ID,
		Seed:         req.Seed,
		TaskType:     req.TaskType,
		RunsPerGroup: req.RunsPerGroup,
		Groups:       req.Groups,
		Stats:        map[string]GroupStats{},
		Tests:        map[string]interface{}{},
		Trend:        map[string][]int{},
		Conclusion:   map[string]interface{}{},
	}

	for _, g := range req.Groups {
		result.Trend[g] = make([]int, 0, req.RunsPerGroup)
	}

	// 论文级：按轮次交错运行，尽量消除模型/环境随时间漂移的干扰
	for i := 0; i < req.RunsPerGroup; i++ {
		in := lotteryInputs[i]
		inputJSON, _ := json.Marshal(in)
		inputStr := string(inputJSON)
		threshold := 0
		ruleVersion := 1
		if len(thresholds) > 0 {
			threshold = thresholds[i]
		}
		if len(ruleVersions) > 0 {
			ruleVersion = ruleVersions[i]
		}

		for _, group := range req.Groups {
			useMemory := group != "A"
			if group == "F" {
				// F 组：执行前设置当前 round，便于短期封禁/探索期生效
				r.agent.SetFCurrentRound(run.ID, req.TaskType, i)
			}
			task, err := r.agent.ExecuteTaskInRun(ctx, run.ID, req.TaskType, inputStr, group, useMemory)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("run=%d group=%s round=%d execute failed: %v", run.ID, group, i, err))
				result.Trend[group] = append(result.Trend[group], 1)
				continue
			}

			var feedback *model.Feedback
			switch req.TaskType {
			case "lottery_v2":
				feedback, err = r.coach.JudgeLotteryV2Task(ctx, task)
			case "lottery_multi":
				if threshold > 0 {
					feedback, err = r.coach.JudgeLotteryMultiPointsTaskWithThreshold(ctx, task, threshold)
				} else {
					feedback, err = r.coach.JudgeLotteryMultiPointsTask(ctx, task)
				}
			default:
				// 规则变更模式下：按当前轮次门槛判题，并把门槛写进反馈，便于记忆演化
				if threshold > 0 {
					feedback, err = r.coach.JudgeLotteryTaskWithThreshold(ctx, task, threshold)
				} else {
					feedback, err = r.coach.JudgeLotteryTask(ctx, task)
				}
			}
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("run=%d group=%s round=%d judge failed: %v", run.ID, group, i, err))
				result.Trend[group] = append(result.Trend[group], 1)
				continue
			}

			// 记录实验元数据（round / rule_mode / rule_version / threshold）
			_ = db.DB.WithContext(ctx).Model(&model.Task{}).
				Where("id = ?", task.ID).
				Updates(map[string]interface{}{
					"round":          i,
					"rule_mode":      req.RuleMode,
					"rule_version":   ruleVersion,
					"rule_threshold": threshold,
				}).Error

			// F 组：判题后更新“变更检测/epoch/bandit”状态（不依赖反思）
			if group == "F" {
				r.agent.UpdateFStateAfterJudge(ctx, run.ID, req.TaskType, task, feedback, i)
			}

			if feedback.Type == "incorrect" {
				result.Trend[group] = append(result.Trend[group], 1)
				if group == "C" {
					if _, err := r.reflection.ReflectAndSaveMemory(ctx, task.ID, feedback); err != nil {
						result.Errors = append(result.Errors, fmt.Sprintf("run=%d group=%s round=%d reflect failed: %v", run.ID, group, i, err))
					}
				}
				if group == "D" {
					if _, err := r.reflection.ReflectAndSaveMemoryAndConsolidateGlobal(ctx, task.ID, feedback); err != nil {
						result.Errors = append(result.Errors, fmt.Sprintf("run=%d group=%s round=%d reflect+consolidate failed: %v", run.ID, group, i, err))
					}
				}
				if group == "E" {
					if _, err := r.reflection.ReflectAndSaveMemoryAndConsolidateGlobalValidated(ctx, task.ID, feedback); err != nil {
						result.Errors = append(result.Errors, fmt.Sprintf("run=%d group=%s round=%d reflect+validate+consolidate failed: %v", run.ID, group, i, err))
					}
				}
				if group == "F" {
					// F 组：仍然用“验证固化”产出高质量记忆，但策略上更强调变更检测+候选竞争
					if _, err := r.reflection.ReflectAndSaveMemoryAndConsolidateGlobalValidated(ctx, task.ID, feedback); err != nil {
						result.Errors = append(result.Errors, fmt.Sprintf("run=%d group=%s round=%d reflect+validate+consolidate failed: %v", run.ID, group, i, err))
					}
				}
			} else {
				// 判对：对本次使用到的记忆做“验证时间”更新，帮助规则变更场景下优先检索当前有效规则
				if (group == "C" || group == "D" || group == "E" || group == "F") && task.MemoryIDs != "" {
					ids := ParseMemoryIDs(task.MemoryIDs)
					if len(ids) > 0 {
						now := time.Now()
						_ = db.DB.WithContext(ctx).
							Model(&model.Memory{}).
							Where("id IN ?", ids).
							Updates(map[string]interface{}{
								"last_verified_at": now,
								"confidence":       gorm.Expr("LEAST(confidence + ?, 1)", 0.01),
							}).Error
					}
				}
				result.Trend[group] = append(result.Trend[group], 0)
			}
		}
	}

	// 严谨统计：只统计本 run_id
	stats, tests, err := ComputeRunStatsAndTests(run.ID, req.Groups, result.Trend)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("run=%d stats failed: %v", run.ID, err))
	}
	result.Stats = stats
	result.Tests = tests
	result.Conclusion = GenerateConclusionFromStats(stats, tests, result.Trend)

	// 输出文件
	outDir := filepath.Join("outputs")
	_ = os.MkdirAll(outDir, 0o755)
	resultPath := filepath.Join(outDir, fmt.Sprintf("experiment_run_%d.json", run.ID))
	conclusionPath := filepath.Join(outDir, fmt.Sprintf("experiment_run_%d_conclusion.md", run.ID))
	result.ResultPath = resultPath
	result.ConclusionPath = conclusionPath
	result.ConclusionMarkdown = RenderConclusionMarkdown(run, result)

	if b, err := json.MarshalIndent(result, "", "  "); err == nil {
		_ = os.WriteFile(resultPath, b, 0o644)
	}
	_ = os.WriteFile(conclusionPath, []byte(result.ConclusionMarkdown), 0o644)

	run.ResultPath = resultPath
	run.ConclusionPath = conclusionPath
	_ = db.DB.Save(run).Error

	return result, nil
}

func buildLotteryThresholdSchedule(n int, mode string) (thresholds []int, versions []int) {
	thresholds = make([]int, n)
	versions = make([]int, n)

	base := 100
	alt := 120
	if mode == "" {
		mode = "none"
	}

	version := 1
	prev := 0
	// 低频：1 次变更；高频：变更次数是低频的 5 倍 => 5 次变更（不要每轮都变）
	highChanges := 5
	highSegment := 1
	if mode == "high" && n > 0 {
		// 5 次变更意味着 6 段，按段切换门槛
		highSegment = n / (highChanges + 1)
		if highSegment < 1 {
			highSegment = 1
		}
	}
	for i := 0; i < n; i++ {
		t := base
		switch mode {
		case "high":
			// 高频：每段切换一次，整段内保持稳定（避免“每轮都变”）
			// 变更次数约为 5 次（n 足够大时），即 low 的 5 倍
			if ((i / highSegment) % 2) == 1 {
				t = alt
			}
		case "low":
			// 低频：中点切换一次
			if i >= n/2 {
				t = alt
			}
		default:
			t = base
		}

		if i == 0 {
			prev = t
		} else if t != prev {
			version++
			prev = t
		}

		thresholds[i] = t
		versions[i] = version
	}
	return thresholds, versions
}

func buildLotteryPoints(n int, seed int64) []int {
	base := []int{0, 1, 10, 50, 99, 100, 101, 150, 200}
	out := make([]int, 0, n)
	// 先塞边界用例
	for len(out) < n && len(out) < len(base) {
		out = append(out, base[len(out)])
	}
	// 再补随机点（0~200）
	rng := rand.New(rand.NewSource(seed))
	for len(out) < n {
		out = append(out, rng.Intn(201))
	}
	return out
}

func buildLotteryV2Inputs(n int, seed int64, action string) []map[string]interface{} {
	if action == "" {
		action = "lottery"
	}
	// 先覆盖边界与组合用例，再补随机
	base := []map[string]interface{}{
		{"points": 0, "action": action, "is_vip": false, "is_blacklisted": false, "daily_draws": 0},
		{"points": 79, "action": action, "is_vip": true, "is_blacklisted": false, "daily_draws": 0},
		{"points": 80, "action": action, "is_vip": true, "is_blacklisted": false, "daily_draws": 0},
		{"points": 99, "action": action, "is_vip": false, "is_blacklisted": false, "daily_draws": 0},
		{"points": 100, "action": action, "is_vip": false, "is_blacklisted": false, "daily_draws": 0},
		{"points": 150, "action": action, "is_vip": false, "is_blacklisted": false, "daily_draws": 1}, // 次数限制
		{"points": 200, "action": action, "is_vip": true, "is_blacklisted": true, "daily_draws": 0},   // 黑名单
	}

	out := make([]map[string]interface{}, 0, n)
	for len(out) < n && len(out) < len(base) {
		out = append(out, base[len(out)])
	}

	rng := rand.New(rand.NewSource(seed))
	for len(out) < n {
		points := rng.Intn(201)
		isVip := rng.Intn(2) == 0
		isBlacklisted := rng.Intn(10) == 0 // 10% 黑名单
		dailyDraws := 0
		if rng.Intn(3) == 0 {
			dailyDraws = 1 // 约 1/3 达到上限
		}
		out = append(out, map[string]interface{}{
			"points":         points,
			"action":         action,
			"is_vip":         isVip,
			"is_blacklisted": isBlacklisted,
			"daily_draws":    dailyDraws,
		})
	}
	return out
}

func buildLotteryMultiPointsInputs(n int, seed int64, action string) []map[string]interface{} {
	if action == "" {
		action = "lottery"
	}
	// 覆盖边界与组合用例：让“可用/奖励/锁定/即将过期/惩罚”都出现
	base := []map[string]interface{}{
		{"action": action, "points_available": 0, "points_bonus": 0, "points_locked": 0, "points_expiring": 0, "expiring_days": 1, "points_penalty": 0},
		{"action": action, "points_available": 60, "points_bonus": 80, "points_locked": 0, "points_expiring": 0, "expiring_days": 7, "points_penalty": 0},  // 奖励多但可能不全计入
		{"action": action, "points_available": 90, "points_bonus": 30, "points_locked": 50, "points_expiring": 0, "expiring_days": 3, "points_penalty": 0}, // 锁定不计入
		{"action": action, "points_available": 85, "points_bonus": 10, "points_locked": 0, "points_expiring": 20, "expiring_days": 1, "points_penalty": 0}, // 即将过期可能加成
		{"action": action, "points_available": 105, "points_bonus": 0, "points_locked": 0, "points_expiring": 0, "expiring_days": 2, "points_penalty": 10}, // 惩罚扣减
		{"action": action, "points_available": 99, "points_bonus": 50, "points_locked": 0, "points_expiring": 5, "expiring_days": 1, "points_penalty": 0},  // 边界附近
	}

	out := make([]map[string]interface{}, 0, n)
	for len(out) < n && len(out) < len(base) {
		out = append(out, base[len(out)])
	}

	rng := rand.New(rand.NewSource(seed))
	for len(out) < n {
		available := rng.Intn(151) // 0~150
		bonus := rng.Intn(201)     // 0~200
		locked := rng.Intn(101)    // 0~100
		expiring := rng.Intn(51)   // 0~50
		expDays := 1
		if rng.Intn(3) != 0 {
			expDays = rng.Intn(14) + 2 // 2~15
		}
		penalty := 0
		if rng.Intn(5) == 0 {
			penalty = rng.Intn(21) // 0~20
		}
		out = append(out, map[string]interface{}{
			"action":           action,
			"points_available": available,
			"points_bonus":     bonus,
			"points_locked":    locked,
			"points_expiring":  expiring,
			"expiring_days":    expDays,
			"points_penalty":   penalty,
		})
	}
	return out
}
