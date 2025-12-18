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

	if points < 100 {
		expectedOutput = "积分不足，无法抽奖。当前积分不足100，请先充值。"
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
		content = fmt.Sprintf("判断错误。积分=%v时，应该: %s", points, expectedOutput)
	}

	return s.SubmitFeedback(ctx, task.ID, feedbackType, content)
}

// JudgeLotteryV2Task 更复杂的抽奖任务判题（多规则）
//
// 输入示例（JSON）：
// {
//   "points": 90,
//   "action": "lottery",
//   "is_vip": true,
//   "is_blacklisted": false,
//   "daily_draws": 0
// }
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
