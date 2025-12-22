package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"mem-test/internal/db"
	"mem-test/internal/model"

	"gorm.io/gorm"
)

type ReflectionService struct {
	difyClient    *DifyClient
	memosClient   *MemOSClient
	memosUserPref string
}

func NewReflectionService(difyClient *DifyClient, memosClient *MemOSClient, memosUserPref string) *ReflectionService {
	return &ReflectionService{
		difyClient:    difyClient,
		memosClient:   memosClient,
		memosUserPref: memosUserPref,
	}
}

// ReflectAndSaveMemory 反思并保存记忆
func (s *ReflectionService) ReflectAndSaveMemory(ctx context.Context, taskID uint, feedback *model.Feedback) (*model.Memory, error) {
	// 获取任务信息
	var task model.Task
	if err := db.DB.First(&task, taskID).Error; err != nil {
		return nil, fmt.Errorf("获取任务失败: %w", err)
	}

	// 如果这次是判错反馈，对本次使用到的记忆进行“反向降权/可废弃”
	if feedback != nil && feedback.Type == "incorrect" {
		s.penalizeUsedMemories(ctx, &task)
	}

	// 构建反思提示词
	reflectionPrompt := s.buildReflectionPrompt(&task, feedback)

	// 调用Dify进行反思（智能选择 chat / completion / workflow）
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
		// query 必填：用反馈内容承载（更像 user input）
		inputs = map[string]interface{}{
			systemKey: reflectionPrompt,
			queryKey:  feedback.Content,
		}
	} else {
		inputs = map[string]interface{}{
			"task_id":  taskID,
			"feedback": feedback.Content,
		}
	}

	resp, err := s.difyClient.ChatOrCompletion(reflectionPrompt, inputs)
	if err != nil {
		return nil, fmt.Errorf("反思失败: %w", err)
	}

	// 解析反思结果（简单解析，实际可以用更复杂的解析）
	memory := s.parseReflectionResult(resp.Answer, feedback)
	// 绑定 run_id，确保记忆不跨实验污染
	memory.RunID = task.RunID
	// 生产级：ApplyTo 不信任模型输出，强制绑定到当前任务类型，避免像 lottery_multi 这类场景写成 lottery 导致完全检索不到
	memory.ApplyTo = task.TaskType
	// 补全归并键（兼容旧数据：默认空串，但新写入必须填充）
	memory.TriggerKey = normalizeTriggerKey(memory.Trigger)

	// 检查是否已有相似记忆（用于演化）
	// 注意：trigger是MySQL保留关键字，需要用反引号包裹
	var existingMemory model.Memory
	q := db.DB.Model(&model.Memory{}).
		Where("trigger_key = ? AND apply_to = ?", memory.TriggerKey, memory.ApplyTo)
	if task.RunID > 0 {
		q = q.Where("run_id = ?", task.RunID)
	}
	query := q.
		Order("version DESC").
		First(&existingMemory)

	if query.Error == nil {
		// 演化：更新版本
		memory.Version = existingMemory.Version + 1
		// 可以选择覆盖或保留旧版本
	}

	// 保存记忆
	if err := db.DB.Create(memory).Error; err != nil {
		return nil, fmt.Errorf("保存记忆失败: %w", err)
	}

	// run_id=0 的常规模式：同步写入 MemOS（作为长期外部记忆层）
	// 注意：实验 run_id>0 严格不写入，避免污染实验可归因性
	if task.RunID == 0 && s.memosClient != nil && s.memosClient.Enabled() {
		userID := s.memOSUserID(task.TaskType)
		// 先注册（尽量幂等）；失败不影响主流程
		if err := s.memosClient.RegisterUser(ctx, userID); err != nil {
			log.Printf("[memos] register user failed user=%s err=%v", userID, err)
		}
		// content 用结构化文本，便于检索与审计
		content := fmt.Sprintf("apply_to=%s trigger=%s lesson=%s confidence=%.4f", memory.ApplyTo, memory.Trigger, memory.Lesson, memory.Confidence)
		// source 显式标注：本项目 + run_id=0 + reflection（避免与实验数据混淆）
		source := fmt.Sprintf("mem-test|local_db|run_id=0|reflection|task_id=%d|memory_id=%d", task.ID, memory.ID)
		if err := s.memosClient.AddMemory(ctx, userID, content, source); err != nil {
			log.Printf("[memos] add memory failed user=%s err=%v", userID, err)
		}
	}

	// 更新反馈记录
	feedback.UsedForMemory = true
	memoryID := memory.ID
	feedback.MemoryID = &memoryID
	db.DB.Save(feedback)

	return memory, nil
}

