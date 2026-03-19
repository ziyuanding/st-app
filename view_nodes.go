package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"golang.org/x/term"
)

func runDetailedMode(data GlobalData, targetPart string) {
	filteredNodes := make(map[string]*Node)
	for name, node := range data.Nodes {
		if node.Part == targetPart {
			filteredNodes[name] = node
		}
	}

	if len(filteredNodes) == 0 {
		fmt.Printf("在分区 [%s] 中没有找到节点。\n", targetPart)
		return
	}

	renderDetailedGrid(filteredNodes)
}

func renderDetailedGrid(nodes map[string]*Node) {
	keys := make([]string, 0, len(nodes))
	for k := range nodes {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	colWidths := []int{4, 4, 14, 14, 15, 4, 8, 8, 7}
	for _, node := range nodes {
		if lipgloss.Width(node.Name) > colWidths[0] { colWidths[0] = lipgloss.Width(node.Name) }
		if lipgloss.Width(node.Part) > colWidths[1] { colWidths[1] = lipgloss.Width(node.Part) }

		for _, j := range node.Jobs {
			if lipgloss.Width(j.User) > colWidths[5] { colWidths[5] = lipgloss.Width(j.User) }
			if lipgloss.Width(j.Name) > colWidths[6] { colWidths[6] = lipgloss.Width(j.Name) }
			
			// 修复：将 int 转换为 string 计算宽度
			jobCPUsStr := fmt.Sprintf("%d", j.CPUs)
			jobMemStr := fmt.Sprintf("%dG", j.MemGB)
			if lipgloss.Width(jobCPUsStr) > colWidths[7] { colWidths[7] = lipgloss.Width(jobCPUsStr) }
			if lipgloss.Width(jobMemStr) > colWidths[8] { colWidths[8] = lipgloss.Width(jobMemStr) }
		}
	}

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(StyleDivider).
		Headers("NODE", "PART", "CPUs", "GPUs", "RAM", "USER", "JOB NAME", "JOB CPUs", "JOB RAM").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == 0 { return StyleHeader }
			return StyleRow
		})

	for i, nodeName := range keys {
		node := nodes[nodeName]

		cpuPct, ramPct, gpuPct := 0, 0, 0
		if node.TotalCPUs > 0 { cpuPct = (node.AllocCPUs * 100) / node.TotalCPUs }
		if node.TotalMemMB > 0 { ramPct = (node.AllocMemMB * 100) / node.TotalMemMB }
		if node.TotalGPUs > 0 { gpuPct = (node.AllocGPUs * 100) / node.TotalGPUs }

		totalRAMGB := float64(node.TotalMemMB) / 1024.0

		cpuCell := lipgloss.NewStyle().Foreground(lipgloss.Color(getColorByPercentage(cpuPct))).Render(fmt.Sprintf("%d\n(%d%% in use)", node.TotalCPUs, cpuPct))
		gpuCell := lipgloss.NewStyle().Foreground(lipgloss.Color(getColorByPercentage(gpuPct))).Render(fmt.Sprintf("%d\n(%d%% in use)", node.TotalGPUs, gpuPct))
		ramCell := lipgloss.NewStyle().Foreground(lipgloss.Color(getColorByPercentage(ramPct))).Render(fmt.Sprintf("%.1f GB\n(%d%% in use)", totalRAMGB, ramPct))

		if len(node.Jobs) == 0 {
			t.Row(node.Name, node.Part, cpuCell, gpuCell, ramCell, "-", "-", "-", "-")
		} else {
			for jIdx, j := range node.Jobs {
				// 修复：将 int 转换为 string 填入表格
				jobCPUsStr := fmt.Sprintf("%d", j.CPUs)
				jobMemStr := fmt.Sprintf("%dG", j.MemGB)
				
				if jIdx == 0 {
					t.Row(node.Name, node.Part, cpuCell, gpuCell, ramCell, j.User, j.Name, jobCPUsStr, jobMemStr)
				} else {
					t.Row("", "", "", "", "", j.User, j.Name, jobCPUsStr, jobMemStr)
				}
			}
		}

		if i < len(keys)-1 {
			t.Row(
				StyleDivider.Render(strings.Repeat("─", colWidths[0])),
				StyleDivider.Render(strings.Repeat("─", colWidths[1])),
				StyleDivider.Render(strings.Repeat("─", 14)), StyleDivider.Render(strings.Repeat("─", 14)), StyleDivider.Render(strings.Repeat("─", 15)),
				StyleDivider.Render(strings.Repeat("─", colWidths[5])), StyleDivider.Render(strings.Repeat("─", colWidths[6])),
				StyleDivider.Render(strings.Repeat("─", colWidths[7])), StyleDivider.Render(strings.Repeat("─", colWidths[8])),
			)
		}
	}
	fmt.Println(t.Render())
}

