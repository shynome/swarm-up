package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shynome/swarm-up/internal/store"
	"github.com/shynome/swarm-up/internal/tui"
)

// version 由构建时通过 -ldflags "-X main.version=..." 注入。
var version = "dev"

// buildVersion 优先使用注入的版本；直接 go build 时回退到 VCS 信息。
func buildVersion() string {
	if version != "dev" {
		return version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}

	var rev string
	dirty := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return version
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		rev += "-dirty"
	}
	return rev
}

func main() {
	dryRun := flag.Bool("dry-run", false, "只展示将执行的命令，不做任何改动")
	showVersion := flag.Bool("version", false, "输出版本号后退出")
	flag.Usage = func() {
		out := flag.CommandLine.Output()
		fmt.Fprintln(out, "用法: swarm-up [--dry-run] [file]")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "  file  yaml 记录文件，格式为: service_name: { image: ... }")
		fmt.Fprintln(out)
		flag.PrintDefaults()
	}
	flag.Parse()

	ver := buildVersion()
	if *showVersion {
		fmt.Println("swarm-up " + ver)
		return
	}

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

	cfg := tui.Config{File: file, DryRun: *dryRun, Store: st, Version: ver}
	p := tea.NewProgram(tui.New(cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}
