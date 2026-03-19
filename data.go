package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"os/user"
)

type Job struct {
	ID           string
	User         string
	Name         string
	State        string
	Part         string
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
		if line == "" { continue }
		parts := strings.Split(line, "|")
		if len(parts) >= 6 {
			allocNodelist := expandNodelist(parts[0])
			jobCPUs, _ := strconv.Atoi(parts[3])
			jobMemMB := parseJobMemToMB(parts[4])
			jobGPUs := parseTRESForGPU(parts[5])

			job := Job{User: parts[1], Name: parts[2], CPUs: jobCPUs}

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

	// 2. 调用 sacct
	// -X: 隐藏多余的 job step (如 .batch)，只看主任务
	// -S now-1days: 限制查询范围为过去 24 小时
	cmdStr := fmt.Sprintf("sacct -X -n -P -S now-1days %s -o JobID,User,Partition,JobName,State,Elapsed,AllocCPUS,ReqMem,ReqTRES,NodeList,Start,End", userFlag)
	out, err := exec.Command("bash", "-c", cmdStr).Output()
	if err != nil { return jobs }

	// 3. 解析输出
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" { continue }
		p := strings.Split(line, "|")
		if len(p) >= 12 {
			cpu, _ := strconv.Atoi(p[6])
			// 提取状态，sacct 返回的状态可能会带有 "CANCELLED by 1001"，只取第一个词
			state := strings.Split(p[4], " ")[0] 
			nodeList := p[9]
			if nodeList == "None" { nodeList = "(Pending/Unknown)" }

			jobs = append(jobs, Job{
				ID: p[0], User: p[1], Part: p[2], Name: p[3], State: state, Elapsed: p[5],
				CPUs: cpu, MemGB: parseJobMemToMB(p[7]) / 1024, GPUs: parseTRESForGPU(p[8]),
				NodeOrReason: nodeList, Start: p[10], End: p[11],
			})
		}
	}
	return jobs
}