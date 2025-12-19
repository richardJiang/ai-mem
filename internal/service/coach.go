package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"mem-test/internal/db"
	"mem-test/internal/model"
)

type CoachService struct {
}

func NewCoachService() *CoachService {
	return &CoachService{}
}

type lotteryAnswer struct {
	Allow  *bool  `json:"allow"`
	Reason string `json:"reason"`
}

func parseLotteryAllow(output string) (*bool, error) {
	var ans lotteryAnswer
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &ans); err != nil {
		return nil, err
	}
	return ans.Allow, nil
}

// SubmitFeedback 提交反馈（人工或规则引擎）
func (s *CoachService) SubmitFeedback(ctx context.Context, taskID uint, feedbackType, content string) (*model.Feedback, error) {
	// 取 run_id 以便论文级隔离
	var task model.Task
	_ = db.DB.Select("id", "run_id").First(&task, taskID).Error

	feedback := &model.Feedback{
		RunID:   task.RunID,
		TaskID:  taskID,
		Type:    feedbackType,
		Content: content,
	}

	if err := db.DB.Create(feedback).Error; err != nil {
		return nil, fmt.Errorf("保存反馈失败: %w", err)
	}

	return feedback, nil
}

// AutoJudge 自动判断（规则引擎）
func (s *CoachService) AutoJudge(ctx context.Context, task *model.Task, expectedOutput string) (*model.Feedback, error) {
	// 简单规则：比较输出是否匹配
	isCorrect := task.Output == expectedOutput

	// 更新任务正确性
	task.IsCorrect = &isCorrect
	db.DB.Save(task)

	var feedbackType string
	var content string

	if isCorrect {
		feedbackType = "correct"
		content = "回答正确"
	} else {
		feedbackType = "incorrect"
		content = fmt.Sprintf("回答错误。期望: %s, 实际: %s", expectedOutput, task.Output)
	}

	return s.SubmitFeedback(ctx, task.ID, feedbackType, content)
}

// JudgeLotteryTask 判断抽奖任务（示例规则引擎）
func (s *CoachService) JudgeLotteryTask(ctx context.Context, task *model.Task) (*model.Feedback, error) {
	return s.JudgeLotteryTaskWithThreshold(ctx, task, 100)
}

// JudgeLotteryTaskWithThreshold 支持动态门槛（用于规则变更实验）
func (s *CoachService) JudgeLotteryTaskWithThreshold(ctx context.Context, task *model.Task, threshold int) (*model.Feedback, error) {
	// 解析输入（简化版，实际应该用JSON）
	var inputData map[string]interface{}
	if err := json.Unmarshal([]byte(task.Input), &inputData); err != nil {
		// 如果不是JSON，尝试简单解析
		inputData = map[string]interface{}{
			"points": 0,
			"action": "lottery",
		}
	}

	points, ok := inputData["points"].(float64)
	if !ok {
		points = 0
	}

	var expectedOutput string
	var expectedAllow bool
	var isCorrect bool

	if int(points) < threshold {
		expectedOutput = fmt.Sprintf("积分不足，无法抽奖。当前积分不足%d，请先充值。", threshold)
		expectedAllow = false
	} else {
		expectedOutput = "可以抽奖。积分充足，允许进行抽奖操作。"
		expectedAllow = true
	}

	// 严格判题：要求模型输出 JSON {"allow": bool, "reason": string}
	// 这样可以避免通过关键词投机命中（比如乱写“可以/不可以”都包含关键字）。
	allow, err := parseLotteryAllow(task.Output)
	if err != nil {
		isCorrect = false
	} else if allow == nil {
		isCorrect = false
	} else {
		isCorrect = (*allow == expectedAllow)
	}

	task.IsCorrect = &isCorrect
	db.DB.Save(task)

	var feedbackType string
	var content string

	if isCorrect {
		feedbackType = "correct"
		content = "判断正确"
	} else {
		feedbackType = "incorrect"
		content = fmt.Sprintf("判断错误。积分=%v时，门槛=%d，应该: %s", points, threshold, expectedOutput)
	}

	return s.SubmitFeedback(ctx, task.ID, feedbackType, content)
}

// JudgeLotteryMultiPointsTask 生产模拟：多积分字段 + 多条隐性规则（不在 prompt 中显式给出）
func (s *CoachService) JudgeLotteryMultiPointsTask(ctx context.Context, task *model.Task) (*model.Feedback, error) {
	return s.JudgeLotteryMultiPointsTaskWithThreshold(ctx, task, 100)
}