// ReflectAndSaveMemoryAndConsolidateGlobal 用于实验 D 组：
// - 仍然把“本次 run 的反思产物”写入 run_id=task.RunID（便于论文级追踪）
// - 额外把经验“固化/整合”到全局中期记忆池（run_id=0 且 derived_from 带 global 前缀），使下一次跑实验可复用
func (s *ReflectionService) ReflectAndSaveMemoryAndConsolidateGlobal(ctx context.Context, taskID uint, feedback *model.Feedback) (*model.Memory, error) {
	// 获取任务信息
	var task model.Task
	if err := db.DB.First(&task, taskID).Error; err != nil {
		return nil, fmt.Errorf("获取任务失败: %w", err)
	}
	if task.RunID == 0 {
		// 非实验模式不走“全局固化池”，保持原行为（可避免把日常零散数据混入实验池）
		return s.ReflectAndSaveMemory(ctx, taskID, feedback)
	}

	// 判错反馈：先对使用到的记忆做反向追责
	if feedback != nil && feedback.Type == "incorrect" {
		s.penalizeUsedMemories(ctx, &task)
	}

	// D 组：更强调“抽象可执行规则”，并注入实验元数据，提升总结规律质量
	reflectionPrompt := s.buildReflectionPromptForGlobal(&task, feedback)

	// 调用 Dify
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
			systemKey: reflectionPrompt,
			queryKey:  feedback.Content,
		}
	} else {
		inputs = map[string]interface{}{
			"task_id":  taskID,
			"feedback": feedback.Content,
		}
	}

	resp, err := s.difyClient.ChatOrCompletion(reflectionPrompt, inputs)
	if err != nil {
		return nil, fmt.Errorf("反思失败: %w", err)
	}

	// 解析 + 保存 run 内记忆（追踪用）
	runMemory := s.parseReflectionResult(resp.Answer, feedback)
	runMemory.RunID = task.RunID
	runMemory.ApplyTo = task.TaskType
	runMemory.TriggerKey = normalizeTriggerKey(runMemory.Trigger)
	if err := s.saveEvolvingMemory(ctx, &task, runMemory); err != nil {
		return nil, err
	}

	// 固化到全局池（run_id=0, derived_from=global|...）
	if err := s.consolidateToGlobal(ctx, &task, runMemory); err != nil {
		log.Printf("[global_memo] consolidate failed task_id=%d run_id=%d err=%v", task.ID, task.RunID, err)
	}

	// D 组：同步写入 MemOS（外部长期记忆层）
	// 注意：仅 D 组使用 MemOS，避免影响 A/B/C 的可归因对照
	if s.memosClient != nil && s.memosClient.Enabled() {
		userID := s.memOSUserID(task.TaskType)
		if err := s.memosClient.RegisterUser(ctx, userID); err != nil {
			log.Printf("[memos] register user failed user=%s err=%v", userID, err)
		}
		content := fmt.Sprintf("apply_to=%s trigger=%s lesson=%s confidence=%.4f", runMemory.ApplyTo, runMemory.Trigger, runMemory.Lesson, runMemory.Confidence)
		source := fmt.Sprintf("mem-test|exp|group=D|run_id=%d|task_id=%d|memory_id=%d", task.RunID, task.ID, runMemory.ID)
		if err := s.memosClient.AddMemory(ctx, userID, content, source); err != nil {
			log.Printf("[memos] add memory failed user=%s err=%v", userID, err)
		}
	}

	// 更新反馈记录（仍关联 run 内产物，便于追踪）
	feedback.UsedForMemory = true
	memoryID := runMemory.ID
	feedback.MemoryID = &memoryID
	db.DB.Save(feedback)

	return runMemory, nil
}

