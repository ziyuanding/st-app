package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"golang.org/x/term"
)

// === 结构体定义 ===

// Job 定义作业信息
type Job struct {
	User string
	Name string
	CPUs string
	Mem  string
}

// Node 定义节点详细信息 (用于默认视图)
type Node struct {
	Name       string
	Part       string
	TotalCPUs  int
	AllocCPUs  int
	TotalGPUs  int
	AllocGPUs  int
	TotalMemMB int
	AllocMemMB int
	Jobs       []Job
}

// GlobalData 存储所有抓取到的数据
type GlobalData struct {
	Nodes map[string]*Node
}

// === 全局样式定义 (回归素色) ===

var (
	StyleHeader       = lipgloss.NewStyle().Bold(true).Align(lipgloss.Center)
	StyleRow          = lipgloss.NewStyle().Align(lipgloss.Center)
	StyleDivider      = lipgloss.NewStyle().Foreground(lipgloss.Color("238")) // 暗灰色隔断线
	StyleLspPartTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")).MarginBottom(1)
	
	// 修改了这里：设置边框颜色使用的是 BorderForeground
	StyleLspNodeCell  = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color("238")). 
				Padding(0, 1).
				Align(lipgloss.Left)
)

// === 主函数 & 命令行解析 ===

func main() {
	// 定义子命令用法
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: st [subcommand] [options]\n\n")
		fmt.Fprintf(os.Stderr, "Subcommands:\n")
		fmt.Fprintf(os.Stderr, "  (default)    显示指定分区的详细节点网格视图\n")
		fmt.Fprintf(os.Stderr, "  lsp          List Partitions: 显示所有分区的节点资源热力图矩阵\n\n")
		fmt.Fprintf(os.Stderr, "Options for default mode:\n")
		flag.PrintDefaults()
	}

	part := flag.String("p", "work", "指定默认模式下要查看的 partition")
	flag.Parse()

	// 1. 抓取全局 Slurm 数据 (sinfo + squeue)
	fmt.Println("正在抓取 Slurm 数据...")
	data := getGlobalSlurmData()

	// 2. 根据子命令决定运行模式
	subcommand := ""
	if flag.NArg() > 0 {
		subcommand = flag.Arg(0)
	}

	switch subcommand {
	case "lsp":
		runLspMode(data)
	case "":
		// 默认模式：显示单个分区的详细网格
		runDetailedMode(data, *part)
	default:
		fmt.Printf("未知子命令: %s\n", subcommand)
		flag.Usage()
		os.Exit(1)
	}
}

// === 核心功能 1: Detailed Mode (你之前的代码，微调过) ===

func runDetailedMode(data GlobalData, targetPart string) {
	// 筛选出属于目标分区的节点
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

	// 动态探测列宽
	colWidths := []int{4, 4, 14, 14, 15, 4, 8, 8, 7}
	for _, node := range nodes {
		updateColWidths(&colWidths, node)
	}

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("238"))).
		Headers("NODE", "PART", "CPUs", "GPUs", "RAM", "USER", "JOB NAME", "JOB CPUs", "JOB RAM").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == 0 { return StyleHeader }
			return StyleRow
		})

	for i, nodeName := range keys {
		node := nodes[nodeName]
		cCell, gCell, rCell := renderResourceCells(node, false)

		if len(node.Jobs) == 0 {
			t.Row(node.Name, node.Part, cCell, gCell, rCell, "-", "-", "-", "-")
		} else {
			for jIdx, j := range node.Jobs {
				if jIdx == 0 {
					t.Row(node.Name, node.Part, cCell, gCell, rCell, j.User, j.Name, j.CPUs, j.Mem)
				} else {
					t.Row("", "", "", "", "", j.User, j.Name, j.CPUs, j.Mem)
				}
			}
		}

		if i < len(keys)-1 {
			t.Row(
				StyleDivider.Render(strings.Repeat("─", colWidths[0])),
				StyleDivider.Render(strings.Repeat("─", colWidths[1])),
				StyleDivider.Render(strings.Repeat("─", 14)), StyleDivider.Render(strings.Repeat("─", 14)), StyleDivider.Render(strings.Repeat("─", 15)),
				StyleDivider.Render(strings.Repeat("─", colWidths[5])), StyleDivider.Render(strings.Repeat("─", colWidths[6])), StyleDivider.Render(strings.Repeat("─", colWidths[7])), StyleDivider.Render(strings.Repeat("─", colWidths[8])),
			)
		}
	}
	fmt.Println(t.Render())
}


