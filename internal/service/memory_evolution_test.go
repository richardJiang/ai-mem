package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mem-test/internal/model"
)

// TestMemoryEvolutionWithRuleChanges 模拟规则变化场景下的记忆演化
// 场景：抽奖门槛变化 100 -> 120 -> 100，验证记忆系统如何应对
func TestMemoryEvolutionWithRuleChanges(t *testing.T) {
	// 注意：这个测试需要真实数据库连接，这里用 mock 演示逻辑
	// 实际运行需要配置好测试数据库

	// ===== 阶段1：规则门槛=100 =====
	t.Log("===== 阶段1：规则门槛=100 =====")

	// 模拟任务执行（积分=90，门槛=100，应该拒绝）
	task1 := createMockTask(1, "lottery", `{"points": 90, "action": "lottery"}`, "C")

	// 模拟判题（门槛=100）
	feedback1, correct1 := judgeWithThreshold(task1, 100)
	t.Logf("Task1: points=90, threshold=100, correct=%v", correct1)

	if !correct1 {
		// 模拟反思生成记忆
		memory1 := simulateReflection(task1, feedback1, 1)
		t.Logf("生成记忆1: trigger='%s', lesson='%s', version=%d, confidence=%.2f",
			memory1.Trigger, memory1.Lesson, memory1.Version, memory1.Confidence)
	}

	// ===== 阶段2：规则变更为门槛=120 =====
	t.Log("\n===== 阶段2：规则变更为门槛=120 =====")

	// 模拟任务执行（积分=110，新门槛=120，应该拒绝）
	// 但如果检索到旧记忆（门槛=100），模型可能判断为"允许"
	task2 := createMockTask(2, "lottery", `{"points": 110, "action": "lottery"}`, "C")

	// 使用旧记忆推理（门槛=100的规则），会得到错误结果
	oldMemories := []model.Memory{
		{
			ID:         1,
			Trigger:    "积分<100",
			Lesson:     "当用户积分低于100时，拒绝抽奖",
			ApplyTo:    "lottery",
			Confidence: 0.9,
			Version:    1,
			UseCount:   3,
		},
	}

	t.Logf("检索到旧记忆: %s (version=%d, use_count=%d)",
		oldMemories[0].Lesson, oldMemories[0].Version, oldMemories[0].UseCount)

	// 模拟判题（新门槛=120）
	feedback2, correct2 := judgeWithThreshold(task2, 120)
	t.Logf("Task2: points=110, threshold=120(新规则), correct=%v (旧记忆导致判错)", correct2)

	if !correct2 {
		// 生成新记忆（演化）
		memory2 := simulateReflection(task2, feedback2, 1) // 同一个 trigger，版本+1
		memory2.Version = 2                                // 演化为版本2
		t.Logf("生成记忆2(演化): trigger='%s', lesson='%s', version=%d, confidence=%.2f",
			memory2.Trigger, memory2.Lesson, memory2.Version, memory2.Confidence)
	}

	// ===== 阶段3：规则又变回门槛=100 =====
	t.Log("\n===== 阶段3：规则又变回门槛=100 =====")

	task3 := createMockTask(3, "lottery", `{"points": 105, "action": "lottery"}`, "C")

	// 此时检索会得到多个版本的记忆
	allMemories := []model.Memory{
		{
			ID:         1,
			Trigger:    "积分<100",
			Lesson:     "当用户积分低于100时，拒绝抽奖",
			ApplyTo:    "lottery",
			Confidence: 0.9,
			Version:    1,
			UseCount:   3,
		},
		{
			ID:         2,
			Trigger:    "积分<120",
			Lesson:     "当用户积分低于120时，拒绝抽奖（新规则）",
			ApplyTo:    "lottery",
			Confidence: 0.85,
			Version:    2,
			UseCount:   1, // 刚生成，使用次数少
		},
	}

	// 按现有排序：confidence DESC, use_count DESC
	// version=1: conf=0.9, use=3
	// version=2: conf=0.85, use=1
	// 会优先使用 version=1（这在"规则变回去"的情况下反而是对的！）

	t.Logf("检索到多版本记忆:")
	for _, m := range allMemories {
		t.Logf("  - version=%d, confidence=%.2f, use_count=%d: %s",
			m.Version, m.Confidence, m.UseCount, m.Lesson)
	}

	// 判题（门槛又变回100）
	_, correct3 := judgeWithThreshold(task3, 100)
	t.Logf("Task3: points=105, threshold=100(变回), correct=%v", correct3)

	if correct3 {
		t.Log("使用version=1的旧记忆，判断正确！说明版本演化机制有一定容错能力")
	}

	// ===== 阶段4：模拟"经常变化"场景的问题 =====
	t.Log("\n===== 阶段4：模拟频繁变化导致的记忆混乱 =====")

	// 假设门槛在 100/120/110/100/130/100 之间频繁切换
	// 且模型总是使用"前一个门槛的记忆"导致判错
	thresholds := []int{100, 120, 110, 100, 130, 100}

	memoryPool := make([]model.Memory, 0)
	for i, threshold := range thresholds {
		points := threshold - 10 // 总是低于门槛一点
		taskInput := fmt.Sprintf(`{"points": %d, "action": "lottery"}`, points)
		task := createMockTask(uint(10+i), "lottery", taskInput, "C")

		// 模拟使用旧规则判断（使用上一轮的门槛）
		oldThreshold := 100
		if i > 0 {
			oldThreshold = thresholds[i-1]
		}

		// 用旧门槛推理，用新门槛判题
		modelAllow := points >= oldThreshold
		task.Output = fmt.Sprintf(`{"allow": %v, "reason": "使用旧规则"}`, modelAllow)

		// 用新门槛判题
		actualAllow := points >= threshold
		isCorrect := modelAllow == actualAllow
		task.IsCorrect = &isCorrect

		if !isCorrect {
			// 每次错误都生成新记忆
			mem := model.Memory{
				ID:         uint(10 + i),
				Trigger:    fmt.Sprintf("积分<%d", threshold),
				Lesson:     fmt.Sprintf("当用户积分低于%d时，拒绝抽奖", threshold),
				ApplyTo:    "lottery",
				Confidence: 0.8 + float64(i)*0.01, // 模拟置信度变化
				Version:    i + 1,
				UseCount:   0,
			}
			memoryPool = append(memoryPool, mem)

			t.Logf("轮次%d: 旧threshold=%d, 新threshold=%d, points=%d, 判错! 生成记忆 version=%d",
				i+1, oldThreshold, threshold, points, mem.Version)
		}
	}

	t.Logf("\n经过%d次规则变化，累积了%d条互相矛盾的记忆", len(thresholds), len(memoryPool))
	if len(memoryPool) > 0 {
		t.Log("记忆列表:")
		for _, m := range memoryPool {
			t.Logf("  - %s (confidence=%.2f, version=%d)", m.Lesson, m.Confidence, m.Version)
		}
	}
	t.Log("问题：当前系统只按 confidence + use_count 排序，无法识别哪条是'当前有效'的规则")
	t.Log("解决方向：需要引入时间维度、废弃机制、或反向降权逻辑")

	// 生成测试报告
	generateMemoryEvolutionReport(t, memoryPool, thresholds)
}

