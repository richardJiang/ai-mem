package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mem-test/internal/db"
	"mem-test/internal/model"

	"gorm.io/gorm"
)

type AgentService struct {
	difyClient    *DifyClient
	memosClient   *MemOSClient
	memosUserPref string
}

func NewAgentService(difyClient *DifyClient, memosClient *MemOSClient, memosUserPref string) *AgentService {
	return &AgentService{
		difyClient:    difyClient,
		memosClient:   memosClient,
		memosUserPref: memosUserPref,
	}
}

// ExecuteTask 执行任务（带记忆检索）
func (s *AgentService) ExecuteTask(ctx context.Context, taskType, input string, groupType string, useMemory bool) (*model.Task, error) {
	return s.ExecuteTaskInRun(ctx, 0, taskType, input, groupType, useMemory)
}

// ExecuteTaskInRun 论文级实验：同一次 run 内严格隔离检索范围
func (s *AgentService) ExecuteTaskInRun(ctx context.Context, runID uint, taskType, input string, groupType string, useMemory bool) (*model.Task, error) {
	var relevantMemories []model.Memory
	var memoryIDs []string
	var logCases []model.Task

	// 如果使用记忆，检索相关记忆
	if useMemory {
		// B 组：检索历史“案例日志”，不做规则抽象
		if groupType == "B" {
			cases, err := s.retrieveTaskLogs(ctx, runID, taskType)
			if err == nil {
				logCases = cases
			}
		} else {
			// C 组：检索抽象规则记忆
			memories, err := s.retrieveMemories(ctx, runID, taskType, input)
			if err == nil {
				relevantMemories = memories
				var ids []uint
				for _, m := range memories {
					memoryIDs = append(memoryIDs, fmt.Sprintf("%d", m.ID))
					ids = append(ids, m.ID)
				}
				// 原子更新使用次数 + 最近使用时间（避免并发丢更新）
				if len(ids) > 0 {
					now := time.Now()
					_ = db.DB.WithContext(ctx).
						Model(&model.Memory{}).
						Where("id IN ?", ids).
						Updates(map[string]interface{}{
							"use_count":    gorm.Expr("use_count + 1"),
							"last_used_at": now,
						}).Error
				}
			}
		}
	}

	// 构建提示词
	prompt := s.buildPrompt(taskType, input, relevantMemories, logCases)

	// 调用Dify（智能选择chat或completion模式）
	var inputs map[string]interface{}
	if s.difyClient != nil && s.difyClient.AppType == "workflow" {
		systemKey := s.difyClient.WorkflowSystemKey
		queryKey := s.difyClient.WorkflowQueryKey
		if systemKey == "" {
			systemKey = "system"
		}
		if queryKey == "" {
			queryKey = "query"
		}
		inputs = map[string]interface{}{
			systemKey: prompt,
			queryKey:  input,
		}
	} else {
		inputs = map[string]interface{}{
			"task_type": taskType,
			"input":     input,
		}
	}

	resp, err := s.difyClient.ChatOrCompletion(prompt, inputs)
	if err != nil {
		return nil, fmt.Errorf("调用AI失败: %w", err)
	}

	// 保存任务记录
	task := &model.Task{
		RunID:      runID,
		TaskType:   taskType,
		Input:      input,
		Output:     resp.Answer,
		MemoryIDs:  strings.Join(memoryIDs, ","),
		TokenCount: resp.Metadata.Usage.TotalTokens,
		GroupType:  groupType,
	}

	if err := db.DB.Create(task).Error; err != nil {
		return nil, fmt.Errorf("保存任务失败: %w", err)
	}

	// 记录可检索日志（尤其用于 B 组）
	taskLog := &model.TaskLog{
		TaskID:       task.ID,
		RunID:        runID,
		TaskType:     taskType,
		GroupType:    groupType,
		SystemPrompt: prompt,
		QueryInput:   input,
		MemoryIDs:    strings.Join(memoryIDs, ","),
	}
	_ = db.DB.Create(taskLog).Error

	return task, nil
}

