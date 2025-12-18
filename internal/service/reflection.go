package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"mem-test/internal/db"
	"mem-test/internal/model"
)

type ReflectionService struct {
	difyClient *DifyClient
}

func NewReflectionService(difyClient *DifyClient) *ReflectionService {
	return &ReflectionService{
		difyClient: difyClient,
	}
}

// ReflectAndSaveMemory 反思并保存记忆
func (s *ReflectionService) ReflectAndSaveMemory(ctx context.Context, taskID uint, feedback *model.Feedback) (*model.Memory, error) {
	// 获取任务信息
	var task model.Task
	if err := db.DB.First(&task, taskID).Error; err != nil {
		return nil, fmt.Errorf("获取任务失败: %w", err)
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

	// 检查是否已有相似记忆（用于演化）
	// 注意：trigger是MySQL保留关键字，需要用反引号包裹
	var existingMemory model.Memory
	q := db.DB.Model(&model.Memory{}).Where("`trigger` LIKE ?", "%"+memory.Trigger+"%")
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

	// 更新反馈记录
	feedback.UsedForMemory = true
	memoryID := memory.ID
	feedback.MemoryID = &memoryID
	db.DB.Save(feedback)

	return memory, nil
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
	prompt.WriteString("3. apply_to: 适用范围（任务类型，例如 lottery）\n")
	prompt.WriteString("4. confidence: 置信度（0~1 小数）\n\n")
	prompt.WriteString("输出格式示例：\n")
	prompt.WriteString(`{"trigger": "...", "lesson": "...", "apply_to": "...", "confidence": 0.8}`)

	return prompt.String()
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
		Trigger     string   `json:"trigger"`
		Lesson      string   `json:"lesson"`
		ApplyTo     string   `json:"apply_to"`
		Confidence  *float64 `json:"confidence"`
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
