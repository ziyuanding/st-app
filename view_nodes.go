package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"golang.org/x/term"
)

func runDetailedMode(targetPart string) {
	interactive := term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stdin.Fd()))
	if interactive {
		fmt.Print("正在拉取所有分区的节点状态...")
	}

	allData := loadDetailedAllViewData(targetPart)
	if interactive {
		fmt.Print("\r\x1b[2K")
	}

	if len(allData.partitions) == 0 {
		fmt.Println("没有找到任何分区节点。")
		return
	}

	render := func(width, height, partIndex, page int) sqRenderResult {
		return renderDetailedPartition(allData, width, height, partIndex, page)
	}
	refresh := func() {
		allData = loadDetailedAllViewData(targetPart)
	}
	partCount := func() int {
		return len(allData.partitions)
	}

	if !interactive {
		width, _, err := term.GetSize(int(os.Stdout.Fd()))
		if err != nil {
			width = 120
		}
		fmt.Print(render(width, 0, allData.initialIndex, 0).text)
		return
	}

	runDetailedInteractive(render, refresh, partCount, allData.initialIndex)
}

type detailedAllViewData struct {
	partitions    []string
	views         map[string]detailedViewData
	initialIndex  int
	targetInitial string
}

type detailedViewData struct {
	targetPart      string
	partitionIndex  int
	totalPartitions int
	nodeCount       int
	jobCount        int
	rows            []detailedRow
}

type detailedRow struct {
	node     *Node
	job      *Job
	showNode bool
}

type detailedRenderMode struct {
	compact bool
}

type detailedColumn struct {
	header   string
	minWidth int
	maxWidth int
	dropRank int
	align    lipgloss.Position
	value    func(detailedRow, detailedRenderMode) string
	style    func(detailedRow, string) string
}

func loadDetailedAllViewData(targetPart string) detailedAllViewData {
	data := getGlobalSlurmData()
	visiblePartitions := getVisibleNodePartitions()
	partNodes := make(map[string]map[string]*Node)
	for name, node := range data.Nodes {
		if len(visiblePartitions) > 0 && !visiblePartitions[node.Part] {
			continue
		}
		if _, ok := partNodes[node.Part]; !ok {
			partNodes[node.Part] = make(map[string]*Node)
		}
		partNodes[node.Part][name] = node
	}

	partitions := make([]string, 0, len(partNodes))
	for part := range partNodes {
		partitions = append(partitions, part)
	}
	sort.Strings(partitions)

	initialIndex := 0
	if targetPart != "" {
		for i, part := range partitions {
			if part == targetPart {
				initialIndex = i
				break
			}
		}
	}

	allData := detailedAllViewData{
		partitions:    partitions,
		views:         make(map[string]detailedViewData),
		initialIndex:  initialIndex,
		targetInitial: targetPart,
	}
	for i, part := range partitions {
		view := buildDetailedViewData(partNodes[part], part)
		view.partitionIndex = i
		view.totalPartitions = len(partitions)
		allData.views[part] = view
	}
	return allData
}