// ReflectAndSaveMemoryAndConsolidateGlobalValidated 用于实验 E 组：
// - 生成 run 内记忆（追踪用）
// - 基于近期样本做快速一致性验证（避免把噪声固化进全局池）
// - 通过后固化到全局中期记忆池（run_id=0 & derived_from=global|...）
// - 同步写入 MemOS（外部长期记忆层）
func (s *ReflectionService) ReflectAndSaveMemoryAndConsolidateGlobalValidated(ctx context.Context, taskID uint, feedback *model.Feedback) (*model.Memory, error) {
	var task model.Task
	if err := db.DB.First(&task, taskID).Error; err != nil {
		return nil, fmt.Errorf("获取任务失败: %w", err)
	}
	if task.RunID == 0 {
		return s.ReflectAndSaveMemory(ctx, taskID, feedback)
	}

	if feedback != nil && feedback.Type == "incorrect" {
		s.penalizeUsedMemories(ctx, &task)
	}

	reflectionPrompt := s.buildReflectionPromptForGlobal(&task, feedback)
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
			systemKey: reflectionPrompt,
			queryKey:  feedback.Content,
		}
	} else {
		inputs = map[string]interface{}{
			"task_id":  taskID,
			"feedback": feedback.Content,
		}
	}

	resp, err := s.difyClient.ChatOrCompletion(reflectionPrompt, inputs)
	if err != nil {
		return nil, fmt.Errorf("反思失败: %w", err)
	}

	runMemory := s.parseReflectionResult(resp.Answer, feedback)
	runMemory.RunID = task.RunID
	runMemory.ApplyTo = task.TaskType
	runMemory.TriggerKey = normalizeTriggerKey(runMemory.Trigger)
	if err := s.saveEvolvingMemory(ctx, &task, runMemory); err != nil {
		return nil, err
	}

	pass := s.quickValidateMemoryAgainstRecentTasks(ctx, &task, runMemory, 20)
	if pass {
		if err := s.consolidateToGlobal(ctx, &task, runMemory); err != nil {
			log.Printf("[global_memo] consolidate failed task_id=%d run_id=%d err=%v", task.ID, task.RunID, err)
		}
	} else {
		// 验证失败：不固化到全局池，避免把噪声扩散到下一次实验
		log.Printf("[global_memo] skip consolidate due to validation failed task_id=%d run_id=%d", task.ID, task.RunID)
	}

	// E 组：同步写入 MemOS（外部长期记忆层）
	if s.memosClient != nil && s.memosClient.Enabled() {
		userID := s.memOSUserID(task.TaskType)
		if err := s.memosClient.RegisterUser(ctx, userID); err != nil {
			log.Printf("[memos] register user failed user=%s err=%v", userID, err)
		}
		content := fmt.Sprintf("apply_to=%s trigger=%s lesson=%s confidence=%.4f", runMemory.ApplyTo, runMemory.Trigger, runMemory.Lesson, runMemory.Confidence)
		source := fmt.Sprintf("mem-test|exp|group=E|run_id=%d|task_id=%d|memory_id=%d|validated=%v", task.RunID, task.ID, runMemory.ID, pass)
		if err := s.memosClient.AddMemory(ctx, userID, content, source); err != nil {
			log.Printf("[memos] add memory failed user=%s err=%v", userID, err)
		}
	}

	feedback.UsedForMemory = true
	memoryID := runMemory.ID
	feedback.MemoryID = &memoryID
	db.DB.Save(feedback)

	return runMemory, nil
}

