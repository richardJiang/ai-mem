package service

import "math"

// GenerateConclusionFromStats 根据 run 内统计与检验结果生成结论（论文级更稳；仍为工程简化版）
func GenerateConclusionFromStats(stats map[string]GroupStats, tests map[string]interface{}, trend map[string][]int) map[string]interface{} {
	out := map[string]interface{}{
		"verdict": "insufficient_data",
		"claims":  []string{},
		"metrics": map[string]interface{}{},
		"caveats": []string{},
	}

	// 趋势：C 前半 vs 后半
	cTrend, ok := trend["C"]
	if ok && len(cTrend) >= 10 {
		first := avgInt(cTrend[:len(cTrend)/2])
		last := avgInt(cTrend[len(cTrend)/2:])
		out["metrics"].(map[string]interface{})["C_first_half_error_rate"] = first
		out["metrics"].(map[string]interface{})["C_second_half_error_rate"] = last
		if last < first {
			out["claims"] = append(out["claims"].([]string), "C组在同一批重复任务中错误率后半段低于前半段，存在test-time learning迹象（反馈→反思→记忆→行为变化）。")
			out["verdict"] = "learning_observed"
		} else {
			out["claims"] = append(out["claims"].([]string), "C组错误率未随轮次下降，未观察到稳定的学习趋势（可能样本不足、提示词不稳定或workflow输出不可判定）。")
			out["verdict"] = "learning_not_observed"
		}
	} else {
		out["caveats"] = append(out["caveats"].([]string), "C组样本量不足，无法判断学习趋势（建议每组>=30）。")
	}

	// 组间：C vs A, C vs B（需要显著性）
	a, okA := stats["A"]
	b, okB := stats["B"]
	c, okC := stats["C"]
	if okA && okB && okC {
		out["metrics"].(map[string]interface{})["A_error_rate"] = a.ErrorRate
		out["metrics"].(map[string]interface{})["B_error_rate"] = b.ErrorRate
		out["metrics"].(map[string]interface{})["C_error_rate"] = c.ErrorRate
		out["metrics"].(map[string]interface{})["A_ci95"] = []float64{a.CI95Low, a.CI95High}
		out["metrics"].(map[string]interface{})["B_ci95"] = []float64{b.CI95Low, b.CI95High}
		out["metrics"].(map[string]interface{})["C_ci95"] = []float64{c.CI95Low, c.CI95High}

		pCA := getPValue(tests, "C_vs_A")
		pCB := getPValue(tests, "C_vs_B")
		out["metrics"].(map[string]interface{})["p_C_vs_A"] = pCA
		out["metrics"].(map[string]interface{})["p_C_vs_B"] = pCB

		if c.ErrorRate < a.ErrorRate && c.ErrorRate < b.ErrorRate && pCA < 0.05 && pCB < 0.05 {
			out["claims"] = append(out["claims"].([]string), "在本次run内，C组错误率显著低于A组与B组（p<0.05），支持“反思→抽象规则记忆”优于“无记忆/仅日志记忆”。")
			rel := (a.ErrorRate - c.ErrorRate) / math.Max(a.ErrorRate, 1e-9)
			out["metrics"].(map[string]interface{})["C_vs_A_relative_reduction"] = rel
			if out["verdict"] == "insufficient_data" {
				out["verdict"] = "memory_effect_supported"
			}
		} else {
			out["caveats"] = append(out["caveats"].([]string), "组间总体错误率未呈现 C < B < A 的理想形态；可能需要更多样本或更强的错误反馈/更严格的判题。")
		}
	} else {
		out["caveats"] = append(out["caveats"].([]string), "缺少A/B/C组的run内统计，无法做组间显著性对比。")
	}

	return out
}

func avgInt(flags []int) float64 {
	if len(flags) == 0 {
		return 0
	}
	sum := 0
	for _, v := range flags {
		sum += v
	}
	return float64(sum) / float64(len(flags))
}

func getPValue(tests map[string]interface{}, key string) float64 {
	v, ok := tests[key]
	if !ok {
		return 1
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return 1
	}
	p, ok := m["p_value"]
	if !ok {
		return 1
	}
	switch t := p.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	default:
		return 1
	}
}
