package service

import (
	"fmt"
	"math"

	"mem-test/internal/db"
	"mem-test/internal/model"
)

type GroupStats struct {
	N         int     `json:"n"`
	Correct   int     `json:"correct"`
	Incorrect int     `json:"incorrect"`
	ErrorRate float64 `json:"error_rate"`
	CI95Low   float64 `json:"ci95_low"`
	CI95High  float64 `json:"ci95_high"`
}

// ComputeRunStatsAndTests 论文级：只统计本 run_id，并做显著性检验/趋势检验
func ComputeRunStatsAndTests(runID uint, groups []string, trend map[string][]int) (map[string]GroupStats, map[string]interface{}, error) {
	stats := map[string]GroupStats{}
	tests := map[string]interface{}{}

	for _, g := range groups {
		var tasks []model.Task
		if err := db.DB.Where("run_id = ? AND group_type = ?", runID, g).Find(&tasks).Error; err != nil {
			return nil, nil, fmt.Errorf("查询任务失败: %w", err)
		}
		gs := calcGroupStats(tasks)
		stats[g] = gs
	}

	// 组间显著性：C vs A, C vs B
	if a, okA := stats["A"]; okA {
		if c, okC := stats["C"]; okC {
			p, z := twoPropZTest(a.Incorrect, a.N, c.Incorrect, c.N)
			tests["C_vs_A"] = map[string]interface{}{
				"p_value": p,
				"z":       z,
			}
		}
	}
	if b, okB := stats["B"]; okB {
		if c, okC := stats["C"]; okC {
			p, z := twoPropZTest(b.Incorrect, b.N, c.Incorrect, c.N)
			tests["C_vs_B"] = map[string]interface{}{
				"p_value": p,
				"z":       z,
			}
		}
	}

	// 趋势检验：C 前半 vs 后半（两比例检验）
	if cTrend, ok := trend["C"]; ok && len(cTrend) >= 10 {
		mid := len(cTrend) / 2
		firstN, firstBad := mid, sumInt(cTrend[:mid])
		lastN, lastBad := len(cTrend)-mid, sumInt(cTrend[mid:])
		p, z := twoPropZTest(firstBad, firstN, lastBad, lastN)
		tests["C_trend_first_vs_second_half"] = map[string]interface{}{
			"p_value":                p,
			"z":                      z,
			"first_half_error_rate":  float64(firstBad) / math.Max(float64(firstN), 1),
			"second_half_error_rate": float64(lastBad) / math.Max(float64(lastN), 1),
		}
	}

	return stats, tests, nil
}

func calcGroupStats(tasks []model.Task) GroupStats {
	gs := GroupStats{N: len(tasks)}
	for _, t := range tasks {
		if t.IsCorrect == nil {
			continue
		}
		if *t.IsCorrect {
			gs.Correct++
		} else {
			gs.Incorrect++
		}
	}

	// 只针对已判定的任务计算错误率和置信区间（排除 IsCorrect == nil）
	judged := gs.Correct + gs.Incorrect
	if judged > 0 {
		gs.ErrorRate = float64(gs.Incorrect) / float64(judged)
		low, high := wilsonCI(gs.Incorrect, judged, 1.96)
		gs.CI95Low, gs.CI95High = low, high
	}
	return gs
}

// Wilson score interval for proportion
func wilsonCI(k int, n int, z float64) (float64, float64) {
	if n == 0 {
		return 0, 0
	}
	p := float64(k) / float64(n)
	zz := z * z
	den := 1 + zz/float64(n)
	center := (p + zz/(2*float64(n))) / den
	half := (z / den) * math.Sqrt((p*(1-p)+zz/(4*float64(n)))/float64(n))
	low := math.Max(0, center-half)
	high := math.Min(1, center+half)
	return low, high
}

// two-proportion z-test (two-sided)
func twoPropZTest(x1, n1, x2, n2 int) (pValue float64, z float64) {
	if n1 == 0 || n2 == 0 {
		return 1, 0
	}
	p1 := float64(x1) / float64(n1)
	p2 := float64(x2) / float64(n2)
	p := float64(x1+x2) / float64(n1+n2)
	se := math.Sqrt(p * (1 - p) * (1/float64(n1) + 1/float64(n2)))
	if se == 0 {
		return 1, 0
	}
	z = (p2 - p1) / se
	// two-sided p-value
	pValue = 2 * (1 - normCDF(math.Abs(z)))
	return pValue, z
}

// standard normal CDF approximation via erf
func normCDF(x float64) float64 {
	return 0.5 * (1 + math.Erf(x/math.Sqrt2))
}

func sumInt(a []int) int {
	s := 0
	for _, v := range a {
		s += v
	}
	return s
}