// === 核心功能 2: LSP Mode (单行大表格版) ===
func runLspMode(data GlobalData) {
	// 按分区组织节点，并获取所有唯一的节点列表用于排序
	partToNodes := make(map[string][]string)
	for name, node := range data.Nodes {
		partToNodes[node.Part] = append(partToNodes[node.Part], name)
	}

	// 探测终端宽度
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		width = 120 // 探测失败时的默认宽度
	}

	// 预估每个格子的宽度：节点名(约10) + CGR指标(约20) + 左右Padding(2) + 边框(1) ≈ 34字符
	cellWidth := 34
	nodesPerRow := (width - 2) / cellWidth
	nodesPerRow = nodesPerRow - 1
	if nodesPerRow < 1 {
		nodesPerRow = 1
	}

	// 获取排序后的分区列表
	partitions := make([]string, 0, len(partToNodes))
	for p := range partToNodes {
		partitions = append(partitions, p)
	}
	sort.Strings(partitions)

	// 遍历分区打印大表格
	for i, partName := range partitions {
		nodeNames := partToNodes[partName]
		sort.Strings(nodeNames) // 分区内节点排序

		// 打印分区标题
		fmt.Println(StyleLspPartTitle.Render(fmt.Sprintf("Partition: %s (Total Nodes: %d)", partName, len(nodeNames))))

		// 构建标准网格表格
		t := table.New().
			Border(lipgloss.NormalBorder()).     // 开启外边框
			BorderRow(false).                    // 关闭行之间的横线 (让节点排布更紧凑)
			BorderColumn(true).                  // 开启列之间的竖线
			BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("238"))).
			StyleFunc(func(row, col int) lipgloss.Style {
				return lipgloss.NewStyle().Padding(0, 1) // 左右留一个空格的间距
			})

		// 将节点以单行格式填入多行表格中
		for rowIdx := 0; rowIdx*nodesPerRow < len(nodeNames); rowIdx++ {
			rowCells := make([]string, 0)
			for colIdx := 0; colIdx < nodesPerRow; colIdx++ {
				nodeIdx := rowIdx*nodesPerRow + colIdx
				if nodeIdx < len(nodeNames) {
					nodeName := nodeNames[nodeIdx]
					// 获取拼装好的单行字符串
					rowCells = append(rowCells, renderLspCellContent(data.Nodes[nodeName]))
				} else {
					// 数量不够时，用空字符串补齐表格列数，保证表格边框完整
					rowCells = append(rowCells, "")
				}
			}
			t.Row(rowCells...)
		}

		fmt.Println(t.Render())

		// 分区之间空一行
		if i < len(partitions)-1 {
			fmt.Println()
		}
	}
}



// === 辅助工具函数 (资源探测、解析、渲染) ===

