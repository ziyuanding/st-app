package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	// 定义 sq 子命令的专属参数
	sqCmd := flag.NewFlagSet("sq", flag.ExitOnError)
	sqAll := sqCmd.Bool("a", false, "显示所有用户的任务")
	sqUser := sqCmd.String("u", "", "显示指定用户的任务")

	// 定义主命令的参数
	defaultPart := flag.String("p", "", "指定默认模式下初始查看的 partition")

	// 自定义帮助信息
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: st [subcommand] [options]\n\n")
		fmt.Fprintf(os.Stderr, "Subcommands:\n")
		fmt.Fprintf(os.Stderr, "  (default)    显示所有分区的详细节点视图，可用 -p <partition> 指定初始分区\n")
		fmt.Fprintf(os.Stderr, "  lsp          List Partitions: 显示所有分区的节点资源热力图矩阵\n")
		fmt.Fprintf(os.Stderr, "  sq           Slurm Queue: 显示队列状态 (支持 -a, -u <user>)\n\n")
	}

	flag.Parse()

	// 解析子命令
	subcommand := ""
	if flag.NArg() > 0 {
		subcommand = flag.Arg(0)
	}

	switch subcommand {
	case "lsp":
		data := getGlobalSlurmData()
		runLspMode(data)
	case "sq":
		sqCmd.Parse(flag.Args()[1:])
		runSqMode(*sqAll, *sqUser)
	case "":
		runDetailedMode(*defaultPart)
	default:
		fmt.Printf("未知子命令: %s\n", subcommand)
		flag.Usage()
		os.Exit(1)
	}
}
