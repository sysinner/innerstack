package inutil

import "fmt"

func FormatUptime(v int64) string {
	days := v / 86400
	hours := (v % 86400) / 3600
	minutes := (v % 3600) / 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
