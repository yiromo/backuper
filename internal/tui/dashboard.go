package tui

import (
	"context"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"backuper/internal/backup"
)

type DashboardModel struct {
	app     *App
	table   table.Model
	records []*backup.Record
	width   int
	height  int
}

func newDashboard(app *App) DashboardModel {
	t := table.New(
		table.WithColumns(dashboardColumns(80)),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	s := table.DefaultStyles()
	s.Header = styleTableHeader
	s.Selected = styleSelected
	t.SetStyles(s)
	return DashboardModel{app: app, table: t}
}

func dashboardColumns(width int) []table.Column {
	nameW := 20
	typeW := 12
	lastRunW := 20
	statusW := 10
	nextRunW := width - nameW - typeW - lastRunW - statusW - 8
	if nextRunW < 10 {
		nextRunW = 10
	}
	return []table.Column{
		{Title: "Name", Width: nameW},
		{Title: "Type", Width: typeW},
		{Title: "Last Run", Width: lastRunW},
		{Title: "Status", Width: statusW},
		{Title: "Next Run", Width: nextRunW},
	}
}

type dashboardLoadedMsg struct{ records []*backup.Record }

func (m DashboardModel) reload(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		if m.app.histDB == nil {
			return dashboardLoadedMsg{}
		}
		records, err := m.app.histDB.Query(ctx, "", 1000)
		if err != nil {
			return statusMsg{text: "history error: " + err.Error(), isErr: true}
		}
		return dashboardLoadedMsg{records: records}
	}
}

func (m DashboardModel) Init() tea.Cmd { return m.reload(context.Background()) }

func (m DashboardModel) Update(msg tea.Msg) (DashboardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case dashboardLoadedMsg:
		m.records = msg.records
		m.table.SetRows(m.buildRows())
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if row := m.table.SelectedRow(); row != nil {
				return m, switchToRun(row[0])
			}
		}
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m DashboardModel) View() string {
	title := styleTitle.Render("Dashboard")
	help := styleHelp.Render("[enter] run now  [↑/↓] navigate")
	return lipgloss.JoinVertical(lipgloss.Left,
		title,
		m.table.View(),
		help,
	)
}

func (m *DashboardModel) resize(w, h int) DashboardModel {
	m.width = w
	m.height = h
	m.table.SetColumns(dashboardColumns(w))
	m.table.SetHeight(h - 4)
	m.table.SetRows(m.buildRows())
	return *m
}

func (m DashboardModel) buildRows() []table.Row {
	// Build a map of target → last record.
	lastRec := make(map[string]*backup.Record)
	for _, r := range m.records {
		if _, exists := lastRec[r.Target]; !exists {
			lastRec[r.Target] = r
		}
	}

	var rows []table.Row
	for _, tgt := range m.app.cfg.Targets {
		lastRun := "-"
		status := "-"
		if r, ok := lastRec[tgt.Name]; ok {
			lastRun = r.CreatedAt.Format("2006-01-02 15:04")
			if r.Status == "success" {
				status = statusSuccess("ok")
			} else {
				status = statusError("fail")
			}
		}
		nextRun := "-"
		if m.app.sched != nil {
			for _, sched := range m.app.cfg.Schedules {
				if sched.Target == tgt.Name {
					nextRun = m.app.sched.NextRun(tgt.Name, sched.Destination)
					break
				}
			}
		}
		rows = append(rows, table.Row{
			tgt.Name, tgt.Type, lastRun, status, nextRun,
		})
	}
	return rows
}
