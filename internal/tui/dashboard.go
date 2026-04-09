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
	return DashboardModel{app: app, table: t, width: 80}
}

func dashboardColumns(width int) []table.Column {
	nextRunW := width - 70
	if nextRunW < 10 {
		nextRunW = 10
	}
	nextW60 := width - 58
	if nextW60 < 10 {
		nextW60 = 10
	}
	nextW40 := width - 38
	if nextW40 < 10 {
		nextW40 = 10
	}

	if width >= 80 {
		return []table.Column{
			{Title: "Name", Width: 20},
			{Title: "Type", Width: 12},
			{Title: "Last Run", Width: 20},
			{Title: "Status", Width: 10},
			{Title: "Next Run", Width: nextRunW},
		}
	} else if width >= 60 {
		return []table.Column{
			{Title: "Name", Width: 20},
			{Title: "Last Run", Width: 20},
			{Title: "Status", Width: 10},
			{Title: "Next Run", Width: nextW60},
		}
	} else if width >= 40 {
		return []table.Column{
			{Title: "Name", Width: 20},
			{Title: "Status", Width: 10},
			{Title: "Next Run", Width: nextW40},
		}
	}
	nameW := width - 15
	if nameW < 10 {
		nameW = 10
	}
	return []table.Column{
		{Title: "Name", Width: nameW},
		{Title: "Status", Width: 10},
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
		m.table.SetRows(m.buildRows(m.width))
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
	help := styleHelp.Render("[enter] run now  [↑/↓] navigate")
	return lipgloss.JoinVertical(lipgloss.Left,
		renderLogo(),
		m.table.View(),
		help,
	)
}

func (m *DashboardModel) resize(w, h int) DashboardModel {
	m.width = w
	m.height = h
	m.table.SetRows(nil)
	m.table.SetColumns(dashboardColumns(w))
	m.table.SetHeight(h - 4)
	m.table.SetRows(m.buildRows(w))
	return *m
}

func (m DashboardModel) buildRows(width int) []table.Row {
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

		if width >= 80 {
			rows = append(rows, table.Row{tgt.Name, tgt.Engine + "/" + tgt.Runtime, lastRun, status, nextRun})
		} else if width >= 60 {
			rows = append(rows, table.Row{tgt.Name, lastRun, status, nextRun})
		} else if width >= 40 {
			rows = append(rows, table.Row{tgt.Name, status, nextRun})
		} else {
			rows = append(rows, table.Row{tgt.Name, status})
		}
	}
	return rows
}