func getGlobalSlurmData() GlobalData {
	nodes := make(map[string]*Node)
	uniqueNodeSet := make(map[string]bool) // 辅助：处理 sinfo 分区展开时的重复节点

	// 1. 获取所有节点、分区和总资源 (sinfo -a -N)
	// 加上-a可以看到所有分区，包括你没有权限提交的分区
	cmdSinfo := "sinfo -N -h -o '%N|%R|%c|%G|%m'"
	outSinfo, _ := exec.Command("bash", "-c", cmdSinfo).Output()
	
	linesSinfo := strings.Split(strings.TrimSpace(string(outSinfo)), "\n")
	for _, line := range linesSinfo {
		parts := strings.Split(line, "|")
		if len(parts) >= 5 {
			nodelist := parts[0]
			partition := parts[1]
			cpu, _ := strconv.Atoi(parts[2])
			gpu := parseGPUString(parts[3])
			mem, _ := strconv.Atoi(parts[4])

			// 展开类似 node[01-05] 的节点名 (简单匹配，假设sinfo -N返回的是单节点)
			// 注意：sinfo -N -o %N 在某些Slurm配置下仍会返回范围，稳妥起见需使用scontrol show hostnames
			// 此处简化处理：假设 sinfo -N -h 已展开节点
			names := expandNodelist(nodelist)

			for _, name := range names {
				key := name + "_" + partition // 以节点名+分区作为唯一键
				if _, exists := nodes[key]; !exists {
					nodes[key] = &Node{
						Name:       name,
						Part:       partition,
						TotalCPUs:  cpu,
						TotalGPUs:  gpu,
						TotalMemMB: mem,
						Jobs:       []Job{},
					}
					uniqueNodeSet[name] = true // 记录唯一节点名用于后续 squeue 匹配
				}
			}
		}
	}

	// 2. 获取全局作业占用 (squeue -a -t R)
	cmdSqueue := "squeue -a -h -t R -o '%N|%u|%j|%C|%m|%b'"
	outSqueue, _ := exec.Command("bash", "-c", cmdSqueue).Output()

	linesSqueue := strings.Split(strings.TrimSpace(string(outSqueue)), "\n")
	for _, line := range linesSqueue {
		if line == "" { continue }
		parts := strings.Split(line, "|")
		if len(parts) >= 6 {
			allocNodelist := expandNodelist(parts[0])
			jobCPUs, _ := strconv.Atoi(parts[3])
			jobMemMB := parseJobMemToMB(parts[4])
			jobGPUs := parseTRESForGPU(parts[5])

			job := Job{User: parts[1], Name: parts[2], CPUs: parts[3], Mem: parts[4]}

			for _, allocNode := range allocNodelist {
				// 将该作业累加到所有属于该节点的 Node 结构体中 (因为节点可属多个分区)
				for key, nodePtr := range nodes {
					if nodePtr.Name == allocNode {
						nodePtr.Jobs = append(nodePtr.Jobs, job)
						nodePtr.AllocCPUs += jobCPUs
						nodePtr.AllocMemMB += jobMemMB
						nodePtr.AllocGPUs += jobGPUs
						nodes[key] = nodePtr // 确保写回 map
					}
				}
			}
		}
	}

	return GlobalData{Nodes: nodes}
}

// 辅助：渲染 lsp 模式下的单行格子内容
func renderLspCellContent(node *Node) string {
	cpuPct, ramPct, gpuPct := 0, 0, 0
	if node.TotalCPUs > 0 { cpuPct = (node.AllocCPUs * 100) / node.TotalCPUs }
	if node.TotalMemMB > 0 { ramPct = (node.AllocMemMB * 100) / node.TotalMemMB }
	if node.TotalGPUs > 0 { gpuPct = (node.AllocGPUs * 100) / node.TotalGPUs }

	colorC := getColorByPercentage(cpuPct)
	colorG := getColorByPercentage(gpuPct)
	colorR := getColorByPercentage(ramPct)

	// 使用 %3d 保证百分比总是占用 3 个字符宽度 (如 "  0", " 50", "100")，避免列因为数字长度不同而抖动
	cText := lipgloss.NewStyle().Foreground(lipgloss.Color(colorC)).Render(fmt.Sprintf("C:%3d%%", cpuPct))
	gText := lipgloss.NewStyle().Foreground(lipgloss.Color(colorG)).Render(fmt.Sprintf("G:%3d%%", gpuPct))
	rText := lipgloss.NewStyle().Foreground(lipgloss.Color(colorR)).Render(fmt.Sprintf("R:%3d%%", ramPct))

	// 将所有信息格式化为单行：节点名强行左对齐占10个字符位，后面跟着指标
	return fmt.Sprintf("%-10s %s  %s  %s", node.Name, cText, gText, rText)
}

// === 之前代码的通用辅助代码 (复制过来，未做大改动) ===

