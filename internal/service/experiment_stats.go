package service

import "mem-test/internal/model"

type ModeCurve struct {
	RunID     uint                  `json:"run_id"`
	RuleMode  string                `json:"rule_mode"`
	Rounds    int                   `json:"rounds"`
	Threshold []int                 `json:"thresholds"`
	Groups    []string              `json:"groups"`
	Overall   map[string]GroupStats `json:"overall"`
	Curves    map[string]Curves     `json:"curves"`
	// C 组调整记忆的试错次数：每次规则变更后的“直到首次判对”的错误次数
	TrialAndError TrialAndError `json:"trial_and_error"`

	// 各组首次出错轮次（-1 表示该组无错误）
	FirstErrorRound map[string]int `json:"first_error_round"`

	// C 组记忆开始变更轮次（-1 表示未触发记忆变更）
	MemoryChangeStartRound int `json:"memory_change_start_round"`

	// C 组每轮记忆变更次数（当前定义：该轮 C 组判错次数；通常为 0/1）
	MemoryChangesPerRound []int `json:"memory_changes_per_round"`
}

type Curves struct {
	CumulativeAccuracy []float64 `json:"cumulative_accuracy"`
	CumulativeError    []float64 `json:"cumulative_error"`
}

type TrialAndError struct {
	ChangePoints []int   `json:"change_points"` // round index
	Attempts     []int   `json:"attempts"`      // per change point
	AvgAttempts  float64 `json:"avg_attempts"`
	MaxAttempts  int     `json:"max_attempts"`
}

func BuildCumulativeCurves(flags []int, rounds int) Curves {
	acc := make([]float64, rounds)
	err := make([]float64, rounds)
	correct := 0
	for i := 0; i < rounds; i++ {
		bad := 0
		if i < len(flags) {
			bad = flags[i]
		} else {
			bad = 1
		}
		if bad == 0 {
			correct++
		}
		acc[i] = float64(correct) / float64(i+1)
		err[i] = 1 - acc[i]
	}
	return Curves{CumulativeAccuracy: acc, CumulativeError: err}
}

func ComputeTrialAndErrorC(ruleVersions []int, cFlags []int) TrialAndError {
	var changePoints []int
	var attempts []int

	if len(ruleVersions) == 0 {
		return TrialAndError{}
	}
	prev := ruleVersions[0]
	for i := 1; i < len(ruleVersions); i++ {
		if ruleVersions[i] != prev {
			changePoints = append(changePoints, i)
			prev = ruleVersions[i]
		}
	}

	// 对每个 change point：数从该轮开始到首次正确的错误次数（C组）
	maxA := 0
	sumA := 0
	for _, cp := range changePoints {
		a := 0
		found := false
		for i := cp; i < len(cFlags); i++ {
			if i >= len(ruleVersions) || ruleVersions[i] != ruleVersions[cp] {
				break
			}
			if cFlags[i] == 1 {
				a++
			} else {
				found = true
				break
			}
		}
		if !found {
			// 若该段没有判对，保持累计错误数
		}
		attempts = append(attempts, a)
		sumA += a
		if a > maxA {
			maxA = a
		}
	}
	avg := 0.0
	if len(attempts) > 0 {
		avg = float64(sumA) / float64(len(attempts))
	}
	return TrialAndError{
		ChangePoints: changePoints,
		Attempts:     attempts,
		AvgAttempts:  avg,
		MaxAttempts:  maxA,
	}
}

func FirstErrorRound(flags []int) int {
	for i, v := range flags {
		if v == 1 {
			return i
		}
	}
	return -1
}

func ExtractRoundFlags(tasks []model.Task, rounds int) (flags []int, thresholds []int, ruleVersions []int) {
	flags = make([]int, rounds)
	thresholds = make([]int, rounds)
	ruleVersions = make([]int, rounds)
	for i := 0; i < rounds; i++ {
		flags[i] = 1 // 默认缺失当作错误
	}
	for _, t := range tasks {
		if t.Round < 0 || t.Round >= rounds {
			continue
		}
		if t.IsCorrect != nil && *t.IsCorrect {
			flags[t.Round] = 0
		} else if t.IsCorrect != nil && !*t.IsCorrect {
			flags[t.Round] = 1
		}
		thresholds[t.Round] = t.RuleThreshold
		if t.RuleVersion > 0 {
			ruleVersions[t.Round] = t.RuleVersion
		}
	}
	// 填充 ruleVersions 默认值
	if rounds > 0 && ruleVersions[0] == 0 {
		ruleVersions[0] = 1
	}
	for i := 1; i < rounds; i++ {
		if ruleVersions[i] == 0 {
			ruleVersions[i] = ruleVersions[i-1]
		}
	}
	return flags, thresholds, ruleVersions
}