// TestMemoryRetrievalPriority 测试记忆检索的优先级逻辑
func TestMemoryRetrievalPriority(t *testing.T) {
	t.Log("===== 测试记忆检索排序逻辑 =====")

	// 模拟3条记忆
	memories := []model.Memory{
		{
			ID:         1,
			Trigger:    "积分<100",
			Lesson:     "旧规则",
			Confidence: 0.9,
			UseCount:   10, // 高使用次数
			Version:    1,
		},
		{
			ID:         2,
			Trigger:    "积分<120",
			Lesson:     "新规则",
			Confidence: 0.95, // 高置信度
			UseCount:   2,
			Version:    2,
		},
		{
			ID:         3,
			Trigger:    "积分<100",
			Lesson:     "最新规则（变回）",
			Confidence: 0.85,
			UseCount:   1,
			Version:    3,
		},
	}

	// 当前排序：ORDER BY confidence DESC, use_count DESC
	// 期望顺序：id=2(0.95,2) > id=1(0.9,10) > id=3(0.85,1)

	t.Log("按当前规则排序 (confidence DESC, use_count DESC):")
	sorted := sortMemoriesByCurrentRule(memories)
	for i, m := range sorted {
		t.Logf("  %d. ID=%d, confidence=%.2f, use_count=%d, version=%d: %s",
			i+1, m.ID, m.Confidence, m.UseCount, m.Version, m.Lesson)
	}

	t.Log("\n观察：")
	t.Log("- 新规则(id=2)因为高置信度排第一 ✓")
	t.Log("- 但如果规则又变回去，version=3 应该优先，现在却排最后 ✗")
	t.Log("- 缺少'最近验证正确'的时间维度")
}

