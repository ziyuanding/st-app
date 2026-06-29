package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/user"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"golang.org/x/term"
)

func runSqMode(showAll bool, targetUser string) {
	interactive := term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stdin.Fd()))
	if interactive {
		fmt.Print("正在从数据库拉取 24 小时内的任务记录...")
	} else {
		fmt.Println("正在从数据库拉取 24 小时内的任务记录...")
	}

	// 把参数传给底层，让 sacct 替我们做过滤
	filteredJobs := getQueueData(showAll, targetUser)
	if interactive {
		fmt.Print("\r\x1b[2K")
	}

	// 1. 确定展示的用户名称
	displayUser := targetUser
	if showAll {
		displayUser = "ALL"
	} else if targetUser == "" {
		currentUser, _ := user.Current()
		displayUser = currentUser.Username
	}

	// 2. 统计各种状态的任务数量
	runCount, pdCount, doneCount, failCount, totalCPUs := 0, 0, 0, 0, 0
	for _, j := range filteredJobs {
		switch j.State {
		case "RUNNING":
			runCount++
			totalCPUs += j.CPUs
		case "PENDING":
			pdCount++
		case "COMPLETED":
			doneCount++
		case "FAILED", "NODE_FAIL", "TIMEOUT", "CANCELLED":
			failCount++
		}
	}

	render := func(width int) string {
		return renderSqView(filteredJobs, displayUser, runCount, pdCount, doneCount, failCount, totalCPUs, width)
	}

	if !interactive {
		width, _, err := term.GetSize(int(os.Stdout.Fd()))
		if err != nil {
			width = 120
		}
		fmt.Print(render(width))
		return
	}

	runSqInteractive(render)
}

func renderSqView(filteredJobs []Job, displayUser string, runCount, pdCount, doneCount, failCount, totalCPUs, width int) string {
	var b bytes.Buffer

	dash := lipgloss.NewStyle().Bold(true).MarginBottom(1).Render(
		fmt.Sprintf("📊 24小时任务总览 | 用户: %s | 运行: %d (占%d核) | 排队: %d | 完成: %d | 失败/取消: %d",
			displayUser, runCount, totalCPUs, pdCount, doneCount, failCount),
	)
	fmt.Fprintln(&b, dash)

	if len(filteredJobs) == 0 {
		fmt.Fprintln(&b, StyleDim.Render("过去 24 小时内没有任何任务记录。"))
		return b.String()
	}

	useFoldedView := width < 105

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(StyleDivider).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == 0 {
				return StyleHeader
			}
			return lipgloss.NewStyle().Padding(0, 1)
		})

	if useFoldedView {
		t.Headers("JOBID   STATE", "PART & NODE", "RESOURCES", "ELAPSED")
	} else {
		t.Headers("JOBID   STATE", "PART & NODE", "JOB NAME", "RESOURCES", "ELAPSED", "START", "END")
	}

	for _, j := range filteredJobs {
		// -- 处理状态列颜色与缩写 --
		stateStr := j.State
		switch stateStr {
		case "RUNNING":
			stateStr = StyleGreen.Render("R ")
		case "PENDING":
			stateStr = StyleYellow.Render("PD")
		case "COMPLETED":
			stateStr = StyleDim.Render("CD")
		case "FAILED", "NODE_FAIL", "TIMEOUT":
			stateStr = StyleRed.Render("F ")
		case "CANCELLED":
			stateStr = StyleRed.Render("CA")
		default:
			stateStr = StyleDim.Render(truncate(stateStr, 2))
		}

		idState := fmt.Sprintf("%-9s %s", j.ID, stateStr)

		// -- 处理节点列 --
		partNode := j.Part + "  "
		if j.State == "PENDING" {
			partNode += StyleItalic.Render(truncate(j.NodeOrReason, 15))
		} else {
			partNode += truncate(j.NodeOrReason, 15)
		}

		res := fmt.Sprintf("C%d G%d R%d", j.CPUs, j.GPUs, j.MemGB)

		if useFoldedView {
			t.Row(idState, partNode, res, j.Elapsed)
			nameRow := StyleDim.Render("  └─ " + truncate(j.Name, 20))
			timeRow := StyleDim.Render(fmt.Sprintf("S: %s  E: %s", truncate(j.Start, 14), truncate(j.End, 14)))
			t.Row(nameRow, timeRow, "", "")
		} else {
			t.Row(idState, partNode, truncate(j.Name, 20), res, j.Elapsed, j.Start, j.End)
		}
	}

	fmt.Fprintln(&b, t.Render())
	fmt.Fprintln(&b, StyleDim.Render("按 q 或 Ctrl+C 退出；调整终端宽度会自动切换布局。"))
	return b.String()
}

func runSqInteractive(render func(width int) string) {
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		width, _, sizeErr := term.GetSize(int(os.Stdout.Fd()))
		if sizeErr != nil {
			width = 120
		}
		fmt.Print(render(width))
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

	fmt.Print("\x1b[?1049h\x1b[?25l")
	defer fmt.Print("\x1b[?25h\x1b[?1049l")

	currentWidth := 0
	draw := func(force bool) {
		width, _, err := term.GetSize(int(os.Stdout.Fd()))
		if err != nil {
			width = 120
		}
		if !force && width == currentWidth {
			return
		}
		currentWidth = width
		fmt.Print("\x1b[H\x1b[2J")
		fmt.Print(render(width))
	}

	draw(true)
	for {
		select {
		case <-ticker.C:
			draw(false)
		case key := <-keyCh:
			if key == 'q' || key == 'Q' || key == 3 {
				return
			}
		}
	}
}