// retrieveMemories 检索相关记忆
func (s *AgentService) retrieveMemories(ctx context.Context, runID uint, taskType, input string) ([]model.Memory, error) {
	var memories []model.Memory

	// 简单关键词匹配（MVP版本）
	// 注意：trigger是MySQL保留关键字，需要用反引号包裹
	q := db.DB.WithContext(ctx).Model(&model.Memory{}).Where("deprecated = 0")
	if runID > 0 {
		q = q.Where("run_id = ?", runID)
	}
	query := q.
		Where("(apply_to = ? OR apply_to = ?)", taskType, "通用").
		// 排序核心：优先“最近被验证为正确”的规则，其次最新版本，再考虑置信度/使用次数
		Order("last_verified_at DESC, version DESC, confidence DESC, use_count DESC, updated_at DESC").
		Limit(5).
		Find(&memories)

	if query.Error != nil {
		return nil, query.Error
	}

	return memories, nil
}

func (s *AgentService) retrieveTaskLogs(ctx context.Context, runID uint, taskType string) ([]model.Task, error) {
	var tasks []model.Task
	// 简化：取 B 组最近的 3 条同类型、且已判定为正确的任务作为“案例”
	// 说明：如果把 incorrect/unknown 的案例喂回上下文，会引入强噪声，导致 B 组被系统性拖累，不利于公平对照。
	q := db.DB.Model(&model.Task{}).
		Where("task_type = ? AND group_type = ? AND is_correct = 1", taskType, "B")
	if runID > 0 {
		q = q.Where("run_id = ?", runID)
	}
	q = q.Order("created_at DESC").Limit(3).Find(&tasks)
	if q.Error != nil {
		return nil, q.Error
	}
	return tasks, nil
}

// buildPrompt 构建提示词（注入记忆/日志案例）
func (s *AgentService) buildPrompt(taskType, input string, memories []model.Memory, logs []model.Task) string {
	var prompt strings.Builder

	prompt.WriteString(fmt.Sprintf("任务类型: %s\n", taskType))
	prompt.WriteString(fmt.Sprintf("输入: %s\n\n", input))

	if len(logs) > 0 {
		prompt.WriteString("历史案例（均为已判定正确，可作为参考范式）:\n")
		for i, t := range logs {
			status := "unknown"
			if t.IsCorrect != nil {
				if *t.IsCorrect {
					status = "correct"
				} else {
					status = "incorrect"
				}
			}
			prompt.WriteString(fmt.Sprintf("%d) input=%s\n   output=%s\n   judge=%s\n", i+1, t.Input, t.Output, status))
		}
		prompt.WriteString("\n")
	}

	if len(memories) > 0 {
		prompt.WriteString("重要经验（请遵循）:\n")
		for i, m := range memories {
			prompt.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, m.Trigger, m.Lesson))
		}
		prompt.WriteString("\n")
	}

	// 根据任务类型添加具体指令
	switch taskType {
	case "lottery":
		prompt.WriteString("请判断用户是否可以抽奖，并给出原因。需要考虑积分是否充足。\n")
		prompt.WriteString("请只输出严格 JSON（不要 Markdown、不要多余文本）：\n")
		prompt.WriteString(`{"allow": true, "reason": "..."}` + "\n")
	case "lottery_multi":
		prompt.WriteString("请判断用户是否可以抽奖，并给出原因。\n")
		prompt.WriteString("注意：输入包含多种积分字段，可能只有部分积分可计入（规则未显式给出）。请根据输入做出判断。\n")
		prompt.WriteString("请只输出严格 JSON（不要 Markdown、不要多余文本）：\n")
		prompt.WriteString(`{"allow": true, "reason": "..."}` + "\n")
	case "lottery_v2":
		prompt.WriteString("请根据规则判断用户是否可以抽奖，并给出原因。\n")
		prompt.WriteString("规则提示：黑名单用户禁止；VIP门槛更低；每日抽奖次数达到上限则禁止。\n")
		prompt.WriteString("请只输出严格 JSON（不要 Markdown、不要多余文本）：\n")
		prompt.WriteString(`{"allow": true, "reason": "..."}` + "\n")
	default:
		prompt.WriteString("请根据输入完成任务。\n")
	}

	return prompt.String()
}
