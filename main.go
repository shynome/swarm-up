package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shynome/swarm-up/internal/store"
	"github.com/shynome/swarm-up/internal/tui"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "只展示将执行的命令，不做任何改动")
	flag.Usage = func() {
		out := flag.CommandLine.Output()
		fmt.Fprintln(out, "用法: swarm-up [--dry-run] [file]")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "  file  yaml 记录文件，格式为: service_name: { image: ... }")
		fmt.Fprintln(out)
		flag.PrintDefaults()
	}
	flag.Parse()

	file := flag.Arg(0)

	var st *store.Store
	if file != "" {
		s, err := store.Load(file)
		if err != nil {
			fmt.Fprintln(os.Stderr, "错误:", err)
			os.Exit(1)
		}
		st = s
		if !s.Exists() {
			fmt.Fprintf(os.Stderr, "警告: 记录文件 %s 不存在，更新成功后将自动创建\n", file)
		}
	}

	cfg := tui.Config{File: file, DryRun: *dryRun, Store: st}
	p := tea.NewProgram(tui.New(cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}
