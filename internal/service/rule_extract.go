package service

import (
	"regexp"
	"strconv"
	"strings"
)

var thresholdRe = regexp.MustCompile(`([0-9]{1,4})`)

// extractThresholdFromText 尝试从文本中提取门槛数值（如 80/100/120）。
// 返回 (threshold, ok)。
func extractThresholdFromText(text string) (int, bool) {
	s := strings.TrimSpace(text)
	if s == "" {
		return 0, false
	}
	m := thresholdRe.FindStringSubmatch(s)
	if len(m) < 2 {
		return 0, false
	}
	v, err := strconv.Atoi(m[1])
	if err != nil || v <= 0 {
		return 0, false
	}
	// 简单防御：阈值过大多半是噪声
	if v > 10000 {
		return 0, false
	}
	return v, true
}
