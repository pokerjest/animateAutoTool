package parser

import (
	"regexp"
	"strconv"
	"strings"
)

// ParseSeason 尝试从番剧标题或文件夹名中解析季度号
func ParseSeason(title string) int {
	title = strings.ToLower(title)

	// 1. 匹配 "Season 2", "S2", "S02"
	sRegex := regexp.MustCompile(`s(?:eason)?\s?(\d+)`)
	if match := sRegex.FindStringSubmatch(title); len(match) > 1 {
		if val, err := strconv.Atoi(match[1]); err == nil {
			return val
		}
	}

	// 2. 匹配 "第2季", "第2期", "2nd season", "part 2"
	numRegex := regexp.MustCompile(`(?:第|part\s?|(\d+)(?:nd|rd|th|st)?\s?season)\s?(\d+)`)
	if match := numRegex.FindStringSubmatch(title); len(match) > 1 {
		// handle "第2季" type
		if match[2] != "" {
			if val, err := strconv.Atoi(match[2]); err == nil {
				return val
			}
		}
	}

	// 3. 简单的 "标题 2"
	tailRegex := regexp.MustCompile(`\s(\d+)$`)
	if match := tailRegex.FindStringSubmatch(title); len(match) > 1 {
		if val, err := strconv.Atoi(match[1]); err == nil {
			return val
		}
	}

	// 4. 罗马数字识别 (II, III, IV, V)
	romanMap := map[string]int{
		" ii":  2,
		" iii": 3,
		" iv":  4,
		" v":   5,
		" vi":  6,
	}
	for suffix, val := range romanMap {
		if strings.HasSuffix(title, suffix) {
			return val
		}
	}

	// 5. 中文数字识别 "第二季"
	cnSeasonMap := map[string]int{
		"第一季": 1, "第二季": 2, "第三季": 3, "第四季": 4, "第五季": 5,
		"第一期": 1, "第二期": 2, "第三期": 3, "第四期": 4, "第五期": 5,
	}
	for key, val := range cnSeasonMap {
		if strings.Contains(title, key) {
			return val
		}
	}

	return 1 // 默认第一季
}
