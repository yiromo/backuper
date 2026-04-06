// Package tui implements the bubbletea-based terminal UI.
package tui

import (
	"context"
	"log/slog"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"backuper/internal/backup"
	"backuper/internal/config"
	"backuper/internal/scheduler"
	"backuper/internal/secrets"
)

type viewID int

const (
	viewDashboard viewID = iota
	viewTargets
	viewSchedules
	viewHistory
	viewRun
	viewSecrets
	viewHelp
)

type App struct {
	view   viewID
	width  int
	height int

	dashboard DashboardModel
	targets   TargetsModel
	schedules SchedulesModel
	history   HistoryModel
	runView   RunModel
	secretsV  SecretsModel

	statusMsg string
	statusErr bool

	cfg    *config.Config
	store  secrets.Store
	sched  *scheduler.Scheduler
	histDB *backup.HistoryDB
	logger *slog.Logger
}

func New(
	cfg *config.Config,
	store secrets.Store,
	sched *scheduler.Scheduler,
	histDB *backup.HistoryDB,
	logger *slog.Logger,
) App {
	a := App{
		view:   viewDashboard,
		cfg:    cfg,
		store:  store,
		sched:  sched,
		histDB: histDB,
		logger: logger,
	}
	a.dashboard = newDashboard(&a)
	a.targets = newTargets(&a)
	a.schedules = newSchedules(&a)
	a.history = newHistory(&a)
	a.runView = newRun(&a)
	a.secretsV = newSecrets(&a)
	return a
}

func Run(app App) error {
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

func (a App) Init() tea.Cmd {
	return tea.Batch(
		a.dashboard.Init(),
		a.history.Init(),
	)
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		bodyW := msg.Width - 4 // horizontal padding in renderBody
		bodyH := msg.Height - 3
		a.dashboard = a.dashboard.resize(bodyW, bodyH)
		a.targets = a.targets.resize(bodyW, bodyH)
		a.schedules = a.schedules.resize(bodyW, bodyH)
		a.history = a.history.resize(bodyW, bodyH)
		a.runView = a.runView.resize(bodyW, bodyH)
		return a, nil

	case switchViewMsg:
		a.view = msg.view
		switch msg.view {
		case viewHistory:
			return a, a.history.reload(context.Background())
		case viewDashboard:
			return a, a.dashboard.reload(context.Background())
		case viewRun:
			if msg.target != "" {
				a.runView = a.runView.withTarget(msg.target)
			}
		}
		return a, nil

	case statusMsg:
		a.statusMsg = msg.text
		a.statusErr = msg.isErr
		return a, nil

	case tea.KeyMsg:
		// Global key bindings (only when not in a sub-view that captures input).
		if !a.currentViewCapturesInput() {
			switch msg.String() {
			case "d":
				a.view = viewDashboard
				return a, a.dashboard.reload(context.Background())
			case "t":
				a.view = viewTargets
				return a, nil
			case "s":
				a.view = viewSchedules
				return a, nil
			case "h":
				a.view = viewHistory
				return a, a.history.reload(context.Background())
			case "r":
				a.view = viewRun
				return a, nil
			case "S":
				a.view = viewSecrets
				return a, nil
			case "?":
				a.view = viewHelp
				return a, nil
			case "q", "ctrl+c":
				return a, tea.Quit
			}
		} else if msg.String() == "ctrl+c" {
			return a, tea.Quit
		}
	}

	switch a.view {
	case viewDashboard:
		var cmd tea.Cmd
		a.dashboard, cmd = a.dashboard.Update(msg)
		cmds = append(cmds, cmd)
	case viewTargets:
		var cmd tea.Cmd
		a.targets, cmd = a.targets.Update(msg)
		cmds = append(cmds, cmd)
	case viewSchedules:
		var cmd tea.Cmd
		a.schedules, cmd = a.schedules.Update(msg)
		cmds = append(cmds, cmd)
	case viewHistory:
		var cmd tea.Cmd
		a.history, cmd = a.history.Update(msg)
		cmds = append(cmds, cmd)
	case viewRun:
		var cmd tea.Cmd
		a.runView, cmd = a.runView.Update(msg)
		cmds = append(cmds, cmd)
	case viewSecrets:
		var cmd tea.Cmd
		a.secretsV, cmd = a.secretsV.Update(msg)
		cmds = append(cmds, cmd)
	}

	return a, tea.Batch(cmds...)
}