func buildDetailedViewData(nodes map[string]*Node, targetPart string) detailedViewData {
	viewData := detailedViewData{targetPart: targetPart, nodeCount: len(nodes)}
	keys := make([]string, 0, len(nodes))
	for k := range nodes {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, nodeName := range keys {
		node := nodes[nodeName]
		if len(node.Jobs) == 0 {
			viewData.rows = append(viewData.rows, detailedRow{node: node, showNode: true})
			continue
		}

		for i := range node.Jobs {
			job := &node.Jobs[i]
			viewData.jobCount++
			viewData.rows = append(viewData.rows, detailedRow{node: node, job: job, showNode: i == 0})
		}
	}

	return viewData
}

func renderDetailedPartition(allData detailedAllViewData, width, height, partIndex, page int) sqRenderResult {
	if len(allData.partitions) == 0 {
		return sqRenderResult{text: "没有找到任何分区节点。\n", page: 0, totalPages: 1}
	}
	if partIndex < 0 {
		partIndex = 0
	}
	if partIndex >= len(allData.partitions) {
		partIndex = len(allData.partitions) - 1
	}
	part := allData.partitions[partIndex]
	data := allData.views[part]
	return renderDetailedView(data, width, height, page)
}

func renderDetailedView(data detailedViewData, width, height, page int) sqRenderResult {
	var b bytes.Buffer
	width = sqEffectiveWidth(width)
	if width < 32 {
		width = 32
	}

	title := fmt.Sprintf("🖥️ 分区: %s (%d/%d) | 节点: %d | 运行任务: %d",
		data.targetPart, data.partitionIndex+1, data.totalPartitions, data.nodeCount, data.jobCount)
	fmt.Fprintln(&b, lipgloss.NewStyle().Bold(true).MarginBottom(1).Render(truncateDisplay(title, width)))

	mode := detailedRenderMode{compact: width < 145}
	columns := selectDetailedColumns(data.rows, width, mode)
	colWidths := detailedColumnWidths(data.rows, columns, mode)
	expandDetailedWidths(data.rows, columns, colWidths, mode, width)
	pageRows, page, totalPages := paginateDetailedRows(data.rows, height, page)

	fmt.Fprintln(&b, renderSqBorder(colWidths))
	fmt.Fprintln(&b, renderDetailedHeader(columns, colWidths))
	fmt.Fprintln(&b, renderSqBorder(colWidths))
	for _, row := range pageRows {
		fmt.Fprintln(&b, renderDetailedRow(row, columns, colWidths, mode))
	}
	fmt.Fprintln(&b, renderSqBorder(colWidths))

	footer := fmt.Sprintf("Page %d/%d  p: 下一分区  Space: 当前分区翻页(循环)  b: 上一页  g/G: 首/末页  q: 退出",
		page+1, totalPages)
	fmt.Fprintln(&b, StyleDim.Render(truncateDisplay(footer, width)))
	return sqRenderResult{text: b.String(), page: page, totalPages: totalPages}
}

func detailedColumns(mode detailedRenderMode) []detailedColumn {
	nodeWidth := 10
	partWidth := 10
	jobWidth := 24
	userWidth := 10
	if mode.compact {
		nodeWidth = 8
		partWidth = 8
		jobWidth = 18
		userWidth = 8
	}

	return []detailedColumn{
		{header: "NODE", minWidth: 4, maxWidth: nodeWidth, dropRank: 0, value: func(r detailedRow, _ detailedRenderMode) string {
			if !r.showNode {
				return ""
			}
			return r.node.Name
		}},
		{header: "PART", minWidth: 4, maxWidth: partWidth, dropRank: 80, value: func(r detailedRow, _ detailedRenderMode) string {
			if !r.showNode {
				return ""
			}
			return r.node.Part
		}},
		{header: "CPU", minWidth: 7, maxWidth: 12, dropRank: 0, value: detailedCPU, style: detailedCPUStyle},
		{header: "GPU", minWidth: 6, maxWidth: 10, dropRank: 10, value: detailedGPU, style: detailedGPUStyle},
		{header: "MEM", minWidth: 8, maxWidth: 14, dropRank: 0, value: detailedMem, style: detailedMemStyle},
		{header: "USER", minWidth: 4, maxWidth: userWidth, dropRank: 50, value: func(r detailedRow, _ detailedRenderMode) string {
			if r.job == nil {
				return "-"
			}
			return r.job.User
		}},
		{header: "JOB NAME", minWidth: 8, maxWidth: jobWidth, dropRank: 60, value: func(r detailedRow, _ detailedRenderMode) string {
			if r.job == nil {
				return "-"
			}
			return r.job.Name
		}},
		{header: "JC", minWidth: 2, maxWidth: 5, dropRank: 30, align: lipgloss.Right, value: func(r detailedRow, _ detailedRenderMode) string {
			if r.job == nil {
				return "-"
			}
			return fmt.Sprintf("%d", r.job.CPUs)
		}},
		{header: "JM", minWidth: 3, maxWidth: 6, dropRank: 40, align: lipgloss.Right, value: func(r detailedRow, _ detailedRenderMode) string {
			if r.job == nil {
				return "-"
			}
			return fmt.Sprintf("%dG", r.job.MemGB)
		}},
	}
}

func selectDetailedColumns(rows []detailedRow, width int, mode detailedRenderMode) []detailedColumn {
	columns := detailedColumns(mode)
	for sqTableWidth(detailedColumnWidths(rows, columns, mode)) > width {
		dropIdx := -1
		for i, col := range columns {
			if col.dropRank == 0 {
				continue
			}
			if dropIdx == -1 || col.dropRank > columns[dropIdx].dropRank {
				dropIdx = i
			}
		}
		if dropIdx == -1 {
			break
		}
		columns = append(columns[:dropIdx], columns[dropIdx+1:]...)
	}
	return columns
}

func detailedColumnWidths(rows []detailedRow, columns []detailedColumn, mode detailedRenderMode) []int {
	widths := make([]int, len(columns))
	for i, col := range columns {
		widths[i] = lipgloss.Width(col.header)
		for _, row := range rows {
			if cellWidth := lipgloss.Width(col.value(row, mode)); cellWidth > widths[i] {
				widths[i] = cellWidth
			}
		}
		if widths[i] > col.maxWidth {
			widths[i] = col.maxWidth
		}
		if widths[i] < col.minWidth {
			widths[i] = col.minWidth
		}
	}
	return widths
}

func expandDetailedWidths(rows []detailedRow, columns []detailedColumn, widths []int, mode detailedRenderMode, availableWidth int) {
	extra := availableWidth - sqTableWidth(widths)
	if extra <= 0 {
		return
	}

	for _, header := range []string{"PART", "JOB NAME", "USER", "NODE"} {
		idx := -1
		for i, col := range columns {
			if col.header == header {
				idx = i
				break
			}
		}
		if idx == -1 {
			continue
		}

		wanted := lipgloss.Width(columns[idx].header)
		for _, row := range rows {
			if cellWidth := lipgloss.Width(columns[idx].value(row, mode)); cellWidth > wanted {
				wanted = cellWidth
			}
		}

		grow := wanted - widths[idx]
		if grow <= 0 {
			continue
		}
		if grow > extra {
			grow = extra
		}
		widths[idx] += grow
		extra -= grow
		if extra == 0 {
			return
		}
	}
}

func paginateDetailedRows(rows []detailedRow, height, page int) ([]detailedRow, int, int) {
	rowsPerPage := len(rows)
	if height > 0 {
		rowsPerPage = height - 7
		if rowsPerPage < 1 {
			rowsPerPage = 1
		}
	}

	totalPages := (len(rows) + rowsPerPage - 1) / rowsPerPage
	if totalPages < 1 {
		totalPages = 1
	}
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}

	start := page * rowsPerPage
	end := start + rowsPerPage
	if end > len(rows) {
		end = len(rows)
	}
	return rows[start:end], page, totalPages
}

