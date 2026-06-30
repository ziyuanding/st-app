package main

import (
	"fmt"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
)

type Job struct {
	ID           string
	User         string
	Name         string
	State        string
	Part         string
	Dependency   string
	CPUs         int
	MemGB        int
	GPUs         int
	Elapsed      string
	Start        string
	End          string
	NodeOrReason string
}

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

type GlobalData struct {
	Nodes map[string]*Node
}

func getGlobalSlurmData() GlobalData {
	nodes := make(map[string]*Node)
	cmdSinfo := "sinfo -a -N -h -o '%N|%R|%c|%G|%m'"
	outSinfo, _ := exec.Command("bash", "-c", cmdSinfo).Output()

	for _, line := range strings.Split(strings.TrimSpace(string(outSinfo)), "\n") {
		parts := strings.Split(line, "|")
		if len(parts) >= 5 {
			cpu, _ := strconv.Atoi(parts[2])
			mem, _ := strconv.Atoi(parts[4])
			names := expandNodelist(parts[0])
			for _, name := range names {
				key := name + "_" + parts[1]
				if _, exists := nodes[key]; !exists {
					nodes[key] = &Node{
						Name: name, Part: parts[1], TotalCPUs: cpu,
						TotalGPUs: parseGPUString(parts[3]), TotalMemMB: mem,
					}
				}
			}
		}
	}

	// 抓取简化的作业信息用于节点视图
	cmdSqueue := "squeue -a -h -t R -o '%N|%u|%j|%C|%m|%b'"
	outSqueue, _ := exec.Command("bash", "-c", cmdSqueue).Output()
	for _, line := range strings.Split(strings.TrimSpace(string(outSqueue)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) >= 6 {
			allocNodelist := expandNodelist(parts[0])
			jobCPUs, _ := strconv.Atoi(parts[3])
			jobMemMB := parseJobMemToMB(parts[4])
			jobGPUs := parseTRESForGPU(parts[5])

			job := Job{User: parts[1], Name: parts[2], CPUs: jobCPUs, MemGB: jobMemMB / 1024, GPUs: jobGPUs}

			for _, allocNode := range allocNodelist {
				for key, nodePtr := range nodes {
					if nodePtr.Name == allocNode {
						nodePtr.Jobs = append(nodePtr.Jobs, job)
						nodePtr.AllocCPUs += jobCPUs
						nodePtr.AllocMemMB += jobMemMB
						nodePtr.AllocGPUs += jobGPUs
						nodes[key] = nodePtr
					}
				}
			}
		}
	}
	return GlobalData{Nodes: nodes}
}

func getVisibleNodePartitions() map[string]bool {
	out, err := exec.Command("scontrol", "show", "node").Output()
	if err != nil {
		return nil
	}

	partitions := make(map[string]bool)
	for _, field := range strings.Fields(string(out)) {
		if !strings.HasPrefix(field, "Partitions=") {
			continue
		}
		value := strings.TrimPrefix(field, "Partitions=")
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(strings.TrimSuffix(part, "*"))
			if part != "" && part != "(null)" {
				partitions[part] = true
			}
		}
	}
	if len(partitions) == 0 {
		return nil
	}
	return partitions
}

