package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mem-test/internal/model"
)

func RenderConclusionMarkdown(run *model.ExperimentRun, result *ExperimentRunResult) string {
	var b strings.Builder
	b.WriteString("# 记忆实验结论（论文级）\n\n")
	b.WriteString(fmt.Sprintf("- run_id: %d\n", run.ID))
	b.WriteString(fmt.Sprintf("- task_type: %s\n", run.TaskType))
	b.WriteString(fmt.Sprintf("- runs_per_group: %d\n", run.RunsPerGroup))
	b.WriteString(fmt.Sprintf("- seed: %d\n", run.Seed))
	b.WriteString(fmt.Sprintf("- created_at: %s\n\n", run.CreatedAt.Format(time.RFC3339)))

	b.WriteString("## 组内统计（仅本次 run）\n\n")
	b.WriteString("| 组别 | N | Incorrect | ErrorRate | CI95 |\n")
	b.WriteString("| --- | ---: | ---: | ---: | --- |\n")
	for _, g := range result.Groups {
		s, ok := result.Stats[g]
		if !ok {
			continue
		}
		b.WriteString(fmt.Sprintf("| %s | %d | %d | %.3f | [%.3f, %.3f] |\n",
			g, s.N, s.Incorrect, s.ErrorRate, s.CI95Low, s.CI95High))
	}
	b.WriteString("\n")

	b.WriteString("## 显著性检验\n\n")
	if len(result.Tests) == 0 {
		b.WriteString("- 无（可能样本不足或统计失败）\n\n")
	} else {
		j, _ := json.MarshalIndent(result.Tests, "", "  ")
		b.WriteString("```json\n")
		b.WriteString(string(j))
		b.WriteString("\n```\n\n")
	}

	b.WriteString("## 自动结论\n\n")
	if verdict, ok := result.Conclusion["verdict"]; ok {
		b.WriteString(fmt.Sprintf("- verdict: %v\n", verdict))
	}
	if claims, ok := result.Conclusion["claims"].([]string); ok && len(claims) > 0 {
		b.WriteString("\n### 主要论断\n\n")
		for _, c := range claims {
			b.WriteString(fmt.Sprintf("- %s\n", c))
		}
	}
	if caveats, ok := result.Conclusion["caveats"].([]string); ok && len(caveats) > 0 {
		b.WriteString("\n### 注意事项/局限\n\n")
		for _, c := range caveats {
			b.WriteString(fmt.Sprintf("- %s\n", c))
		}
	}

	if len(result.Errors) > 0 {
		b.WriteString("\n## 执行错误（如有）\n\n")
		max := len(result.Errors)
		if max > 20 {
			max = 20
		}
		for i := 0; i < max; i++ {
			b.WriteString(fmt.Sprintf("- %s\n", result.Errors[i]))
		}
		if len(result.Errors) > max {
			b.WriteString(fmt.Sprintf("- ...(剩余 %d 条省略)\n", len(result.Errors)-max))
		}
	}
	return b.String()
}