func (s *ReflectionService) memOSUserID(taskType string) string {
	p := strings.TrimSpace(s.memosUserPref)
	if p == "" {
		p = "mem-test"
	}
	tt := strings.TrimSpace(taskType)
	if tt == "" {
		tt = "unknown"
	}
	return fmt.Sprintf("%s:%s", p, tt)
}

func (s *ReflectionService) penalizeUsedMemories(ctx context.Context, task *model.Task) {
	ids := ParseMemoryIDs(task.MemoryIDs)
	if len(ids) == 0 {
		return
	}
	now := time.Now()
	const failureThreshold = 3

	// 说明：
	// - 原子更新 failure_count，避免并发丢更新
	// - 每次判错轻微降低 confidence，避免旧规则长期“霸榜”
	// - 失败次数达到阈值后标记 deprecated，检索时过滤
	updates := map[string]interface{}{
		"failure_count":  gorm.Expr("failure_count + 1"),
		"last_failed_at": now,
		"confidence":     gorm.Expr("GREATEST(confidence - ?, 0)", 0.05),
		"deprecated": gorm.Expr(
			"CASE WHEN failure_count + 1 >= ? THEN 1 ELSE deprecated END",
			failureThreshold,
		),
		"deprecated_at": gorm.Expr(
			"CASE WHEN failure_count + 1 >= ? AND (deprecated = 0 OR deprecated IS NULL) THEN ? ELSE deprecated_at END",
			failureThreshold,
			now,
		),
	}

	_ = db.DB.WithContext(ctx).
		Model(&model.Memory{}).
		Where("id IN ?", ids).
		Updates(updates).Error
}

func (s *ReflectionService) buildReflectionPrompt(task *model.Task, feedback *model.Feedback) string {
	var prompt strings.Builder

	prompt.WriteString("你刚刚完成了一个任务，但收到了反馈。请进行反思。\n\n")
	prompt.WriteString(fmt.Sprintf("任务类型: %s\n", task.TaskType))
	prompt.WriteString(fmt.Sprintf("输入: %s\n", task.Input))
	prompt.WriteString(fmt.Sprintf("你的输出: %s\n", task.Output))
	prompt.WriteString(fmt.Sprintf("反馈类型: %s\n", feedback.Type))
	prompt.WriteString(fmt.Sprintf("反馈内容: %s\n\n", feedback.Content))

	prompt.WriteString("请只输出严格 JSON（不要 Markdown、不要解释、不要多余文本），字段名固定如下：\n")
	prompt.WriteString("1. trigger: 触发条件（简短关键词）\n")
	prompt.WriteString("2. lesson: 学到的经验（可复用规则）\n")
	prompt.WriteString("3. apply_to: 适用范围（任务类型，必须与上面的任务类型一致，例如 lottery 或 lottery_multi）\n")
	prompt.WriteString("4. confidence: 置信度（0~1 小数）\n\n")
	prompt.WriteString("输出格式示例：\n")
	prompt.WriteString(`{"trigger": "...", "lesson": "...", "apply_to": "...", "confidence": 0.8}`)

	return prompt.String()
}