func renderDetailedHeader(columns []detailedColumn, widths []int) string {
	var b strings.Builder
	b.WriteString("|")
	for i, col := range columns {
		b.WriteString(" ")
		b.WriteString(padDisplay(truncateDisplay(col.header, widths[i]), widths[i], lipgloss.Center))
		b.WriteString(" |")
	}
	return StyleHeader.Render(b.String())
}

func renderDetailedRow(row detailedRow, columns []detailedColumn, widths []int, mode detailedRenderMode) string {
	var b strings.Builder
	b.WriteString("|")
	for i, col := range columns {
		raw := truncateDisplay(col.value(row, mode), widths[i])
		cell := padDisplay(raw, widths[i], col.align)
		if col.style != nil {
			cell = col.style(row, cell)
		}
		b.WriteString(" ")
		b.WriteString(cell)
		b.WriteString(" |")
	}
	return b.String()
}

func detailedCPU(row detailedRow, mode detailedRenderMode) string {
	if !row.showNode {
		return ""
	}
	pct := percent(row.node.AllocCPUs, row.node.TotalCPUs)
	if mode.compact {
		return fmt.Sprintf("C%d %d%%", row.node.TotalCPUs, pct)
	}
	return fmt.Sprintf("%d/%d %d%%", row.node.AllocCPUs, row.node.TotalCPUs, pct)
}

func detailedGPU(row detailedRow, mode detailedRenderMode) string {
	if !row.showNode {
		return ""
	}
	pct := percent(row.node.AllocGPUs, row.node.TotalGPUs)
	if mode.compact {
		return fmt.Sprintf("G%d %d%%", row.node.TotalGPUs, pct)
	}
	return fmt.Sprintf("%d/%d %d%%", row.node.AllocGPUs, row.node.TotalGPUs, pct)
}

func detailedMem(row detailedRow, mode detailedRenderMode) string {
	if !row.showNode {
		return ""
	}
	totalGB := row.node.TotalMemMB / 1024
	allocGB := row.node.AllocMemMB / 1024
	pct := percent(row.node.AllocMemMB, row.node.TotalMemMB)
	if mode.compact {
		return fmt.Sprintf("M%dG %d%%", totalGB, pct)
	}
	return fmt.Sprintf("%d/%dG %d%%", allocGB, totalGB, pct)
}

func detailedCPUStyle(row detailedRow, text string) string {
	if row.node == nil || !row.showNode {
		return text
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(getColorByPercentage(percent(row.node.AllocCPUs, row.node.TotalCPUs)))).Render(text)
}

func detailedGPUStyle(row detailedRow, text string) string {
	if row.node == nil || !row.showNode {
		return text
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(getColorByPercentage(percent(row.node.AllocGPUs, row.node.TotalGPUs)))).Render(text)
}

func detailedMemStyle(row detailedRow, text string) string {
	if row.node == nil || !row.showNode {
		return text
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(getColorByPercentage(percent(row.node.AllocMemMB, row.node.TotalMemMB)))).Render(text)
}

func percent(used, total int) int {
	if total <= 0 {
		return 0
	}
	return (used * 100) / total
}

