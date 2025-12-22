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

	// 趋势：D 前半 vs 后半（跨 run 复用全局中期记忆池，期望更稳）
	dTrend, okDTrend := trend["D"]
	if okDTrend && len(dTrend) >= 10 {
		first := avgInt(dTrend[:len(dTrend)/2])
		last := avgInt(dTrend[len(dTrend)/2:])
		out["metrics"].(map[string]interface{})["D_first_half_error_rate"] = first
		out["metrics"].(map[string]interface{})["D_second_half_error_rate"] = last
		if last < first {
			out["claims"] = append(out["claims"].([]string), "D组错误率后半段低于前半段，表明“短期纠错 + 全局中期记忆池固化”的学习链路在本次run内发挥作用。")
			if out["verdict"] == "insufficient_data" {
				out["verdict"] = "learning_observed"
			}
		}
	} else {
		out["caveats"] = append(out["caveats"].([]string), "D组样本量不足，无法判断学习趋势（建议每组>=30）。")
	}

	// 趋势：E 前半 vs 后半（两阶段推理 + 记忆门控 + 验证固化）
	eTrend, okETrend := trend["E"]
	if okETrend && len(eTrend) >= 10 {
		first := avgInt(eTrend[:len(eTrend)/2])
		last := avgInt(eTrend[len(eTrend)/2:])
		out["metrics"].(map[string]interface{})["E_first_half_error_rate"] = first
		out["metrics"].(map[string]interface{})["E_second_half_error_rate"] = last
		if last < first {
			out["claims"] = append(out["claims"].([]string), "E组错误率后半段低于前半段，表明“自检纠错 + 记忆门控 + 验证固化”在本次run内进一步提升稳定性。")
			if out["verdict"] == "insufficient_data" {
				out["verdict"] = "learning_observed"
			}
		}
	} else {
		out["caveats"] = append(out["caveats"].([]string), "E组样本量不足，无法判断学习趋势（建议每组>=30）。")
	}

	// 趋势：F 前半 vs 后半（变更检测 + 候选竞争 + 自检纠错）
	fTrend, okFTrend := trend["F"]
	if okFTrend && len(fTrend) >= 10 {
		first := avgInt(fTrend[:len(fTrend)/2])
		last := avgInt(fTrend[len(fTrend)/2:])
		out["metrics"].(map[string]interface{})["F_first_half_error_rate"] = first
		out["metrics"].(map[string]interface{})["F_second_half_error_rate"] = last
		if last < first {
			out["claims"] = append(out["claims"].([]string), "F组错误率后半段低于前半段，表明“变更检测 + 候选竞争 + 自检纠错”在本次run内提升了规则切换适应能力。")
			if out["verdict"] == "insufficient_data" {
				out["verdict"] = "learning_observed"
			}
		}
	} else {
		out["caveats"] = append(out["caveats"].([]string), "F组样本量不足，无法判断学习趋势（建议每组>=30）。")
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

	// 组间：D vs A/B/C（如果存在 D 组）
	d, okD := stats["D"]
	if okA && okB && okC && okD {
		out["metrics"].(map[string]interface{})["D_error_rate"] = d.ErrorRate
		out["metrics"].(map[string]interface{})["D_ci95"] = []float64{d.CI95Low, d.CI95High}
		out["metrics"].(map[string]interface{})["p_D_vs_A"] = getPValue(tests, "D_vs_A")
		out["metrics"].(map[string]interface{})["p_D_vs_B"] = getPValue(tests, "D_vs_B")
		out["metrics"].(map[string]interface{})["p_D_vs_C"] = getPValue(tests, "D_vs_C")

		pDA := getPValue(tests, "D_vs_A")
		pDB := getPValue(tests, "D_vs_B")
		pDC := getPValue(tests, "D_vs_C")
		if d.ErrorRate < a.ErrorRate && d.ErrorRate < b.ErrorRate && d.ErrorRate < c.ErrorRate && pDA < 0.05 && pDB < 0.05 && pDC < 0.05 {
			out["claims"] = append(out["claims"].([]string), "在本次run内，D组错误率显著低于A/B/C组（p<0.05），支持“跨run可复用的中期记忆池 + 短期纠错信号”进一步提升稳定性。")
			if out["verdict"] == "insufficient_data" {
				out["verdict"] = "memory_effect_supported"
			}
		}
	}

	// 组间：E vs A/B/C/D（如果存在 E 组）
	e, okE := stats["E"]
	if okA && okB && okC && okD && okE {
		out["metrics"].(map[string]interface{})["E_error_rate"] = e.ErrorRate
		out["metrics"].(map[string]interface{})["E_ci95"] = []float64{e.CI95Low, e.CI95High}
		out["metrics"].(map[string]interface{})["p_E_vs_A"] = getPValue(tests, "E_vs_A")
		out["metrics"].(map[string]interface{})["p_E_vs_B"] = getPValue(tests, "E_vs_B")
		out["metrics"].(map[string]interface{})["p_E_vs_C"] = getPValue(tests, "E_vs_C")
		out["metrics"].(map[string]interface{})["p_E_vs_D"] = getPValue(tests, "E_vs_D")

		pEA := getPValue(tests, "E_vs_A")
		pEB := getPValue(tests, "E_vs_B")
		pEC := getPValue(tests, "E_vs_C")
		pED := getPValue(tests, "E_vs_D")
		if e.ErrorRate < a.ErrorRate && e.ErrorRate < b.ErrorRate && e.ErrorRate < c.ErrorRate && e.ErrorRate < d.ErrorRate &&
			pEA < 0.05 && pEB < 0.05 && pEC < 0.05 && pED < 0.05 {
			out["claims"] = append(out["claims"].([]string), "在本次run内，E组错误率显著低于A/B/C/D组（p<0.05），支持“验证固化 + 自检纠错 + MemOS门控召回”优于 D 组策略。")
			if out["verdict"] == "insufficient_data" {
				out["verdict"] = "memory_effect_supported"
			}
		}
	}

	// 组间：F vs A/B/C/D/E（如果存在 F 组）
	f, okF := stats["F"]
	if okA && okB && okC && okD && okE && okF {
		out["metrics"].(map[string]interface{})["F_error_rate"] = f.ErrorRate
		out["metrics"].(map[string]interface{})["F_ci95"] = []float64{f.CI95Low, f.CI95High}
		out["metrics"].(map[string]interface{})["p_F_vs_A"] = getPValue(tests, "F_vs_A")
		out["metrics"].(map[string]interface{})["p_F_vs_B"] = getPValue(tests, "F_vs_B")
		out["metrics"].(map[string]interface{})["p_F_vs_C"] = getPValue(tests, "F_vs_C")
		out["metrics"].(map[string]interface{})["p_F_vs_D"] = getPValue(tests, "F_vs_D")
		out["metrics"].(map[string]interface{})["p_F_vs_E"] = getPValue(tests, "F_vs_E")

		pFA := getPValue(tests, "F_vs_A")
		pFB := getPValue(tests, "F_vs_B")
		pFC := getPValue(tests, "F_vs_C")
		pFD := getPValue(tests, "F_vs_D")
		pFE := getPValue(tests, "F_vs_E")
		// 只要显著优于 E，同时也显著优于 D 与其他组，就给出强结论
		if f.ErrorRate < e.ErrorRate && f.ErrorRate < d.ErrorRate && f.ErrorRate < c.ErrorRate && f.ErrorRate < b.ErrorRate && f.ErrorRate < a.ErrorRate &&
			pFA < 0.05 && pFB < 0.05 && pFC < 0.05 && pFD < 0.05 && pFE < 0.05 {
			out["claims"] = append(out["claims"].([]string), "在本次run内，F组错误率显著低于A/B/C/D/E组（p<0.05），支持“变更检测 + 候选竞争”在高频规则切换下进一步优于 E 组策略。")
			if out["verdict"] == "insufficient_data" {
				out["verdict"] = "memory_effect_supported"
			}
		}
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