func (s *ReflectionService) buildReflectionPromptForGlobal(task *model.Task, feedback *model.Feedback) string {
	var prompt strings.Builder
	prompt.WriteString("你刚刚完成了一个任务，但收到了反馈。请进行反思，并提炼成可复用的规则记忆。\n\n")

	prompt.WriteString(fmt.Sprintf("任务类型: %s\n", task.TaskType))
	prompt.WriteString(fmt.Sprintf("输入: %s\n", task.Input))
	prompt.WriteString(fmt.Sprintf("你的输出: %s\n", task.Output))
	if task.RuleMode != "" {
		prompt.WriteString(fmt.Sprintf("规则变更模式: %s\n", task.RuleMode))
	}
	if task.RuleVersion > 0 {
		prompt.WriteString(fmt.Sprintf("规则版本: %d\n", task.RuleVersion))
	}
	if task.RuleThreshold > 0 {
		prompt.WriteString(fmt.Sprintf("当前门槛/门限(若适用): %d\n", task.RuleThreshold))
	}
	if task.Round >= 0 {
		prompt.WriteString(fmt.Sprintf("轮次: %d\n", task.Round))
	}
	prompt.WriteString(fmt.Sprintf("反馈类型: %s\n", feedback.Type))
	prompt.WriteString(fmt.Sprintf("反馈内容: %s\n\n", feedback.Content))

	prompt.WriteString("要求：\n")
	prompt.WriteString("- 产出的 lesson 必须是“可执行的条件规则”，优先使用 if/then 或 条件→动作 的形式\n")
	prompt.WriteString("- 尽量抽象，不要仅复述反馈；不要把某一次具体输入当成规则\n")
	prompt.WriteString("- 若涉及门槛/门限，请用“门槛(当前值=...)”描述，避免写死到具体 case\n\n")

	prompt.WriteString("请只输出严格 JSON（不要 Markdown、不要解释、不要多余文本），字段名固定如下：\n")
	prompt.WriteString("1. trigger: 触发条件（简短关键词，用于检索）\n")
	prompt.WriteString("2. lesson: 学到的经验（可复用规则）\n")
	prompt.WriteString("3. apply_to: 适用范围（任务类型，必须与上面的任务类型一致，例如 lottery 或 lottery_multi）\n")
	prompt.WriteString("4. confidence: 置信度（0~1 小数）\n\n")
	prompt.WriteString(`{"trigger": "...", "lesson": "...", "apply_to": "...", "confidence": 0.8}`)
	return prompt.String()
}

func (s *ReflectionService) saveEvolvingMemory(ctx context.Context, task *model.Task, memory *model.Memory) error {
	// 检查是否已有相似记忆（用于演化）
	var existingMemory model.Memory
	q := db.DB.WithContext(ctx).Model(&model.Memory{}).
		Where("trigger_key = ? AND apply_to = ?", memory.TriggerKey, memory.ApplyTo)
	if task.RunID > 0 {
		q = q.Where("run_id = ?", task.RunID)
	}
	query := q.Order("version DESC").First(&existingMemory)

	if query.Error == nil {
		memory.Version = existingMemory.Version + 1
	}
	if err := db.DB.WithContext(ctx).Create(memory).Error; err != nil {
		return fmt.Errorf("保存记忆失败: %w", err)
	}
	return nil
}