// JudgeLotteryMultiPointsTaskWithThreshold 支持动态门槛（用于规则变更实验）
func (s *CoachService) JudgeLotteryMultiPointsTaskWithThreshold(ctx context.Context, task *model.Task, threshold int) (*model.Feedback, error) {
	var inputData map[string]interface{}
	if err := json.Unmarshal([]byte(task.Input), &inputData); err != nil {
		inputData = map[string]interface{}{}
	}

	available := int(getFloat(inputData, "points_available"))
	bonus := int(getFloat(inputData, "points_bonus"))
	locked := int(getFloat(inputData, "points_locked"))
	expiring := int(getFloat(inputData, "points_expiring"))
	expDays := int(getFloat(inputData, "expiring_days"))
	penalty := int(getFloat(inputData, "points_penalty"))

	effective, explain := computeEffectivePoints(threshold, available, bonus, locked, expiring, expDays, penalty)
	expectedAllow := effective >= threshold

	allow, err := parseLotteryAllow(task.Output)
	isCorrect := false
	if err == nil && allow != nil {
		isCorrect = (*allow == expectedAllow)
	}

	task.IsCorrect = &isCorrect
	db.DB.Save(task)

	feedbackType := "correct"
	content := "判断正确"
	if !isCorrect {
		feedbackType = "incorrect"
		content = fmt.Sprintf(
			"判断错误。门槛=%d，有效积分=%d（%s）。输入: available=%d bonus=%d locked=%d expiring=%d expiring_days=%d penalty=%d，应该: allow=%v",
			threshold, effective, explain, available, bonus, locked, expiring, expDays, penalty, expectedAllow,
		)
	}

	return s.SubmitFeedback(ctx, task.ID, feedbackType, content)
}

func getFloat(m map[string]interface{}, key string) float64 {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch vv := v.(type) {
	case float64:
		return vv
	case int:
		return float64(vv)
	case int64:
		return float64(vv)
	case string:
		// 简化：不解析字符串
		return 0
	default:
		return 0
	}
}

// computeEffectivePoints 多条隐性规则（示例，可按你的生产规则继续加复杂度）
//
// 隐性规则：
// 1) locked 不计入
// 2) bonus 仅按 50% 折算，且最多计入门槛的 20%
// 3) expiring 仅在 expiring_days<=1 时计入（按 100%）
// 4) penalty 直接扣减
// 5) effective 最小为 0
func computeEffectivePoints(threshold, available, bonus, locked, expiring, expDays, penalty int) (effective int, explain string) {
	if threshold <= 0 {
		threshold = 100
	}
	if available < 0 {
		available = 0
	}
	if bonus < 0 {
		bonus = 0
	}
	if locked < 0 {
		locked = 0
	}
	if expiring < 0 {
		expiring = 0
	}
	if penalty < 0 {
		penalty = 0
	}

	// bonus 折算 + cap
	bonusEff := bonus / 2
	cap := int(float64(threshold) * 0.2)
	if bonusEff > cap {
		bonusEff = cap
	}

	expEff := 0
	if expDays <= 1 {
		expEff = expiring
	}

	// locked 不计入，只用于 explain
	effective = available + bonusEff + expEff - penalty
	if effective < 0 {
		effective = 0
	}

	explain = fmt.Sprintf("available(%d)+bonus50%%cap(%d)+expiring(%d)-penalty(%d), locked(%d)不计", available, bonusEff, expEff, penalty, locked)
	return effective, explain
}

// JudgeLotteryV2Task 更复杂的抽奖任务判题（多规则）
//
// 输入示例（JSON）：
//
//	{
//	  "points": 90,
//	  "action": "lottery",
//	  "is_vip": true,
//	  "is_blacklisted": false,
//	  "daily_draws": 0
//	}
//
// 规则（v2）：
// - 黑名单：禁止
// - 门槛：VIP>=80，非VIP>=100
// - 每日次数：daily_draws>=1 禁止
func (s *CoachService) JudgeLotteryV2Task(ctx context.Context, task *model.Task) (*model.Feedback, error) {
	var inputData map[string]interface{}
	if err := json.Unmarshal([]byte(task.Input), &inputData); err != nil {
		inputData = map[string]interface{}{}
	}

	points, _ := inputData["points"].(float64)
	isVip, _ := inputData["is_vip"].(bool)
	isBlacklisted, _ := inputData["is_blacklisted"].(bool)
	dailyDrawsF, _ := inputData["daily_draws"].(float64)
	dailyDraws := int(dailyDrawsF)

	threshold := 100
	if isVip {
		threshold = 80
	}

	expectedAllow := true
	expectedReason := "满足规则，允许抽奖"
	if isBlacklisted {
		expectedAllow = false
		expectedReason = "黑名单用户禁止抽奖"
	} else if dailyDraws >= 1 {
		expectedAllow = false
		expectedReason = "已达到每日抽奖次数上限"
	} else if int(points) < threshold {
		expectedAllow = false
		expectedReason = fmt.Sprintf("积分不足，VIP门槛=%d", threshold)
		if !isVip {
			expectedReason = fmt.Sprintf("积分不足，门槛=%d", threshold)
		}
	}

	allow, err := parseLotteryAllow(task.Output)
	isCorrect := false
	if err == nil && allow != nil {
		isCorrect = (*allow == expectedAllow)
	}

	task.IsCorrect = &isCorrect
	db.DB.Save(task)

	feedbackType := "correct"
	content := "判断正确"
	if !isCorrect {
		feedbackType = "incorrect"
		content = fmt.Sprintf("判断错误。points=%v is_vip=%v is_blacklisted=%v daily_draws=%d 时，应该: allow=%v（%s）",
			points, isVip, isBlacklisted, dailyDraws, expectedAllow, expectedReason)
	}

	return s.SubmitFeedback(ctx, task.ID, feedbackType, content)
}
