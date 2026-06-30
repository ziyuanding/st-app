package main

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"os/exec"
	"strconv"
	"strings"
)

// 全局样式
var (
	StyleHeader  = lipgloss.NewStyle().Bold(true).Align(lipgloss.Center)
	StyleRow     = lipgloss.NewStyle().Align(lipgloss.Center)
	StyleDivider = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	// 补充缺失的 LSP 分区标题样式
	StyleLspPartTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")).MarginBottom(1)
	// sq 视图专属颜色
	StyleGreen  = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00"))
	StyleBlue   = lipgloss.NewStyle().Foreground(lipgloss.Color("#1890FF"))
	StyleYellow = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00"))
	StyleRed    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")) // 新增红色，用于失败/取消的任务
	StyleDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	StyleItalic = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true)
)

func getColorByPercentage(pct int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	colors := []string{"#00FF00", "#33FF00", "#66FF00", "#99FF00", "#CCFF00", "#FFFF00", "#FFCC00", "#FF9900", "#FF6600", "#FF3300", "#FF0000"}
	index := pct / 10
	if index > 10 {
		index = 10
	}
	return colors[index]
}

func parseGPUString(g string) int {
	if g == "(null)" || g == "" || g == "none" || !strings.Contains(strings.ToLower(g), "gpu") {
		return 0
	}
	parts := strings.Split(g, ":")
	if len(parts) > 0 {
		val, err := strconv.Atoi(parts[len(parts)-1])
		if err == nil {
			return val
		}
	}
	return 0
}

func parseTRESForGPU(tres string) int {
	tres = strings.ToLower(tres)
	for _, item := range strings.Split(tres, ",") {
		if strings.Contains(item, "gpu") {
			item = strings.ReplaceAll(item, "=", ":")
			parts := strings.Split(item, ":")
			if len(parts) > 0 {
				val, err := strconv.Atoi(parts[len(parts)-1])
				if err == nil {
					return val
				}
			}
		}
	}
	return 0
}

func parseJobMemToMB(m string) int {
	m = strings.ToUpper(strings.TrimSpace(m))
	if len(m) == 0 || m == "UNLIMITED" {
		return 0
	}

	// 兼容 sacct：去掉末尾表示每核心(C)或每节点(N)的后缀
	m = strings.TrimRight(m, "CN")

	multiplier := 1.0
	if strings.HasSuffix(m, "G") {
		multiplier = 1024.0
		m = strings.TrimSuffix(m, "G")
	} else if strings.HasSuffix(m, "M") {
		multiplier = 1.0
		m = strings.TrimSuffix(m, "M")
	} else if strings.HasSuffix(m, "T") {
		multiplier = 1024.0 * 1024.0
		m = strings.TrimSuffix(m, "T")
	}

	val, _ := strconv.ParseFloat(m, 64)
	return int(val * multiplier)
}

func formatDependency(dep string) string {
	dep = strings.TrimSpace(dep)
	if dep == "" || dep == "None" || dep == "(null)" || dep == "N/A" {
		return ""
	}
	return dep
}

func expandNodelist(nodelist string) []string {
	out, err := exec.Command("bash", "-c", fmt.Sprintf("scontrol show hostnames '%s'", nodelist)).Output()
	if err != nil {
		return []string{nodelist}
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

// 字符串截断辅助函数 (防止过长破坏窄屏排版)
func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max-2] + ".."
	}
	return s
}