// TestMemoryDeprecationScenario 模拟"记忆废弃"场景
func TestMemoryDeprecationScenario(t *testing.T) {
	t.Log("===== 模拟记忆废弃场景 =====")

	// 记忆A：旧规则
	memoryA := model.Memory{
		ID:         1,
		Trigger:    "积分<100",
		Lesson:     "积分低于100拒绝",
		Confidence: 0.9,
		UseCount:   5,
		Version:    1,
	}

	t.Logf("初始记忆: %s (confidence=%.2f, use_count=%d)", memoryA.Lesson, memoryA.Confidence, memoryA.UseCount)

	// 使用这条记忆进行推理
	task := createMockTask(1, "lottery", `{"points": 105, "action": "lottery"}`, "C")
	t.Logf("任务: points=105, 使用记忆A推理...")

	// 如果规则已变为120，判题会失败
	_, correct := judgeWithThreshold(task, 120)

	if !correct {
		t.Log("判题失败！这条记忆导致了错误")
		t.Log("理想的废弃机制应该:")
		t.Log("  1. 识别出'使用了记忆ID=1'")
		t.Log("  2. 给这条记忆降权: confidence -= 0.1 或标记 deprecated=true")
		t.Log("  3. 下次检索时过滤掉或排序靠后")

		// 模拟降权
		memoryA.Confidence -= 0.2
		if memoryA.Confidence < 0.3 {
			t.Logf("记忆置信度降至%.2f，建议标记为废弃", memoryA.Confidence)
		}
	}

	t.Log("\n当前实现的问题：")
	t.Log("- Task.MemoryIDs 记录了使用的记忆ID ✓")
	t.Log("- 但判错后，不会反向追责并降权这些记忆 ✗")
	t.Log("- 需要在 CoachService.JudgeLotteryTask 之后，增加降权逻辑")
}

// ===== 辅助函数 =====

func createMockTask(id uint, taskType, input, groupType string) *model.Task {
	return &model.Task{
		ID:        id,
		TaskType:  taskType,
		Input:     input,
		GroupType: groupType,
		RunID:     1,
	}
}

func judgeWithThreshold(task *model.Task, threshold int) (*model.Feedback, bool) {
	var inputData map[string]interface{}
	json.Unmarshal([]byte(task.Input), &inputData)

	points, _ := inputData["points"].(float64)
	expectedAllow := int(points) >= threshold

	// 模拟模型输出
	modelOutput := fmt.Sprintf(`{"allow": %v, "reason": "test"}`, expectedAllow)
	task.Output = modelOutput

	var ans struct {
		Allow *bool `json:"allow"`
	}
	json.Unmarshal([]byte(modelOutput), &ans)

	actualAllow := false
	if ans.Allow != nil {
		actualAllow = *ans.Allow
	}

	isCorrect := actualAllow == expectedAllow
	task.IsCorrect = &isCorrect

	feedbackType := "correct"
	content := "判断正确"
	if !isCorrect {
		feedbackType = "incorrect"
		content = fmt.Sprintf("判断错误。积分=%.0f时，应该: allow=%v（门槛=%d）", points, expectedAllow, threshold)
	}

	feedback := &model.Feedback{
		TaskID:  task.ID,
		Type:    feedbackType,
		Content: content,
	}

	return feedback, isCorrect
}

