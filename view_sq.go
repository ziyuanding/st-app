package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/user"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

func runSqMode(showAll bool, targetUser string) {
	interactive := term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stdin.Fd()))
	if interactive {
		fmt.Print("正在从数据库拉取 24 小时内的任务记录...")
	} else {
		fmt.Println("正在从数据库拉取 24 小时内的任务记录...")
	}

	viewData := loadSqViewData(showAll, targetUser)
	if interactive {
		fmt.Print("\r\x1b[2K")
	}

	render := func(width, height, page int) sqRenderResult {
		return renderSqView(viewData, width, height, page)
	}
	refresh := func() {
		viewData = loadSqViewData(showAll, targetUser)
	}

	if !interactive {
		width, _, err := term.GetSize(int(os.Stdout.Fd()))
		if err != nil {
			width = 120
		}
		fmt.Print(render(width, 0, 0).text)
		return
	}

	runSqInteractive(render, refresh)
}

type sqViewData struct {
	jobs        []Job
	displayUser string
	runCount    int
	pdCount     int
	doneCount   int
	failCount   int
	totalCPUs   int
}

func loadSqViewData(showAll bool, targetUser string) sqViewData {
	filteredJobs := getQueueData(showAll, targetUser)
	sort.SliceStable(filteredJobs, func(i, j int) bool {
		return sqJobIDNumber(filteredJobs[i].ID) > sqJobIDNumber(filteredJobs[j].ID)
	})

	displayUser := targetUser
	if showAll {
		displayUser = "ALL"
	} else if targetUser == "" {
		currentUser, _ := user.Current()
		displayUser = currentUser.Username
	}

	data := sqViewData{
		jobs:        filteredJobs,
		displayUser: displayUser,
	}
	for _, j := range filteredJobs {
		switch j.State {
		case "RUNNING":
			data.runCount++
			data.totalCPUs += j.CPUs
		case "PENDING":
			data.pdCount++
		case "COMPLETED":
			data.doneCount++
		case "FAILED", "NODE_FAIL", "TIMEOUT", "CANCELLED":
			data.failCount++
		}
	}
	return data
}

type sqRenderResult struct {
	text       string
	page       int
	totalPages int
}

func renderSqView(data sqViewData, width, height, page int) sqRenderResult {
	var b bytes.Buffer
	width = sqEffectiveWidth(width)
	if width < 32 {
		width = 32
	}

	dashText := fmt.Sprintf("📊 24小时任务总览 | 用户: %s | 运行: %d (占%d核) | 排队: %d | 完成: %d | 失败/取消: %d",
		data.displayUser, data.runCount, data.totalCPUs, data.pdCount, data.doneCount, data.failCount)
	dash := lipgloss.NewStyle().Bold(true).MarginBottom(1).Render(truncateDisplay(dashText, width))
	fmt.Fprintln(&b, dash)

	if len(data.jobs) == 0 {
		fmt.Fprintln(&b, StyleDim.Render("过去 24 小时内没有任何任务记录。"))
		return sqRenderResult{text: b.String(), page: 0, totalPages: 1}
	}

	mode := sqRenderMode{compact: width < 155}
	columns := selectSqColumns(data.jobs, width, mode)
	colWidths := sqColumnWidths(data.jobs, columns, mode)
	pageJobs, page, totalPages := sqPaginateJobs(data.jobs, height, page)

	fmt.Fprintln(&b, renderSqBorder(colWidths))
	fmt.Fprintln(&b, renderSqHeader(columns, colWidths))
	fmt.Fprintln(&b, renderSqBorder(colWidths))
	for _, j := range pageJobs {
		fmt.Fprintln(&b, renderSqRow(j, columns, colWidths, mode))
	}
	fmt.Fprintln(&b, renderSqBorder(colWidths))

	footer := fmt.Sprintf("Page %d/%d  Space: 下一页(循环)  n: 下一页  p/b: 上一页  g/G: 首/末页  q: 退出",
		page+1, totalPages)
	footer = truncateDisplay(footer, width)
	fmt.Fprintln(&b, StyleDim.Render(footer))
	return sqRenderResult{text: b.String(), page: page, totalPages: totalPages}
}

type sqRenderMode struct {
	compact bool
}

