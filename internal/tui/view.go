package tui

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"

	"github.com/shynome/swarm-up/internal/docker"
	"github.com/shynome/swarm-up/internal/store"
)

var (
	styleTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	styleDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	styleSub   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	styleOK    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	styleWarn  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	styleErr   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	styleHelp  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	styleBox   = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 2)
)

type syncStatus int

const (
	statusMissing syncStatus = iota
	statusInSync
	statusMismatch
)

type serviceItem struct {
	svc      docker.Service
	status   syncStatus
	recorded string
}

func (i serviceItem) Title() string { return i.svc.Name }

func (i serviceItem) Description() string {
	desc := shortImage(i.svc.Image)
	switch i.status {
	case statusInSync:
		desc += "  ✓ 同步"
	case statusMismatch:
		desc += "  ⚠ yaml: " + shortImage(i.recorded)
	default:
		desc += "  ⊘ 未记录"
	}
	return desc
}

func (i serviceItem) FilterValue() string {
	return i.svc.Name + " " + i.svc.Image + " " + i.recorded
}

func newDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.ShowDescription = true
	d.SetSpacing(0)
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.
		Foreground(lipgloss.Color("229")).
		BorderForeground(lipgloss.Color("39"))
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.
		Foreground(lipgloss.Color("252")).
		BorderForeground(lipgloss.Color("39"))
	return d
}

func buildItems(services []docker.Service, st *store.Store) []serviceItem {
	byName := make(map[string]docker.Service, len(services))
	for _, svc := range services {
		byName[svc.Name] = svc
	}

	// 记录文件存在时，优先按文件中的定义顺序排列，其余服务再按名称排序。
	ordered := make([]docker.Service, 0, len(services))
	seen := make(map[string]bool, len(services))
	if st != nil {
		for _, name := range st.Order() {
			svc, ok := byName[name]
			if !ok || seen[name] {
				continue
			}
			seen[name] = true
			ordered = append(ordered, svc)
		}
	}

	rest := make([]docker.Service, 0, len(services)-len(ordered))
	for _, svc := range services {
		if !seen[svc.Name] {
			rest = append(rest, svc)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].Name < rest[j].Name })
	ordered = append(ordered, rest...)

	items := make([]serviceItem, 0, len(ordered))
	for _, svc := range ordered {
		it := serviceItem{svc: svc, status: statusMissing}
		if st != nil {
			if rec, ok := st.Lookup(svc.Name); ok {
				it.recorded = rec
				if rec == svc.Image {
					it.status = statusInSync
				} else {
					it.status = statusMismatch
				}
			}
		}
		items = append(items, it)
	}
	return items
}

func (m *Model) View() string {
	if m.w <= 0 {
		return "加载中…"
	}
	switch m.state {
	case stateEdit:
		return m.viewEdit()
	case stateConfirm:
		return m.viewConfirm()
	case stateRunning:
		return m.viewRunning()
	case stateUpdateConfirm:
		return m.viewUpdateConfirm()
	case stateWriteConfirm:
		return m.viewWriteConfirm()
	case stateResult:
		return m.viewResult()
	default:
		return m.viewList()
	}
}

func (m *Model) viewList() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		m.header(),
		m.list.View(),
		m.footer("↑↓ 选择", "/ 搜索", "enter 更新", "r 刷新", "q 退出"),
	)
}

func (m *Model) viewEdit() string {
	svc := m.selected
	if svc == nil {
		return m.viewList()
	}

	rows := []string{
		label("服务") + svc.Name,
		label("当前") + shortImage(svc.Image),
		label("仓库") + svc.Ref.Repo,
		"",
		m.input.View(),
		styleDim.Render("默认是当前 tag，直接改成新版本后回车"),
	}
	if m.inputErr != "" {
		rows = append(rows, "", styleErr.Render(m.inputErr))
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		m.header(),
		"",
		m.boxRows(rows),
		"",
		m.footer("enter 下一步", "esc 返回"),
	)
}

