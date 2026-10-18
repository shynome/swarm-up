package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/shynome/swarm-up/internal/docker"
	"github.com/shynome/swarm-up/internal/store"
)

const maxLogLines = 400

// Config 是 TUI 的启动参数。
type Config struct {
	File   string
	DryRun bool
	Client *docker.Client
	Store  *store.Store
}

type state int

const (
	stateList state = iota
	stateEdit
	stateConfirm
	stateRunning
	stateResult
)

type flowStage int

const (
	stagePull flowStage = iota
	stageUpdate
	stageWrite
	stageDone
)

func (s flowStage) label() string {
	switch s {
	case stagePull:
		return "pull 镜像"
	case stageUpdate:
		return "更新 service"
	case stageWrite:
		return "写入记录文件"
	default:
		return "完成"
	}
}

// Model 是 Bubble Tea 的根模型。
type Model struct {
	cfg   Config
	w, h  int
	state state

	list  list.Model
	input textinput.Model
	spin  spinner.Model

	items    []serviceItem
	selected *docker.Service
	newTag   string
	inputErr string

	logs      []string
	stage     flowStage
	flowCh    chan flowEvent
	cancel    context.CancelFunc
	resultErr error

	toast    string
	toastErr bool
}

func New(cfg Config) *Model {
	if cfg.Client == nil {
		cfg.Client = docker.NewClient()
	}

	ti := textinput.New()
	ti.Prompt = "新 tag > "
	ti.Placeholder = "v0.0.3"
	ti.CharLimit = 128
	ti.Width = 48

	sp := spinner.New()
	sp.Spinner = spinner.Line

	m := &Model{cfg: cfg, input: ti, spin: sp, state: stateList}

	delegate := newDelegate()
	m.list = list.New(nil, delegate, 0, 0)
	m.list.SetShowTitle(false)
	m.list.SetShowHelp(false)
	m.list.SetShowStatusBar(true)
	m.list.SetShowPagination(true)
	m.list.SetFilteringEnabled(true)
	m.list.SetStatusBarItemName("service", "services")

	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.loadServices(), textinput.Blink)
}

type servicesMsg struct {
	items []serviceItem
	err   error
}

type flowEvent struct {
	stage   flowStage
	line    string
	service string
	image   string
	warn    string
	err     error
}

type flowClosedMsg struct{}

func (m *Model) loadServices() tea.Cmd {
	client := m.cfg.Client
	st := m.cfg.Store
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		services, err := client.ListServices(ctx)
		if err != nil {
			return servicesMsg{err: err}
		}
		return servicesMsg{items: buildItems(services, st)}
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.list.SetSize(msg.Width, maxInt(4, msg.Height-4))
		return m, nil
	case tea.KeyMsg:
		return m, m.onKey(msg)
	case servicesMsg:
		return m, m.onServices(msg)
	case flowEvent:
		return m, m.onFlowEvent(msg)
	case flowClosedMsg:
		return m, nil
	case spinner.TickMsg:
		if m.state != stateRunning {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}
	return m, m.forward(msg)
}

func (m *Model) forward(msg tea.Msg) tea.Cmd {
	switch m.state {
	case stateList:
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return cmd
	case stateEdit:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return cmd
	}
	return nil
}

func (m *Model) onServices(msg servicesMsg) tea.Cmd {
	if msg.err != nil {
		m.toast = "加载 service 失败: " + msg.err.Error()
		m.toastErr = true
		return nil
	}

	prev := ""
	if it, ok := m.list.SelectedItem().(serviceItem); ok {
		prev = it.svc.Name
	}

	m.items = msg.items
	items := make([]list.Item, len(msg.items))
	for i, it := range msg.items {
		items[i] = it
	}
	cmd := m.list.SetItems(items)

	if prev != "" {
		for i, it := range msg.items {
			if it.svc.Name == prev {
				m.list.Select(i)
				break
			}
		}
	}
	if m.toast == "正在刷新…" {
		m.toast = ""
	}
	return cmd
}

func (m *Model) onKey(key tea.KeyMsg) tea.Cmd {
	switch m.state {
	case stateList:
		return m.listKey(key)
	case stateEdit:
		return m.editKey(key)
	case stateConfirm:
		return m.confirmKey(key)
	case stateRunning:
		return m.runningKey(key)
	case stateResult:
		return m.resultKey(key)
	}
	return nil
}