type sqColumn struct {
	key      string
	header   string
	minWidth int
	maxWidth int
	dropRank int
	align    lipgloss.Position
	value    func(Job, sqRenderMode) string
	style    func(Job, string) string
}

func sqColumns(mode sqRenderMode) []sqColumn {
	dateWidth := 19
	resWidth := 22
	nodeWidth := 18
	nameWidth := 24
	if mode.compact {
		dateWidth = 11
		resWidth = 13
		nodeWidth = 14
		nameWidth = 18
	}

	return []sqColumn{
		{key: "id", header: "JOBID", minWidth: 5, maxWidth: 10, dropRank: 0, align: lipgloss.Right, value: func(j Job, _ sqRenderMode) string { return j.ID }},
		{key: "state", header: "ST", minWidth: 2, maxWidth: 2, dropRank: 0, align: lipgloss.Center, value: func(j Job, _ sqRenderMode) string { return sqStateShort(j.State) }, style: sqStateStyle},
		{key: "part", header: "PART", minWidth: 4, maxWidth: 10, dropRank: 50, value: func(j Job, _ sqRenderMode) string { return j.Part }},
		{key: "node", header: "NODE/REASON", minWidth: 6, maxWidth: nodeWidth, dropRank: 70, value: func(j Job, _ sqRenderMode) string { return j.NodeOrReason }, style: sqNodeStyle},
		{key: "name", header: "JOB NAME", minWidth: 8, maxWidth: nameWidth, dropRank: 60, value: func(j Job, _ sqRenderMode) string { return j.Name }},
		{key: "res", header: "RES", minWidth: 9, maxWidth: resWidth, dropRank: 10, value: sqResources},
		{key: "elapsed", header: "ELAPSED", minWidth: 7, maxWidth: 9, dropRank: 20, align: lipgloss.Right, value: func(j Job, _ sqRenderMode) string { return j.Elapsed }},
		{key: "start", header: "START", minWidth: 5, maxWidth: dateWidth, dropRank: 80, value: func(j Job, m sqRenderMode) string { return sqTime(j.Start, m) }},
		{key: "end", header: "END", minWidth: 5, maxWidth: dateWidth, dropRank: 90, value: func(j Job, m sqRenderMode) string { return sqTime(j.End, m) }},
	}
}