func (a App) View() string {
	if a.width == 0 {
		return "Loading..."
	}
	nav := a.renderNav()
	body := a.renderBody()
	status := a.renderStatus()

	return lipgloss.JoinVertical(lipgloss.Left, nav, body, status)
}

func (a App) renderNav() string {
	items := []struct {
		key, label string
		view       viewID
	}{
		{"d", "dashboard", viewDashboard},
		{"t", "targets", viewTargets},
		{"s", "schedules", viewSchedules},
		{"h", "history", viewHistory},
		{"r", "run", viewRun},
		{"S", "secrets", viewSecrets},
		{"?", "help", viewHelp},
		{"q", "quit", -1},
	}
	var parts []string
	for _, item := range items {
		parts = append(parts, navItem(item.key, item.label, a.view == item.view))
	}
	bar := strings.Join(parts, "")
	return lipgloss.NewStyle().
		Width(a.width).
		Background(colorBgLight).
		Render(bar)
}

func (a App) renderBody() string {
	h := a.height - 3 // nav + status
	if h < 1 {
		h = 1
	}
	contentW := a.width - 4
	if contentW < 20 {
		contentW = 20
	}
	style := lipgloss.NewStyle().
		Width(contentW).
		Height(h).
		Background(colorBg).
		Padding(0, 2)
	switch a.view {
	case viewDashboard:
		return style.Render(a.dashboard.View())
	case viewTargets:
		return style.Render(a.targets.View())
	case viewSchedules:
		return style.Render(a.schedules.View())
	case viewHistory:
		return style.Render(a.history.View())
	case viewRun:
		return style.Render(a.runView.View())
	case viewSecrets:
		return style.Render(a.secretsV.View())
	case viewHelp:
		return style.Render(a.renderHelp())
	default:
		return style.Render("")
	}
}

func (a App) renderStatus() string {
	msg := a.statusMsg
	if msg == "" {
		msg = "Press [?] for help"
	}
	st := styleStatusBar
	if a.statusErr {
		st = styleStatusErr
	}
	return st.Width(a.width).Render(msg)
}

func (a App) renderHelp() string {
	keyStyle := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Width(12)
	descStyle := lipgloss.NewStyle().Foreground(colorFg)

	var sb strings.Builder
	sb.WriteString(styleTitle.Render("Keyboard Shortcuts"))

	sb.WriteString(styleAccent.Render("Navigation") + "\n")
	for _, item := range [][2]string{
		{"d", "Dashboard — overview of all targets"},
		{"t", "Targets — manage backup sources"},
		{"s", "Schedules — manage cron schedules"},
		{"h", "History — view past backup runs"},
		{"r", "Run — run a backup interactively"},
		{"S", "Secrets — manage encrypted secrets"},
		{"q", "Quit"},
	} {
		sb.WriteString("  " + keyStyle.Render(item[0]) + descStyle.Render(item[1]) + "\n")
	}

	sb.WriteString("\n" + styleAccent.Render("Within Views") + "\n")
	for _, item := range [][2]string{
		{"↑/↓ j/k", "Navigate rows"},
		{"enter", "Select / confirm"},
		{"a", "Add new item"},
		{"e", "Edit selected item"},
		{"D", "Delete selected item"},
		{"f", "Filter (history view)"},
		{"esc", "Cancel / go back"},
	} {
		sb.WriteString("  " + keyStyle.Render(item[0]) + descStyle.Render(item[1]) + "\n")
	}

	return sb.String()
}

func (a App) currentViewCapturesInput() bool {
	switch a.view {
	case viewTargets:
		return a.targets.capturesInput()
	case viewSecrets:
		return a.secretsV.capturesInput()
	case viewRun:
		return a.runView.capturesInput()
	}
	return false
}

type switchViewMsg struct {
	view   viewID
	target string // optional pre-selected target for viewRun
}

type statusMsg struct {
	text  string
	isErr bool
}

func setStatus(text string) tea.Cmd {
	return func() tea.Msg { return statusMsg{text: text} }
}

func setStatusErr(text string) tea.Cmd {
	return func() tea.Msg { return statusMsg{text: text, isErr: true} }
}

func switchTo(v viewID) tea.Cmd {
	return func() tea.Msg { return switchViewMsg{view: v} }
}

func switchToRun(target string) tea.Cmd {
	return func() tea.Msg { return switchViewMsg{view: viewRun, target: target} }
}