func runDetailedInteractive(render func(width, height, partIndex, page int) sqRenderResult, refresh func(), partCount func() int, initialPart int) {
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		width, height, sizeErr := term.GetSize(int(os.Stdout.Fd()))
		if sizeErr != nil {
			width = 120
			height = 0
		}
		fmt.Print(render(width, height, initialPart, 0).text)
		return
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	keyCh := make(chan byte, 1)
	go func() {
		var buf [1]byte
		for {
			n, err := os.Stdin.Read(buf[:])
			if err != nil {
				if err != io.EOF {
					keyCh <- 3
				}
				return
			}
			if n > 0 {
				keyCh <- buf[0]
			}
		}
	}()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	refreshTicker := time.NewTicker(10 * time.Second)
	defer refreshTicker.Stop()

	fmt.Print("\x1b[?1049h\x1b[?25l")
	defer fmt.Print("\x1b[?25h\x1b[?1049l")

	currentWidth := 0
	currentHeight := 0
	currentPart := initialPart
	currentPage := 0
	currentTotalPages := 1
	draw := func(force bool) {
		width, height, err := term.GetSize(int(os.Stdout.Fd()))
		if err != nil {
			width = 120
			height = 24
		}
		if !force && width == currentWidth && height == currentHeight {
			return
		}
		if count := partCount(); count > 0 {
			if currentPart < 0 {
				currentPart = 0
			}
			if currentPart >= count {
				currentPart = 0
			}
		}
		currentWidth = width
		currentHeight = height
		result := render(width, height, currentPart, currentPage)
		currentPage = result.page
		currentTotalPages = result.totalPages
		fmt.Print("\x1b[H\x1b[2J\x1b[3J")
		fmt.Print(strings.ReplaceAll(result.text, "\n", "\r\n"))
	}

	draw(true)
	for {
		select {
		case <-ticker.C:
			draw(false)
		case <-refreshTicker.C:
			refresh()
			draw(true)
		case key := <-keyCh:
			if key == 'q' || key == 'Q' || key == 3 {
				return
			}
			switch key {
			case 'p':
				if count := partCount(); count > 0 {
					currentPart = (currentPart + 1) % count
				}
				currentPage = 0
				draw(true)
			case ' ':
				if currentPage >= currentTotalPages-1 {
					currentPage = 0
				} else {
					currentPage++
				}
				draw(true)
			case 'n':
				currentPage++
				draw(true)
			case 'b':
				currentPage--
				draw(true)
			case 'g':
				currentPage = 0
				draw(true)
			case 'G':
				currentPage = 1<<31 - 1
				draw(true)
			}
		}
	}
}

func runLspMode(data GlobalData) {
	partToNodes := make(map[string][]string)
	for name, node := range data.Nodes {
		partToNodes[node.Part] = append(partToNodes[node.Part], name)
	}

	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		width = 120
	}

	cellWidth := 34
	nodesPerRow := (width - 2) / cellWidth
	nodesPerRow = nodesPerRow - 1
	if nodesPerRow < 1 {
		nodesPerRow = 1
	}

	partitions := make([]string, 0, len(partToNodes))
	for p := range partToNodes {
		partitions = append(partitions, p)
	}
	sort.Strings(partitions)

	for i, partName := range partitions {
		nodeNames := partToNodes[partName]
		sort.Strings(nodeNames)

		fmt.Println(StyleLspPartTitle.Render(fmt.Sprintf("Partition: %s (Total Nodes: %d)", partName, len(nodeNames))))

		t := table.New().
			Border(lipgloss.NormalBorder()).BorderRow(false).BorderColumn(true).
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
		if i < len(partitions)-1 {
			fmt.Println()
		}
	}
}

func renderLspCellContent(node *Node) string {
	cpuPct, ramPct, gpuPct := 0, 0, 0
	if node.TotalCPUs > 0 {
		cpuPct = (node.AllocCPUs * 100) / node.TotalCPUs
	}
	if node.TotalMemMB > 0 {
		ramPct = (node.AllocMemMB * 100) / node.TotalMemMB
	}
	if node.TotalGPUs > 0 {
		gpuPct = (node.AllocGPUs * 100) / node.TotalGPUs
	}

	cText := lipgloss.NewStyle().Foreground(lipgloss.Color(getColorByPercentage(cpuPct))).Render(fmt.Sprintf("C:%3d%%", cpuPct))
	gText := lipgloss.NewStyle().Foreground(lipgloss.Color(getColorByPercentage(gpuPct))).Render(fmt.Sprintf("G:%3d%%", gpuPct))
	rText := lipgloss.NewStyle().Foreground(lipgloss.Color(getColorByPercentage(ramPct))).Render(fmt.Sprintf("R:%3d%%", ramPct))

	return fmt.Sprintf("%-10s %s  %s  %s", node.Name, cText, gText, rText)
}