func sqJobIDNumber(id string) int64 {
	var b strings.Builder
	for _, r := range id {
		if r < '0' || r > '9' {
			break
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return 0
	}
	value, err := strconv.ParseInt(b.String(), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func sqPaginateJobs(jobs []Job, height, page int) ([]Job, int, int) {
	rowsPerPage := len(jobs)
	if height > 0 {
		rowsPerPage = height - 7
		if rowsPerPage < 1 {
			rowsPerPage = 1
		}
	}

	totalPages := (len(jobs) + rowsPerPage - 1) / rowsPerPage
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
	if end > len(jobs) {
		end = len(jobs)
	}
	return jobs[start:end], page, totalPages
}

func selectSqColumns(jobs []Job, width int, mode sqRenderMode) []sqColumn {
	columns := sqColumns(mode)
	for sqTableWidth(sqColumnWidths(jobs, columns, mode)) > width {
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

func sqColumnWidths(jobs []Job, columns []sqColumn, mode sqRenderMode) []int {
	widths := make([]int, len(columns))
	for i, col := range columns {
		widths[i] = lipgloss.Width(col.header)
		for _, j := range jobs {
			if cellWidth := lipgloss.Width(col.value(j, mode)); cellWidth > widths[i] {
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

func sqTableWidth(widths []int) int {
	total := 1
	for _, w := range widths {
		total += w + 3
	}
	return total
}

func renderSqBorder(widths []int) string {
	var b strings.Builder
	b.WriteString("+")
	for _, width := range widths {
		b.WriteString(strings.Repeat("-", width+2))
		b.WriteString("+")
	}
	return StyleDivider.Render(b.String())
}

func renderSqHeader(columns []sqColumn, widths []int) string {
	var b strings.Builder
	b.WriteString("|")
	for i, col := range columns {
		b.WriteString(" ")
		b.WriteString(padDisplay(truncateDisplay(col.header, widths[i]), widths[i], lipgloss.Center))
		b.WriteString(" |")
	}
	return StyleHeader.Render(b.String())
}

func renderSqRow(job Job, columns []sqColumn, widths []int, mode sqRenderMode) string {
	var b strings.Builder
	b.WriteString("|")
	for i, col := range columns {
		raw := truncateDisplay(col.value(job, mode), widths[i])
		cell := padDisplay(raw, widths[i], col.align)
		if col.style != nil {
			cell = col.style(job, cell)
		}
		b.WriteString(" ")
		b.WriteString(cell)
		b.WriteString(" |")
	}
	return b.String()
}

func sqStateShort(state string) string {
	switch state {
	case "RUNNING":
		return "R"
	case "PENDING":
		return "PD"
	case "COMPLETED":
		return "CD"
	case "FAILED", "NODE_FAIL", "TIMEOUT":
		return "F"
	case "CANCELLED":
		return "CA"
	default:
		return truncateDisplay(state, 2)
	}
}

func sqStateStyle(job Job, text string) string {
	switch job.State {
	case "RUNNING":
		return StyleBlue.Render(text)
	case "PENDING":
		return StyleYellow.Render(text)
	case "COMPLETED":
		return StyleGreen.Render(text)
	case "FAILED", "NODE_FAIL", "TIMEOUT", "CANCELLED":
		return StyleRed.Render(text)
	default:
		return StyleDim.Render(text)
	}
}

func sqNodeStyle(job Job, text string) string {
	if job.State == "PENDING" {
		return StyleItalic.Render(text)
	}
	return text
}

func sqResources(job Job, mode sqRenderMode) string {
	if mode.compact {
		return fmt.Sprintf("C%d G%d M%dG", job.CPUs, job.GPUs, job.MemGB)
	}
	return fmt.Sprintf("CPU:%d GPU:%d MEM:%dG", job.CPUs, job.GPUs, job.MemGB)
}

func sqTime(value string, mode sqRenderMode) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "Unknown" {
		return "Unknown"
	}
	value = strings.Replace(value, "T", " ", 1)
	if mode.compact {
		if len(value) >= 16 && value[4] == '-' {
			return value[5:16]
		}
		return truncateDisplay(value, 11)
	}
	if len(value) > 19 {
		return value[:19]
	}
	return value
}

func truncateDisplay(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	if max <= 2 {
		return strings.Repeat(".", max)
	}

	var b strings.Builder
	width := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if width+rw > max-2 {
			break
		}
		b.WriteRune(r)
		width += rw
	}
	b.WriteString("..")
	return b.String()
}

func padDisplay(s string, width int, align lipgloss.Position) string {
	textWidth := lipgloss.Width(s)
	if textWidth >= width {
		return s
	}

	padding := width - textWidth
	switch align {
	case lipgloss.Right:
		return strings.Repeat(" ", padding) + s
	case lipgloss.Center:
		left := padding / 2
		right := padding - left
		return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
	default:
		return s + strings.Repeat(" ", padding)
	}
}

func sqEffectiveWidth(width int) int {
	if override := strings.TrimSpace(os.Getenv("ST_SQ_WIDTH")); override != "" {
		if value, err := strconv.Atoi(override); err == nil && value > 0 {
			return value
		}
	}

	if width <= 0 {
		return 120
	}

	// VS Code's terminal can report the PTY width while the visible panel is
	// narrower, which makes otherwise valid lines wrap on screen. Use a more
	// conservative budget there; resizing still changes the layout, just with a
	// safety margin.
	if strings.EqualFold(os.Getenv("TERM_PROGRAM"), "vscode") {
		effective := (width * 2) / 3
		if effective < 80 {
			effective = width - 4
		}
		if effective < 32 {
			effective = 32
		}
		return effective
	}

	if width > 4 {
		return width - 2
	}
	return width
}

func runSqInteractive(render func(width, height, page int) sqRenderResult, refresh func()) {
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		width, height, sizeErr := term.GetSize(int(os.Stdout.Fd()))
		if sizeErr != nil {
			width = 120
			height = 0
		}
		fmt.Print(render(width, height, 0).text)
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
		currentWidth = width
		currentHeight = height
		result := render(width, height, currentPage)
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
			case 'p', 'b':
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