func (m *Model) viewConfirm() string {
	svc := m.selected
	if svc == nil {
		return m.viewList()
	}

	next := svc.Ref.WithTag(m.newTag)
	pending := next.RepoTag() + "@sha256:<pull 后解析>"
	rows := []string{
		label("服务") + svc.Name,
		label("当前") + shortImage(svc.Image),
		label("目标") + next.RepoTag(),
		"",
		styleSub.Render("步骤 1/3 · 拉取镜像"),
		"  " + m.cfg.Client.Bin() + " pull " + next.RepoTag(),
	}

	if m.cfg.DryRun {
		rows = append(rows,
			"",
			styleSub.Render("步骤 2/3 · 更新 service（不执行）"),
			"  "+m.cfg.Client.Bin()+" "+strings.Join(docker.UpdateArgs(svc.Name, pending), " "),
		)
		if m.cfg.Store != nil {
			rows = append(rows,
				"",
				styleSub.Render("步骤 3/3 · 写入记录文件（不执行）"),
				"  "+m.cfg.Store.Path()+": "+svc.Name+".image = "+pending,
			)
		} else {
			rows = append(rows, "", styleWarn.Render("未指定记录文件，不会写入 yaml"))
		}
		rows = append(rows, "", styleWarn.Render("dry-run：只展示命令，不执行"))
	} else {
		rows = append(rows, "", styleDim.Render("pull 完成后会再次确认 update 命令"))
		if m.cfg.Store == nil {
			rows = append(rows, styleWarn.Render("未指定记录文件，更新结果不会写入 yaml"))
		}
	}

	keys := []string{"enter 开始 pull", "esc 返回"}
	if m.cfg.DryRun {
		keys = []string{"enter 结束", "esc 返回"}
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		m.header(),
		"",
		m.boxRows(rows),
		"",
		m.footer(keys...),
	)
}

// viewUpdateConfirm 展示 pull 后得到的完整镜像和将要执行的 update 命令。
func (m *Model) viewUpdateConfirm() string {
	svc := m.selected
	if svc == nil || m.pendingImage == "" {
		return m.viewList()
	}

	cmdline := m.cfg.Client.Bin() + " " + strings.Join(docker.UpdateArgs(svc.Name, m.pendingImage), " ")
	rows := []string{
		label("服务") + svc.Name,
		label("当前") + shortImage(svc.Image),
		label("新镜像") + shortImage(m.pendingImage),
		label("完整") + m.pendingImage,
		"",
		styleSub.Render("步骤 2/3 · 确认更新命令"),
		"  " + cmdline,
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		m.header(),
		"",
		m.boxRows(rows),
		"",
		m.footer("enter 执行", "esc 取消"),
	)
}

// viewWriteConfirm 在 service 更新成功后确认写入记录文件。
func (m *Model) viewWriteConfirm() string {
	svc := m.selected
	if svc == nil || m.cfg.Store == nil {
		return m.viewList()
	}

	rows := []string{
		label("服务") + svc.Name,
		label("新镜像") + shortImage(m.pendingImage),
		"",
		styleSub.Render("步骤 3/3 · 确认写入记录文件"),
		"  " + m.cfg.Store.Path(),
		"  " + svc.Name + ".image = " + m.pendingImage,
		"",
		styleDim.Render("esc 跳过写入（本次更新不会被记录）"),
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		m.header(),
		"",
		m.boxRows(rows),
		"",
		m.footer("enter 写入", "esc 跳过"),
	)
}

func (m *Model) viewRunning() string {
	head := styleTitle.Render("执行中") + "  " + m.spin.View() + "  " + m.stage.label()
	return lipgloss.JoinVertical(lipgloss.Left,
		m.header(),
		"",
		head,
		"",
		m.visibleLogs(maxInt(3, m.h-7)),
		"",
		m.footer("esc 取消", "ctrl+c 退出"),
	)
}