func (m *Model) listKey(key tea.KeyMsg) tea.Cmd {
	// 过滤输入中，所有按键交给 list（包括 / 输入、esc 取消、enter 应用）。
	if m.list.FilterState() == list.Filtering {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(key)
		return cmd
	}

	switch key.String() {
	case "q", "ctrl+c":
		return tea.Quit
	case "r":
		m.toast = "正在刷新…"
		m.toastErr = false
		return m.loadServices()
	case "enter":
		it, ok := m.list.SelectedItem().(serviceItem)
		if !ok {
			return nil
		}
		if it.svc.Ref.Repo == "" {
			m.toast = "无法解析镜像仓库: " + it.svc.Image
			m.toastErr = true
			return nil
		}
		svc := it.svc
		m.selected = &svc
		m.newTag = ""
		m.inputErr = ""
		m.toast = ""
		m.input.SetValue("")
		m.input.Placeholder = "当前 " + svc.Ref.Tag
		m.input.Focus()
		m.state = stateEdit
		return textinput.Blink
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(key)
	return cmd
}

func (m *Model) editKey(key tea.KeyMsg) tea.Cmd {
	switch key.String() {
	case "esc":
		m.input.Blur()
		m.state = stateList
		return nil
	case "ctrl+c":
		return tea.Quit
	case "enter":
		tag := strings.TrimSpace(m.input.Value())
		if err := docker.ValidateTag(tag); err != nil {
			m.inputErr = err.Error()
			return nil
		}
		m.newTag = tag
		m.inputErr = ""
		m.input.Blur()
		m.state = stateConfirm
		return nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(key)
	m.inputErr = ""
	return cmd
}

func (m *Model) confirmKey(key tea.KeyMsg) tea.Cmd {
	switch key.String() {
	case "esc":
		m.state = stateEdit
		m.input.Focus()
		return textinput.Blink
	case "ctrl+c":
		return tea.Quit
	case "enter":
		m.logs = nil
		m.resultErr = nil
		m.stage = stagePull
		m.state = stateRunning
		return tea.Batch(m.startFlow(), m.spin.Tick)
	}
	return nil
}

func (m *Model) runningKey(key tea.KeyMsg) tea.Cmd {
	switch key.String() {
	case "ctrl+c":
		if m.cancel != nil {
			m.cancel()
			m.cancel = nil
		}
		return tea.Quit
	case "esc":
		if m.cancel != nil {
			m.cancel()
			m.cancel = nil
		}
		m.toast = "已取消"
		m.toastErr = true
		m.state = stateList
		return m.loadServices()
	}
	return nil
}

func (m *Model) resultKey(key tea.KeyMsg) tea.Cmd {
	switch key.String() {
	case "ctrl+c", "q":
		return tea.Quit
	case "enter", "esc":
		m.state = stateList
		return m.loadServices()
	}
	return nil
}

func (m *Model) onFlowEvent(ev flowEvent) tea.Cmd {
	if m.state != stateRunning {
		return nil
	}
	if ev.line != "" {
		m.appendLog(ev.line)
	}
	if ev.stage != stageDone {
		m.stage = ev.stage
		return waitFlow(m.flowCh)
	}

	m.cancel = nil
	if ev.err != nil {
		m.resultErr = ev.err
		m.state = stateResult
		return nil
	}

	// 成功：回到列表并刷新
	m.state = stateList
	m.toastErr = false
	msg := "✓ 已更新 " + ev.service
	if ev.image != "" {
		msg += " → " + shortImage(ev.image)
	}
	if ev.warn != "" {
		msg += "（" + ev.warn + "）"
		m.toastErr = false
	}
	m.toast = msg
	return m.loadServices()
}

func (m *Model) appendLog(line string) {
	m.logs = append(m.logs, line)
	if len(m.logs) > maxLogLines {
		m.logs = m.logs[len(m.logs)-maxLogLines:]
	}
}

// startFlow 在后台执行 pull → service update → 写 yaml，并通过事件通道汇报进度。
func (m *Model) startFlow() tea.Cmd {
	ch := make(chan flowEvent, 512)
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.flowCh = ch

	svc := *m.selected
	tag := m.newTag
	client := m.cfg.Client
	st := m.cfg.Store
	dryRun := m.cfg.DryRun

	next := svc.Ref.WithTag(tag)
	pending := next.RepoTag() + "@sha256:<pull 后解析>"

	go func() {
		defer close(ch)
		defer cancel()

		send := func(ev flowEvent) {
			select {
			case ch <- ev:
			default: // UI 已退出时丢弃，避免阻塞
			}
		}
		onLine := func(stage flowStage) func(string) {
			return func(line string) {
				send(flowEvent{stage: stage, line: line})
			}
		}
		fail := func(err error) {
			send(flowEvent{stage: stageDone, service: svc.Name, err: err})
		}

		// 1. pull
		send(flowEvent{stage: stagePull, line: "$ " + client.Bin() + " pull " + next.RepoTag()})

		if dryRun {
			send(flowEvent{stage: stagePull, line: "（dry-run：不执行 pull，无法解析 digest）"})
			send(flowEvent{stage: stageUpdate, line: "$ " + client.Bin() + " " + strings.Join(docker.UpdateArgs(svc.Name, pending), " ")})
			if st == nil {
				send(flowEvent{stage: stageWrite, line: "⚠ 未指定记录文件，跳过 yaml 记录"})
			} else {
				send(flowEvent{stage: stageWrite, line: "$ " + st.Path() + ": " + svc.Name + ".image = " + pending})
			}
			send(flowEvent{stage: stageDone, service: svc.Name, warn: "dry-run：未执行任何命令"})
			return
		}

		digest, err := client.Pull(ctx, next, onLine(stagePull))
		if err != nil {
			fail(err)
			return
		}
		image := next.RepoTag() + "@" + digest

		// 2. service update
		send(flowEvent{stage: stageUpdate, line: "$ " + client.Bin() + " " + strings.Join(docker.UpdateArgs(svc.Name, image), " ")})
		if err := client.UpdateService(ctx, svc.Name, image, onLine(stageUpdate)); err != nil {
			fail(err)
			return
		}

		// 3. yaml
		warn := ""
		if st == nil {
			warn = "未指定记录文件，镜像未记录"
			send(flowEvent{stage: stageWrite, line: "⚠ " + warn})
		} else {
			send(flowEvent{stage: stageWrite, line: "$ " + st.Path() + ": " + svc.Name + ".image = " + image})
			if err := st.SetImage(svc.Name, image); err != nil {
				fail(err)
				return
			}
		}

		send(flowEvent{stage: stageDone, service: svc.Name, image: image, warn: warn})
	}()

	return waitFlow(ch)
}

func waitFlow(ch <-chan flowEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return flowClosedMsg{}
		}
		return ev
	}
}
