package platform

import "strings"

func boundedLines(value string, max int) []string {
	result := []string{}
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 512 {
			line = line[:512]
		}
		result = append(result, line)
		if len(result) >= max {
			break
		}
	}
	return result
}

func firstLine(value string) string {
	lines := boundedLines(value, 1)
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}