func (s *ReflectionService) consolidateToGlobal(ctx context.Context, task *model.Task, runMemory *model.Memory) error {
	if task == nil || runMemory == nil {
		return nil
	}
	// 只固化实验 run（run_id>0）
	if task.RunID == 0 {
		return nil
	}

	triggerKey := normalizeTriggerKey(runMemory.Trigger)
	applyTo := strings.TrimSpace(task.TaskType)
	if applyTo == "" {
		applyTo = strings.TrimSpace(runMemory.ApplyTo)
	}
	if applyTo == "" {
		applyTo = "通用"
	}

	// 查找全局池的最新版本（只取 derived_from=global|... 的记录）
	var existing model.Memory
	q := db.DB.WithContext(ctx).Model(&model.Memory{}).
		Where("run_id = 0 AND apply_to = ? AND trigger_key = ? AND derived_from LIKE ?", applyTo, triggerKey, "global|%").
		Order("version DESC").
		First(&existing)

	newGlobal := &model.Memory{
		RunID:       0,
		Trigger:     strings.TrimSpace(runMemory.Trigger),
		TriggerKey:  triggerKey,
		Lesson:      strings.TrimSpace(runMemory.Lesson),
		DerivedFrom: fmt.Sprintf("global|src_run_id=%d|task_id=%d|src_memory_id=%d", task.RunID, task.ID, runMemory.ID),
		ApplyTo:     applyTo,
		Confidence:  runMemory.Confidence,
		Version:     1,
	}
	// 置信度边界（避免模型异常输出）
	if newGlobal.Confidence < 0 {
		newGlobal.Confidence = 0
	}
	if newGlobal.Confidence > 1 {
		newGlobal.Confidence = 1
	}

	if q.Error == nil {
		// 若规则文本几乎一致：不新增版本，仅轻微提高置信度（“重复证据”）
		if normalizeLessonText(existing.Lesson) == normalizeLessonText(newGlobal.Lesson) &&
			normalizeLessonText(existing.Trigger) == normalizeLessonText(newGlobal.Trigger) {
			return db.DB.WithContext(ctx).
				Model(&model.Memory{}).
				Where("id = ?", existing.ID).
				Updates(map[string]interface{}{
					"confidence": gorm.Expr("LEAST(confidence + ?, 1)", 0.02),
					"updated_at": time.Now(),
				}).Error
		}
		newGlobal.Version = existing.Version + 1
	}

	return db.DB.WithContext(ctx).Create(newGlobal).Error
}

func normalizeLessonText(s string) string {
	s = strings.TrimSpace(s)
	s = whitespaceRe.ReplaceAllString(s, " ")
	s = strings.ToLower(s)
	if len(s) > 800 {
		s = s[:800]
	}
	return s
}

func (s *ReflectionService) quickValidateMemoryAgainstRecentTasks(ctx context.Context, task *model.Task, mem *model.Memory, limit int) bool {
	if task == nil || mem == nil || limit <= 0 {
		return false
	}
	tt := strings.TrimSpace(task.TaskType)
	if tt == "" {
		return false
	}
	thr, ok := extractThresholdFromText(mem.Trigger + " " + mem.Lesson)
	if !ok {
		return false
	}

	var tasks []model.Task
	q := db.DB.WithContext(ctx).Model(&model.Task{}).
		Where("run_id = ? AND task_type = ? AND rule_threshold > 0", task.RunID, tt).
		Order("id DESC").
		Limit(limit).
		Find(&tasks)
	if q.Error != nil || len(tasks) < 5 {
		return false
	}

	conflicts := 0
	checked := 0
	for _, t := range tasks {
		exp, ok := expectedAllowFromTask(tt, &t)
		if !ok {
			continue
		}
		pred, ok2 := predictedAllowFromRule(tt, &t, thr)
		if !ok2 {
			continue
		}
		checked++
		if pred != exp {
			conflicts++
		}
	}
	if checked < 5 {
		return false
	}
	// 冲突率过高则拒绝固化
	return float64(conflicts)/float64(checked) <= 0.10
}

func expectedAllowFromTask(taskType string, t *model.Task) (bool, bool) {
	if t == nil {
		return false, false
	}
	var inputData map[string]interface{}
	if err := json.Unmarshal([]byte(t.Input), &inputData); err != nil {
		inputData = map[string]interface{}{}
	}
	threshold := t.RuleThreshold
	if threshold <= 0 {
		return false, false
	}
	switch taskType {
	case "lottery":
		points, ok := inputData["points"].(float64)
		if !ok {
			points = 0
		}
		return int(points) >= threshold, true
	case "lottery_multi":
		available := int(getFloat(inputData, "points_available"))
		bonus := int(getFloat(inputData, "points_bonus"))
		locked := int(getFloat(inputData, "points_locked"))
		expiring := int(getFloat(inputData, "points_expiring"))
		expDays := int(getFloat(inputData, "expiring_days"))
		penalty := int(getFloat(inputData, "points_penalty"))
		effective, _ := computeEffectivePoints(threshold, available, bonus, locked, expiring, expDays, penalty)
		return effective >= threshold, true
	default:
		return false, false
	}
}