func renderResourceCells(node *Node, simpleMode bool) (string, string, string) {
	cpuPct, ramPct, gpuPct := 0, 0, 0
	if node.TotalCPUs > 0 { cpuPct = (node.AllocCPUs * 100) / node.TotalCPUs }
	if node.TotalMemMB > 0 { ramPct = (node.AllocMemMB * 100) / node.TotalMemMB }
	if node.TotalGPUs > 0 { gpuPct = (node.AllocGPUs * 100) / node.TotalGPUs }

	totalRAMGB := float64(node.TotalMemMB) / 1024.0

	cpuStr := fmt.Sprintf("%d\n(%d%% in use)", node.TotalCPUs, cpuPct)
	gpuStr := fmt.Sprintf("%d\n(%d%% in use)", node.TotalGPUs, gpuPct)
	ramStr := fmt.Sprintf("%.1f GB\n(%d%% in use)", totalRAMGB, ramPct)

	cpuCell := lipgloss.NewStyle().Foreground(lipgloss.Color(getColorByPercentage(cpuPct))).Render(cpuStr)
	gpuCell := lipgloss.NewStyle().Foreground(lipgloss.Color(getColorByPercentage(gpuPct))).Render(gpuStr)
	ramCell := lipgloss.NewStyle().Foreground(lipgloss.Color(getColorByPercentage(ramPct))).Render(ramStr)
	
	return cpuCell, gpuCell, ramCell
}

func getColorByPercentage(pct int) string {
	if pct < 0 { pct = 0 }
	if pct > 100 { pct = 100 }
	// 11 色阶：绿 -> 黄 -> 红
	colors := []string{"#00FF00", "#33FF00", "#66FF00", "#99FF00", "#CCFF00", "#FFFF00", "#FFCC00", "#FF9900", "#FF6600", "#FF3300", "#FF0000"}
	index := pct / 10
	if index > 10 { index = 10 }
	return colors[index]
}

func parseGPUString(g string) int {
	if g == "(null)" || g == "" || g == "none" || !strings.Contains(strings.ToLower(g), "gpu") { return 0 }
	parts := strings.Split(g, ":")
	if len(parts) > 0 {
		val, err := strconv.Atoi(parts[len(parts)-1])
		if err == nil { return val }
	}
	return 0
}
func parseTRESForGPU(tres string) int {
	tres = strings.ToLower(tres)
	items := strings.Split(tres, ",")
	for _, item := range items {
		if strings.Contains(item, "gpu") {
			item = strings.ReplaceAll(item, "=", ":")
			parts := strings.Split(item, ":")
			if len(parts) > 0 {
				val, err := strconv.Atoi(parts[len(parts)-1])
				if err == nil { return val }
			}
		}
	}
	return 0
}
func parseJobMemToMB(m string) int {
	m = strings.ToUpper(strings.TrimSpace(m))
	if len(m) == 0 {
		return 0
	}

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
func updateColWidths(colWidths *[]int, node *Node) {
	if lipgloss.Width(node.Name) > (*colWidths)[0] { (*colWidths)[0] = lipgloss.Width(node.Name) }
	if lipgloss.Width(node.Part) > (*colWidths)[1] { (*colWidths)[1] = lipgloss.Width(node.Part) }
	for _, j := range node.Jobs {
		if lipgloss.Width(j.User) > (*colWidths)[5] { (*colWidths)[5] = lipgloss.Width(j.User) }
		if lipgloss.Width(j.Name) > (*colWidths)[6] { (*colWidths)[6] = lipgloss.Width(j.Name) }
		if lipgloss.Width(j.CPUs) > (*colWidths)[7] { (*colWidths)[7] = lipgloss.Width(j.CPUs) }
		if lipgloss.Width(j.Mem) > (*colWidths)[8] { (*colWidths)[8] = lipgloss.Width(j.Mem) }
	}
}
// 辅助：使用 scontrol 展开节点名 (HPC 必须)
func expandNodelist(nodelist string) []string {
	cmd := fmt.Sprintf("scontrol show hostnames '%s'", nodelist)
	out, err := exec.Command("bash", "-c", cmd).Output()
	if err != nil { return []string{nodelist} } // 失败退回到原始字符串
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}