package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
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

type memoryScope int

const (
	memoryScopeRunOnly memoryScope = iota
	memoryScopeRunAndGlobal
)

func NewAgentService(difyClient *DifyClient, memosClient *MemOSClient, memosUserPref string) *AgentService {
	return &AgentService{
		difyClient:    difyClient,
		memosClient:   memosClient,
		memosUserPref: memosUserPref,
	}
}

const (
	// memosFallbackMinLocalHits：本地命中少于该值，且置信度较低时，认为“很弱”
	memosFallbackMinLocalHits  = 2
	memosFallbackLowConfidence = 0.65
)

// ExecuteTask 执行任务（带记忆检索）
func (s *AgentService) ExecuteTask(ctx context.Context, taskType, input string, groupType string, useMemory bool) (*model.Task, error) {
	return s.ExecuteTaskInRun(ctx, 0, taskType, input, groupType, useMemory)
}

// ExecuteTaskInRun 论文级实验：同一次 run 内严格隔离检索范围
func (s *AgentService) ExecuteTaskInRun(ctx context.Context, runID uint, taskType, input string, groupType string, useMemory bool) (*model.Task, error) {
	var relevantMemories []model.Memory
	var memoryIDs []string
	var logCases []model.Task
	var externalMemories []MemOSSearchItem
	var recentIncorrectFeedbacks []model.Feedback
	inputFeatures := extractInputFeatures(taskType, input)

	// 如果使用记忆，检索相关记忆
	if useMemory {
		// B 组：检索历史“案例日志”，不做规则抽象
		if groupType == "B" {
			cases, err := s.retrieveTaskLogs(ctx, runID, taskType)
			if err == nil {
				logCases = cases
			}
		} else {
			scope := memoryScopeRunOnly
			if groupType == "D" || groupType == "E" {
				// D 组：跨 run 复用“全局中期记忆池”（run_id=0 且 global 前缀）+ 当前 run 内的新记忆
				scope = memoryScopeRunAndGlobal
				if runID > 0 {
					limit := 2
					if groupType == "E" {
						limit = 3
					}
					if rec, err := s.retrieveRecentIncorrectFeedbacks(ctx, runID, taskType, limit); err == nil {
						recentIncorrectFeedbacks = rec
					}
				}
			}
			// C/D 组：检索抽象规则记忆
			memories, err := s.retrieveMemories(ctx, runID, taskType, input, scope)
			if err == nil {
				// E 组：按输入相关性重排（优先阈值更接近当前 points 的规则，减少无关规则污染）
				if groupType == "E" {
					memories = rerankMemoriesByInput(taskType, inputFeatures, memories)
				}
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

			// MemOS 外部长期记忆检索：
			// - 常规模式（run_id=0）：本地记忆为空/很弱时补充
			// - 实验 D 组（run_id>0 且 group=D）：允许使用 MemOS（用户显式要求 D 组使用 MemOS）
			useMemOS := (runID == 0) || (groupType == "D") || (groupType == "E")
			if useMemOS && s.shouldFallbackToMemOS(relevantMemories) && s.memosClient != nil && s.memosClient.Enabled() {
				userID := s.memOSUserID(taskType)
				// 尝试注册（多数实现会幂等；失败不影响主流程）
				if err := s.memosClient.RegisterUser(ctx, userID); err != nil {
					log.Printf("[memos] register user failed user=%s err=%v", userID, err)
				}
				hits, err := s.memosClient.Search(ctx, userID, s.memOSQuery(taskType, input))
				if err != nil {
					log.Printf("[memos] search failed user=%s err=%v", userID, err)
				} else {
					externalMemories = hits
				}
			}
		}
	}

	// 构建提示词（E 组采用两阶段：先答题、再自检纠错）
	prompt := s.buildPrompt(taskType, input, relevantMemories, logCases, externalMemories, recentIncorrectFeedbacks)

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

	answer := resp.Answer
	tokenCount := resp.Metadata.Usage.TotalTokens

	if groupType == "E" {
		// E 阶段2：自检纠错（把 stage1 的答案、严格规则、以及 MemOS 候选记忆一起给模型做一致性校验）
		checkPrompt := s.buildECheckPrompt(taskType, input, relevantMemories, recentIncorrectFeedbacks, externalMemories, answer)
		checkInputs := inputs
		checkResp, err := s.difyClient.ChatOrCompletion(checkPrompt, checkInputs)
		if err == nil && strings.TrimSpace(checkResp.Answer) != "" {
			answer = checkResp.Answer
			tokenCount += checkResp.Metadata.Usage.TotalTokens
			prompt = checkPrompt
		}
	}

	// 保存任务记录
	task := &model.Task{
		RunID:      runID,
		TaskType:   taskType,
		Input:      input,
		Output:     answer,
		MemoryIDs:  strings.Join(memoryIDs, ","),
		TokenCount: tokenCount,
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
func (s *AgentService) retrieveMemories(ctx context.Context, runID uint, taskType, input string, scope memoryScope) ([]model.Memory, error) {
	var memories []model.Memory

	// 简单关键词匹配（MVP版本）
	// 注意：trigger是MySQL保留关键字，需要用反引号包裹
	q := db.DB.WithContext(ctx).Model(&model.Memory{}).Where("deprecated = 0")
	if runID > 0 {
		switch scope {
		case memoryScopeRunAndGlobal:
			// global 记忆池：run_id=0 且 derived_from 带 global 前缀，避免把普通 run_id=0 任务的零散记忆混入实验
			q = q.Where("(run_id = ? OR (run_id = 0 AND derived_from LIKE ?))", runID, "global|%")
		default:
			// 论文级实验：严格 run 隔离
			q = q.Where("run_id = ?", runID)
		}
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

func (s *AgentService) retrieveRecentIncorrectFeedbacks(ctx context.Context, runID uint, taskType string, limit int) ([]model.Feedback, error) {
	if runID == 0 || limit <= 0 {
		return nil, nil
	}
	var feedbacks []model.Feedback
	q := db.DB.WithContext(ctx).
		Model(&model.Feedback{}).
		Joins("JOIN tasks ON tasks.id = feedbacks.task_id").
		Where("feedbacks.run_id = ? AND tasks.task_type = ? AND feedbacks.type = ?", runID, taskType, "incorrect").
		Order("feedbacks.id DESC").
		Limit(limit).
		Find(&feedbacks)
	if q.Error != nil {
		return nil, q.Error
	}
	// 让 prompt 里按时间正序展示
	for i, j := 0, len(feedbacks)-1; i < j; i, j = i+1, j-1 {
		feedbacks[i], feedbacks[j] = feedbacks[j], feedbacks[i]
	}
	return feedbacks, nil
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

func (s *AgentService) shouldFallbackToMemOS(local []model.Memory) bool {
	if len(local) == 0 {
		return true
	}
	// “很弱”：命中极少且最高置信度偏低
	if len(local) < memosFallbackMinLocalHits {
		maxConf := 0.0
		for _, m := range local {
			if m.Confidence > maxConf {
				maxConf = m.Confidence
			}
		}
		if maxConf < memosFallbackLowConfidence {
			return true
		}
	}
	return false
}

func (s *AgentService) memOSUserID(taskType string) string {
	p := strings.TrimSpace(s.memosUserPref)
	if p == "" {
		p = "mem-test"
	}
	tt := strings.TrimSpace(taskType)
	if tt == "" {
		tt = "unknown"
	}
	// 以 taskType 做隔离，避免不同任务域的记忆互相污染
	return fmt.Sprintf("%s:%s", p, tt)
}

func (s *AgentService) memOSQuery(taskType, input string) string {
	// 尽量让 MemOS 侧有可检索的关键词，同时避免超长 query
	q := fmt.Sprintf("task_type=%s input=%s", strings.TrimSpace(taskType), strings.TrimSpace(input))
	if len(q) > 800 {
		q = q[:800]
	}
	return q
}

// buildPrompt 构建提示词（注入记忆/日志案例/外部长期记忆）
func (s *AgentService) buildPrompt(taskType, input string, memories []model.Memory, logs []model.Task, external []MemOSSearchItem, recentIncorrect []model.Feedback) string {
	var prompt strings.Builder

	prompt.WriteString(fmt.Sprintf("任务类型: %s\n", taskType))
	prompt.WriteString(fmt.Sprintf("输入: %s\n\n", input))

	if len(recentIncorrect) > 0 {
		prompt.WriteString("短期记忆（近期错误与纠正信号；优先避免重复犯错）:\n")
		for i, f := range recentIncorrect {
			prompt.WriteString(fmt.Sprintf("%d) %s\n", i+1, strings.TrimSpace(f.Content)))
		}
		prompt.WriteString("\n")
	}

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

	if len(external) > 0 {
		prompt.WriteString("外部长期记忆（仅供参考；可能与当前规则不一致，请谨慎）:\n")
		for i, it := range external {
			// score 可能为 0（部分版本不返回），不强制展示
			if it.Score > 0 {
				prompt.WriteString(fmt.Sprintf("%d. (score=%.4f) %s\n", i+1, it.Score, it.Content))
			} else {
				prompt.WriteString(fmt.Sprintf("%d. %s\n", i+1, it.Content))
			}
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

func (s *AgentService) buildECheckPrompt(taskType, input string, memories []model.Memory, recentIncorrect []model.Feedback, external []MemOSSearchItem, stage1Answer string) string {
	var b strings.Builder
	b.WriteString("你是一个严格的审校器。你将看到：输入、已验证规则（必须遵循）、近期错误信号、候选外部记忆、以及模型的初稿输出。\n")
	b.WriteString("你的任务：检查初稿是否与已验证规则矛盾；如有矛盾必须修正。最终只输出严格 JSON，不要解释。\n\n")
	b.WriteString(fmt.Sprintf("任务类型: %s\n", taskType))
	b.WriteString(fmt.Sprintf("输入: %s\n\n", input))
	if len(recentIncorrect) > 0 {
		b.WriteString("近期错误信号（避免重复错误）:\n")
		for i, f := range recentIncorrect {
			b.WriteString(fmt.Sprintf("%d) %s\n", i+1, strings.TrimSpace(f.Content)))
		}
		b.WriteString("\n")
	}
	if len(memories) > 0 {
		b.WriteString("已验证规则（必须遵循）:\n")
		for i, m := range memories {
			b.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, m.Trigger, m.Lesson))
		}
		b.WriteString("\n")
	}
	if len(external) > 0 {
		b.WriteString("候选外部记忆（可能包含噪声；只有在与已验证规则不冲突时才可参考）:\n")
		for i, it := range external {
			if it.Score > 0 {
				b.WriteString(fmt.Sprintf("%d. (score=%.4f) %s\n", i+1, it.Score, it.Content))
			} else {
				b.WriteString(fmt.Sprintf("%d. %s\n", i+1, it.Content))
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("初稿输出（待审校）:\n")
	b.WriteString(strings.TrimSpace(stage1Answer))
	b.WriteString("\n\n")
	b.WriteString("请输出最终答案（严格 JSON）：\n")
	b.WriteString(`{"allow": true, "reason": "..."}` + "\n")
	return b.String()
}

type inputFeatures struct {
	points *float64
}

func extractInputFeatures(taskType, input string) inputFeatures {
	if strings.TrimSpace(input) == "" {
		return inputFeatures{}
	}
	switch taskType {
	case "lottery", "lottery_v2":
		var m map[string]any
		if err := json.Unmarshal([]byte(input), &m); err == nil {
			if v, ok := m["points"]; ok {
				if f, ok := v.(float64); ok {
					return inputFeatures{points: &f}
				}
			}
		}
	}
	return inputFeatures{}
}

func rerankMemoriesByInput(taskType string, f inputFeatures, memories []model.Memory) []model.Memory {
	if len(memories) <= 1 || f.points == nil {
		return memories
	}
	type scored struct {
		m     model.Memory
		score float64
	}
	out := make([]scored, 0, len(memories))
	for _, m := range memories {
		t, ok := extractThresholdFromText(m.Trigger + " " + m.Lesson)
		s := 0.0
		if ok {
			// 越接近当前 points 的阈值越相关（避免把与当前输入无关的旧门槛硬塞进上下文）
			s = 1.0 / (1.0 + math.Abs(float64(t)-*f.points))
		}
		out = append(out, scored{m: m, score: s})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].score > out[j].score
	})
	res := make([]model.Memory, 0, len(out))
	for _, it := range out {
		res = append(res, it.m)
	}
	return res
}
