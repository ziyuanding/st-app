package main

import (
	"fmt"
	"os"
	"os/user"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"golang.org/x/term"
)

func runSqMode(showAll bool, targetUser string) {
	fmt.Println("正在从数据库拉取 24 小时内的任务记录...")
	
	// 把参数传给底层，让 sacct 替我们做过滤
	filteredJobs := getQueueData(showAll, targetUser)

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

	// 3. 渲染顶部仪表盘 (新增了成功和失败的统计)
	dash := lipgloss.NewStyle().Bold(true).MarginBottom(1).Render(
		fmt.Sprintf("📊 24小时任务总览 | 用户: %s | 运行: %d (占%d核) | 排队: %d | 完成: %d | 失败/取消: %d", 
		displayUser, runCount, totalCPUs, pdCount, doneCount, failCount),
	)
	fmt.Println(dash)

	if len(filteredJobs) == 0 {
		fmt.Println(StyleDim.Render("过去 24 小时内没有任何任务记录。"))
		return
	}

	// 4. 终端宽度探测
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil { width = 120 }
	useFoldedView := width < 105

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(StyleDivider).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == 0 { return StyleHeader }
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

	fmt.Println(t.Render())
}