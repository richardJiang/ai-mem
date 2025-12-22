package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mem-test/internal/config"
	"mem-test/internal/db"
	"mem-test/internal/model"
)

// TestMemoryEvolution_Integration 集成测试：真实测试记忆演化
// 需要真实的数据库连接
func TestMemoryEvolution_Integration(t *testing.T) {
	// 加载测试配置
	cfg, err := config.LoadConfig("../../config/config.yaml")
	if err != nil {
		t.Skip("跳过集成测试：无法加载配置文件（请确保 config/config.yaml 存在）")
		return
	}

	// 初始化数据库
	if err := db.InitDB(cfg); err != nil {
		t.Skip("跳过集成测试：无法连接数据库")
		return
	}

	// 创建服务实例
	difyClient := NewDifyClient(
		cfg.Dify.BaseURL,
		cfg.Dify.APIKey,
		cfg.Dify.AppType,
		cfg.Dify.ResponseMode,
		cfg.Dify.WorkflowSystemKey,
		cfg.Dify.WorkflowQueryKey,
		cfg.Dify.WorkflowOutputKey,
	)

	agentService := NewAgentService(difyClient, nil, "")
	coachService := NewCoachService()
	reflectionService := NewReflectionService(difyClient, nil, "")

	ctx := context.Background()

	// 创建测试专用的 run_id
	testRun := &model.ExperimentRun{
		TaskType:     "lottery",
		RunsPerGroup: 3,
		Seed:         time.Now().UnixNano(),
		GroupsJSON:   `["C"]`,
	}
	if err := db.DB.Create(testRun).Error; err != nil {
		t.Fatalf("创建测试 run 失败: %v", err)
	}
	runID := testRun.ID

	t.Logf("创建测试 RunID: %d", runID)

	// 测试场景：规则从门槛100变为120，再变回100
	scenarios := []struct {
		name      string
		points    int
		threshold int
		expected  bool
	}{
		{"第1轮-门槛100-积分90", 90, 100, false},
		{"第2轮-门槛120-积分110", 110, 120, false},
		{"第3轮-门槛100-积分105", 105, 100, true},
	}

	var generatedMemories []model.Memory

	for _, scenario := range scenarios {
		t.Logf("\n===== %s =====", scenario.name)

		// 构造输入
		input := fmt.Sprintf(`{"points": %d, "action": "lottery"}`, scenario.points)

		// 执行任务（C组，使用记忆）
		task, err := agentService.ExecuteTaskInRun(ctx, runID, "lottery", input, "C", true)
		if err != nil {
			t.Errorf("执行任务失败: %v", err)
			continue
		}

		t.Logf("任务执行完成: ID=%d, Output=%s", task.ID, task.Output)

		// 判题（用当前场景的门槛）
		feedback, err := judgeWithCustomThreshold(coachService, ctx, task, scenario.threshold, scenario.expected)
		if err != nil {
			t.Errorf("判题失败: %v", err)
			continue
		}

		t.Logf("判题结果: Type=%s, Content=%s", feedback.Type, feedback.Content)

		// 如果判错，触发反思
		if feedback.Type == "incorrect" {
			memory, err := reflectionService.ReflectAndSaveMemory(ctx, task.ID, feedback)
			if err != nil {
				t.Errorf("反思失败: %v", err)
				continue
			}

			generatedMemories = append(generatedMemories, *memory)
			t.Logf("生成记忆: ID=%d, Trigger='%s', Lesson='%s', Version=%d",
				memory.ID, memory.Trigger, memory.Lesson, memory.Version)
		}
	}

	// 生成报告
	generateIntegrationTestReport(t, runID, generatedMemories, scenarios)

	t.Logf("\n✅ 集成测试完成，生成了 %d 条记忆", len(generatedMemories))
}

// judgeWithCustomThreshold 用自定义门槛判题
func judgeWithCustomThreshold(coach *CoachService, ctx context.Context, task *model.Task, threshold int, expectedAllow bool) (*model.Feedback, error) {
	var inputData map[string]interface{}
	json.Unmarshal([]byte(task.Input), &inputData)

	points, _ := inputData["points"].(float64)

	// 解析模型输出
	var output struct {
		Allow  *bool  `json:"allow"`
		Reason string `json:"reason"`
	}
	json.Unmarshal([]byte(task.Output), &output)

	modelAllow := false
	if output.Allow != nil {
		modelAllow = *output.Allow
	}

	// 判断是否正确
	isCorrect := modelAllow == expectedAllow
	task.IsCorrect = &isCorrect
	db.DB.Save(task)

	feedbackType := "correct"
	content := "判断正确"
	if !isCorrect {
		feedbackType = "incorrect"
		content = fmt.Sprintf("判断错误。积分=%.0f时，门槛=%d，应该: allow=%v，实际输出: allow=%v",
			points, threshold, expectedAllow, modelAllow)
	}

	return coach.SubmitFeedback(ctx, task.ID, feedbackType, content)
}

// generateIntegrationTestReport 生成集成测试报告
func generateIntegrationTestReport(t *testing.T, runID uint, memories []model.Memory, scenarios []struct {
	name      string
	points    int
	threshold int
	expected  bool
}) {
	report := map[string]interface{}{
		"test_type":          "IntegrationTest",
		"test_name":          "MemoryEvolution_RealDatabase",
		"timestamp":          time.Now().Format(time.RFC3339),
		"run_id":             runID,
		"scenarios":          scenarios,
		"memories_generated": len(memories),
		"memories":           memories,
		"notes": []string{
			"这是集成测试，使用了真实的数据库和服务",
			"测试了规则变化场景下的记忆生成和检索",
			"验证了记忆系统在实际环境中的行为",
		},
	}

	// 保存到 outputs 目录
	workDir, _ := os.Getwd()
	if filepath.Base(workDir) == "service" {
		workDir = filepath.Join(workDir, "..", "..")
	}

	outputDir := filepath.Join(workDir, "outputs")
	os.MkdirAll(outputDir, 0o755)

	filePath := filepath.Join(outputDir, fmt.Sprintf("memory_integration_test_%s.json", time.Now().Format("20060102_150405")))
	data, _ := json.MarshalIndent(report, "", "  ")
	os.WriteFile(filePath, data, 0o644)

	t.Logf("\n✅ 集成测试报告已生成: %s", filePath)
}