func (m *Model) viewResult() string {
	title := styleOK.Render("✓ 完成")
	if m.resultErr != nil {
		title = styleErr.Render("✗ 更新失败")
	}

	rows := []string{m.header(), "", title}
	if m.resultErr != nil {
		for _, line := range strings.Split(m.resultErr.Error(), "\n") {
			rows = append(rows, wrapText(line, m.lineWidth())...)
		}
	}
	rows = append(rows, "", m.visibleLogs(maxInt(3, m.h-9)), "")
	rows = append(rows, m.footer("enter 返回列表", "q 退出"))
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (m *Model) header() string {
	parts := []string{styleTitle.Render("swarm-up")}
	if m.cfg.Version != "" {
		parts = append(parts, styleDim.Render(m.cfg.Version))
	}
	if m.cfg.DryRun {
		parts = append(parts, styleWarn.Render("dry-run"))
	}

	if m.cfg.Store == nil {
		parts = append(parts, styleWarn.Render("未指定记录文件，更新结果不会写入 yaml"))
	} else {
		ok, mismatch, missing := m.counts()
		parts = append(parts,
			styleDim.Render(m.cfg.Store.Path()),
			fmt.Sprintf("✓ %d  ⚠ %d  ⊘ %d", ok, mismatch, missing),
		)
		if !m.cfg.Store.Exists() {
			parts = append(parts, styleWarn.Render("文件不存在，成功后自动创建"))
		}
	}
	return truncate(strings.Join(parts, "  "), m.lineWidth())
}

func (m *Model) counts() (ok, mismatch, missing int) {
	for _, it := range m.items {
		switch it.status {
		case statusInSync:
			ok++
		case statusMismatch:
			mismatch++
		default:
			missing++
		}
	}
	return ok, mismatch, missing
}

func (m *Model) footer(keys ...string) string {
	help := styleHelp.Render(strings.Join(keys, "  "))
	if m.toast == "" {
		return truncate(help, m.lineWidth())
	}

	style := styleOK
	if m.toastErr {
		style = styleErr
	}
	return truncate(help, m.lineWidth()) + "\n" + truncate(style.Render(m.toast), m.lineWidth())
}

// lineWidth 预留一列，避免写满整行时终端自动换行。
func (m *Model) lineWidth() int {
	w := m.w - 1
	if w < 10 {
		w = 10
	}
	return w
}

func (m *Model) visibleLogs(n int) string {
	if n < 1 {
		n = 1
	}
	logs := m.logs
	if len(logs) > n {
		logs = logs[len(logs)-n:]
	}
	if len(logs) == 0 {
		return styleDim.Render("…")
	}
	lines := make([]string, len(logs))
	for i, line := range logs {
		lines[i] = truncate(line, m.w-2)
	}
	return strings.Join(lines, "\n")
}

func shortImage(raw string) string {
	ref, err := docker.ParseRef(raw)
	if err != nil {
		return raw
	}
	s := ref.RepoTag()
	if ref.Digest != "" {
		d := strings.TrimPrefix(ref.Digest, "sha256:")
		if len(d) > 8 {
			d = d[:8]
		}
		s += "#" + d
	}
	return s
}

func label(s string) string {
	return styleDim.Width(8).Render(s)
}

// boxRows 将内容按终端宽度换行后放进圆角边框，避免长命令撑破边框。
func (m *Model) boxRows(rows []string) string {
	w := m.w - 7 // 边框 2 + 左右 padding 4 + 预留 1 列防止终端自动换行
	if w < 10 {
		w = 10
	}

	var out []string
	for _, row := range rows {
		if row == "" {
			out = append(out, "")
			continue
		}
		out = append(out, wrapText(row, w)...)
	}
	return styleBox.Render(strings.Join(out, "\n"))
}

// wrapText 按显示宽度换行（中文按 2 列计算），优先在空格处断开。
func wrapText(s string, w int) []string {
	if w <= 0 {
		return []string{s}
	}

	var lines []string
	rest := s
	for {
		if lipgloss.Width(rest) <= w {
			lines = append(lines, strings.TrimRight(rest, " "))
			break
		}

		cut, lastSpace, width := 0, -1, 0
		for i, r := range rest {
			rw := lipgloss.Width(string(r))
			if width+rw > w {
				if r == ' ' {
					// 空格恰好落在边界上时，直接在这里断行
					lastSpace = i
				}
				break
			}
			width += rw
			cut = i + len(string(r))
			if r == ' ' {
				lastSpace = cut
			}
		}
		if cut == 0 {
			_, size := utf8.DecodeRuneInString(rest)
			cut = size
		}

		if lastSpace > 0 {
			lines = append(lines, strings.TrimRight(rest[:lastSpace], " "))
			rest = strings.TrimLeft(rest[lastSpace:], " ")
		} else {
			lines = append(lines, strings.TrimRight(rest[:cut], " "))
			rest = rest[cut:]
		}
	}
	return lines
}

func truncate(s string, w int) string {
	if w <= 0 || lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	for i := len(r) - 1; i >= 0; i-- {
		cand := string(r[:i]) + "…"
		if lipgloss.Width(cand) <= w {
			return cand
		}
	}
	return "…"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