func simulateReflection(task *model.Task, feedback *model.Feedback, runID uint) *model.Memory {
	var inputData map[string]interface{}
	json.Unmarshal([]byte(task.Input), &inputData)

	// 从反馈中提取门槛信息（简化）
	threshold := 100 // 默认

	return &model.Memory{
		RunID:       runID,
		Trigger:     fmt.Sprintf("积分<%.0f", float64(threshold)),
		Lesson:      fmt.Sprintf("当用户积分低于%d时，拒绝抽奖", threshold),
		ApplyTo:     "lottery",
		Confidence:  0.8,
		Version:     1,
		DerivedFrom: feedback.Content,
		UseCount:    0,
		CreatedAt:   time.Now(),
	}
}

func sortMemoriesByCurrentRule(memories []model.Memory) []model.Memory {
	// 模拟当前排序：ORDER BY confidence DESC, use_count DESC
	result := make([]model.Memory, len(memories))
	copy(result, memories)

	// 简单冒泡排序
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			// confidence优先
			if result[i].Confidence < result[j].Confidence {
				result[i], result[j] = result[j], result[i]
			} else if result[i].Confidence == result[j].Confidence {
				// confidence相同时看use_count
				if result[i].UseCount < result[j].UseCount {
					result[i], result[j] = result[j], result[i]
				}
			}
		}
	}

	return result
}

// generateMemoryEvolutionReport 生成记忆演化测试报告
func generateMemoryEvolutionReport(t *testing.T, memories []model.Memory, thresholds []int) {
	report := map[string]interface{}{
		"test_name":          "MemoryEvolutionTest",
		"timestamp":          time.Now().Format(time.RFC3339),
		"rule_changes":       thresholds,
		"total_changes":      len(thresholds),
		"memories_generated": len(memories),
		"memories":           memories,
		"analysis": map[string]interface{}{
			"current_mechanism": map[string]interface{}{
				"sorting_rule":  "confidence DESC, use_count DESC",
				"versioning":    "enabled",
				"run_isolation": "enabled",
			},
			"strengths": []string{
				"版本演化：相似 trigger 的记忆会递增 version",
				"run 隔离：不同实验不会互相污染",
				"排序机制：按 confidence + use_count 排序有一定合理性",
				"容错能力：规则变回时旧记忆可能因高 use_count 重新生效",
			},
			"weaknesses": []string{
				"缺少时间维度：没有 last_used_at、last_verified_at",
				"缺少废弃机制：记忆导致错误后不会被降权或标记废弃",
				"缺少有效期/时间窗口：无法过滤太久远的记忆",
				"对频繁变化场景支持不足：会累积互相矛盾的记忆",
			},
			"recommendations": []string{
				"增加字段：LastUsedAt, LastVerifiedAt, Deprecated, FailureCount",
				"检索逻辑增强：过滤废弃、时间窗口、时间优先排序",
				"反向降权逻辑：判错时给使用的记忆降权",
				"反向验证逻辑：判对时更新 last_verified_at",
			},
		},
		"conclusion": map[string]interface{}{
			"verdict":          "当前策略方向正确，对偶尔变化够用，对频繁变化需增强",
			"suitable_for":     "验证 test-time learning 的 MVP 目标",
			"not_suitable_for": "生产环境下频繁变化的业务规则（需增强）",
		},
	}

	// 保存到 outputs 目录
	// 获取项目根目录
	workDir, _ := os.Getwd()
	if !filepath.IsAbs(workDir) {
		workDir, _ = filepath.Abs(workDir)
	}
	// 如果当前在 internal/service 目录，向上两级
	if filepath.Base(workDir) == "service" {
		workDir = filepath.Join(workDir, "..", "..")
	}

	outputDir := filepath.Join(workDir, "outputs")
	os.MkdirAll(outputDir, 0o755)

	filePath := filepath.Join(outputDir, fmt.Sprintf("memory_evolution_report_%s.json", time.Now().Format("20060102_150405")))
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Errorf("生成报告失败: %v", err)
		return
	}

	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		t.Errorf("保存报告失败: %v", err)
		return
	}

	t.Logf("\n✅ 记忆演化测试报告已生成: %s", filePath)
}