// 专门为 sq 视图抓取过去 24 小时的详细队列与历史信息 (基于 sacct)
func getQueueData(showAll bool, targetUser string) []Job {
	var jobs []Job

	// 1. 组装用户查询参数
	userFlag := ""
	if showAll {
		userFlag = "-a" // 看全集群
	} else {
		if targetUser == "" {
			currentUser, _ := user.Current()
			targetUser = currentUser.Username
		}
		userFlag = "-u " + targetUser // 只看指定用户（默认自己）
	}
	currentJobs := getCurrentQueueJobs(showAll, targetUser)
	dependencies := make(map[string]string)
	for _, job := range currentJobs {
		if job.Dependency != "" {
			dependencies[sqJobIDBase(job.ID)] = job.Dependency
		}
	}
	for id, dependency := range getSubmitLineDependencies(userFlag) {
		if dependency != "" {
			dependencies[id] = dependency
		}
	}

	// 2. 调用 sacct
	// -X: 隐藏多余的 job step (如 .batch)，只看主任务
	// -S now-1days: 限制查询范围为过去 24 小时
	cmdStr := fmt.Sprintf("sacct -X -n -P -S now-1days %s -o JobID,User,Partition,JobName,State,Elapsed,AllocCPUS,ReqMem,ReqTRES,NodeList,Start,End,Dependency", userFlag)
	out, err := exec.Command("bash", "-c", cmdStr).Output()
	if err != nil {
		cmdStr = fmt.Sprintf("sacct -X -n -P -S now-1days %s -o JobID,User,Partition,JobName,State,Elapsed,AllocCPUS,ReqMem,ReqTRES,NodeList,Start,End", userFlag)
		out, err = exec.Command("bash", "-c", cmdStr).Output()
		if err != nil {
			return currentJobs
		}
	}

	// 3. 解析输出
	seen := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		p := strings.Split(line, "|")
		if len(p) >= 12 {
			cpu, _ := strconv.Atoi(p[6])
			// 提取状态，sacct 返回的状态可能会带有 "CANCELLED by 1001"，只取第一个词
			state := strings.Split(p[4], " ")[0]
			nodeList := p[9]
			if nodeList == "None" {
				nodeList = "(Pending/Unknown)"
			}
			dependency := ""
			if len(p) >= 13 {
				dependency = formatDependency(p[12])
			}
			if dependency == "" {
				dependency = dependencies[sqJobIDBase(p[0])]
			}

			jobs = append(jobs, Job{
				ID: p[0], User: p[1], Part: p[2], Name: p[3], State: state, Elapsed: p[5],
				CPUs: cpu, MemGB: parseJobMemToMB(p[7]) / 1024, GPUs: parseTRESForGPU(p[8]),
				NodeOrReason: nodeList, Start: p[10], End: p[11], Dependency: dependency,
			})
			seen[sqJobIDBase(p[0])] = true
		}
	}
	for _, job := range currentJobs {
		if !seen[sqJobIDBase(job.ID)] {
			jobs = append(jobs, job)
		}
	}
	return jobs
}

func getSubmitLineDependencies(userFlag string) map[string]string {
	dependencies := make(map[string]string)
	cmdStr := fmt.Sprintf("sacct -X -n -P -S now-1days %s -o JobID,SubmitLine%%1500", userFlag)
	out, err := exec.Command("bash", "-c", cmdStr).Output()
	if err != nil {
		return dependencies
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		p := strings.SplitN(line, "|", 2)
		if len(p) != 2 {
			continue
		}
		dependency := parseDependencyFromSubmitLine(p[1])
		if dependency != "" {
			dependencies[sqJobIDBase(p[0])] = dependency
		}
	}
	return dependencies
}

func parseDependencyFromSubmitLine(submitLine string) string {
	fields := strings.Fields(submitLine)
	for i, field := range fields {
		if strings.HasPrefix(field, "--dependency=") {
			return formatDependency(strings.TrimPrefix(field, "--dependency="))
		}
		if field == "--dependency" && i+1 < len(fields) {
			return formatDependency(fields[i+1])
		}
	}
	return ""
}

func getCurrentQueueJobs(showAll bool, targetUser string) []Job {
	var jobs []Job
	userFlag := ""
	if !showAll {
		userFlag = "-u " + targetUser
	}

	cmdStr := fmt.Sprintf("squeue -a -h %s -o '%%i|%%u|%%P|%%j|%%T|%%M|%%C|%%m|%%b|%%N|%%S|%%E|%%R'", userFlag)
	out, err := exec.Command("bash", "-c", cmdStr).Output()
	if err != nil {
		return jobs
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		p := strings.Split(line, "|")
		if len(p) < 13 {
			continue
		}
		cpu, _ := strconv.Atoi(p[6])
		nodeOrReason := p[9]
		if nodeOrReason == "" || nodeOrReason == "N/A" {
			nodeOrReason = p[12]
		}
		start := p[10]
		if start == "" || start == "N/A" {
			start = "Unknown"
		}

		jobs = append(jobs, Job{
			ID:           p[0],
			User:         p[1],
			Part:         p[2],
			Name:         p[3],
			State:        p[4],
			Elapsed:      p[5],
			CPUs:         cpu,
			MemGB:        parseJobMemToMB(p[7]) / 1024,
			GPUs:         parseTRESForGPU(p[8]),
			NodeOrReason: nodeOrReason,
			Start:        start,
			End:          "Unknown",
			Dependency:   formatDependency(p[11]),
		})
	}
	return jobs
}
