package service

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	digitsRe     = regexp.MustCompile(`[0-9]+`)
	whitespaceRe = regexp.MustCompile(`\s+`)
)

func normalizeTriggerKey(trigger string) string {
	s := strings.TrimSpace(trigger)
	s = strings.ToLower(s)
	// 去掉数字：把“积分<100/120”归并到同一类
	s = digitsRe.ReplaceAllString(s, "")
	// 压缩空白
	s = whitespaceRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	// 限制长度，避免异常长 trigger 影响索引/存储
	if len(s) > 180 {
		s = s[:180]
	}
	if s == "" {
		return "通用"
	}
	return s
}

func ParseMemoryIDs(memoryIDs string) []uint {
	memoryIDs = strings.TrimSpace(memoryIDs)
	if memoryIDs == "" {
		return nil
	}
	parts := strings.Split(memoryIDs, ",")
	out := make([]uint, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			continue
		}
		if v == 0 {
			continue
		}
		out = append(out, uint(v))
	}
	return out
}