func predictedAllowFromRule(taskType string, t *model.Task, ruleThreshold int) (bool, bool) {
	if t == nil || ruleThreshold <= 0 {
		return false, false
	}
	var inputData map[string]interface{}
	if err := json.Unmarshal([]byte(t.Input), &inputData); err != nil {
		inputData = map[string]interface{}{}
	}
	switch taskType {
	case "lottery":
		points, ok := inputData["points"].(float64)
		if !ok {
			points = 0
		}
		return int(points) >= ruleThreshold, true
	case "lottery_multi":
		available := int(getFloat(inputData, "points_available"))
		bonus := int(getFloat(inputData, "points_bonus"))
		locked := int(getFloat(inputData, "points_locked"))
		expiring := int(getFloat(inputData, "points_expiring"))
		expDays := int(getFloat(inputData, "expiring_days"))
		penalty := int(getFloat(inputData, "points_penalty"))
		effective, _ := computeEffectivePoints(ruleThreshold, available, bonus, locked, expiring, expDays, penalty)
		return effective >= ruleThreshold, true
	default:
		return false, false
	}
}

func (s *ReflectionService) parseReflectionResult(answer string, feedback *model.Feedback) *model.Memory {
	// 优先 JSON 解析（更稳，换模型时也更不容易因为排版变化而失效）
	memory := &model.Memory{
		DerivedFrom: feedback.Content,
		Version:     1,
		Confidence:  0.8,
	}

	raw := strings.TrimSpace(answer)
	// 尝试截取最外层 JSON（允许模型前后夹带解释）
	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j > i {
			raw = raw[i : j+1]
		}
	}
	// 1) 优先按标准 key 解析
	type reflectionJSON struct {
		Trigger    string   `json:"trigger"`
		Lesson     string   `json:"lesson"`
		ApplyTo    string   `json:"apply_to"`
		Confidence *float64 `json:"confidence"`
	}
	var rj reflectionJSON
	if err := json.Unmarshal([]byte(raw), &rj); err == nil {
		memory.Trigger = strings.TrimSpace(rj.Trigger)
		memory.Lesson = strings.TrimSpace(rj.Lesson)
		memory.ApplyTo = strings.TrimSpace(rj.ApplyTo)
		if rj.Confidence != nil {
			memory.Confidence = *rj.Confidence
		}
	} else {
		// 2) 兼容中文 key（触发条件/学到的经验/适用范围/置信度）
		var m map[string]interface{}
		if err2 := json.Unmarshal([]byte(raw), &m); err2 == nil {
			// 常见中英文别名
			for _, k := range []string{"trigger", "触发条件"} {
				if v, ok := m[k]; ok {
					if s, ok := v.(string); ok {
						memory.Trigger = strings.TrimSpace(s)
						break
					}
				}
			}
			for _, k := range []string{"lesson", "学到的经验"} {
				if v, ok := m[k]; ok {
					if s, ok := v.(string); ok {
						memory.Lesson = strings.TrimSpace(s)
						break
					}
				}
			}
			for _, k := range []string{"apply_to", "适用范围"} {
				if v, ok := m[k]; ok {
					if s, ok := v.(string); ok {
						memory.ApplyTo = strings.TrimSpace(s)
						break
					}
				}
			}
			for _, k := range []string{"confidence", "置信度"} {
				if v, ok := m[k]; ok {
					switch vv := v.(type) {
					case float64:
						memory.Confidence = vv
					case string:
						// 简化：忽略无法解析的字符串
					}
					break
				}
			}
		}
	}

	// 如果没有解析到，使用默认值
	if memory.Trigger == "" {
		memory.Trigger = "通用规则"
	}
	if memory.Lesson == "" {
		memory.Lesson = feedback.Content
	}
	if memory.ApplyTo == "" {
		memory.ApplyTo = "通用"
	}

	return memory
}