func runLspMode(data GlobalData) {
	partToNodes := make(map[string][]string)
	for name, node := range data.Nodes {
		partToNodes[node.Part] = append(partToNodes[node.Part], name)
	}

	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil { width = 120 }

	cellWidth := 34
	nodesPerRow := (width - 2) / cellWidth
	nodesPerRow = nodesPerRow - 1
	if nodesPerRow < 1 { nodesPerRow = 1 }

	partitions := make([]string, 0, len(partToNodes))
	for p := range partToNodes { partitions = append(partitions, p) }
	sort.Strings(partitions)

	for i, partName := range partitions {
		nodeNames := partToNodes[partName]
		sort.Strings(nodeNames)

		fmt.Println(StyleLspPartTitle.Render(fmt.Sprintf("Partition: %s (Total Nodes: %d)", partName, len(nodeNames))))

		t := table.New().
			Border(lipgloss.NormalBorder()). BorderRow(false). BorderColumn(true).
			BorderStyle(StyleDivider).
			StyleFunc(func(row, col int) lipgloss.Style { return lipgloss.NewStyle().Padding(0, 1) })

		for rowIdx := 0; rowIdx*nodesPerRow < len(nodeNames); rowIdx++ {
			rowCells := make([]string, 0)
			for colIdx := 0; colIdx < nodesPerRow; colIdx++ {
				nodeIdx := rowIdx*nodesPerRow + colIdx
				if nodeIdx < len(nodeNames) {
					rowCells = append(rowCells, renderLspCellContent(data.Nodes[nodeNames[nodeIdx]]))
				} else {
					rowCells = append(rowCells, "")
				}
			}
			t.Row(rowCells...)
		}
		fmt.Println(t.Render())
		if i < len(partitions)-1 { fmt.Println() }
	}
}

func renderLspCellContent(node *Node) string {
	cpuPct, ramPct, gpuPct := 0, 0, 0
	if node.TotalCPUs > 0 { cpuPct = (node.AllocCPUs * 100) / node.TotalCPUs }
	if node.TotalMemMB > 0 { ramPct = (node.AllocMemMB * 100) / node.TotalMemMB }
	if node.TotalGPUs > 0 { gpuPct = (node.AllocGPUs * 100) / node.TotalGPUs }

	cText := lipgloss.NewStyle().Foreground(lipgloss.Color(getColorByPercentage(cpuPct))).Render(fmt.Sprintf("C:%3d%%", cpuPct))
	gText := lipgloss.NewStyle().Foreground(lipgloss.Color(getColorByPercentage(gpuPct))).Render(fmt.Sprintf("G:%3d%%", gpuPct))
	rText := lipgloss.NewStyle().Foreground(lipgloss.Color(getColorByPercentage(ramPct))).Render(fmt.Sprintf("R:%3d%%", ramPct))

	return fmt.Sprintf("%-10s %s  %s  %s", node.Name, cText, gText, rText)
}