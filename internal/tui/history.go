package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"backuper/internal/backup"
)

type historyMode int

const (
	historyModeTable historyMode = iota
	historyModeDetail
	historyModeFilter
)

type HistoryModel struct {
	app     *App
	table   table.Model
	records []*backup.Record
	mode    historyMode
	filter  textinput.Model
	detail  *backup.Record
	width   int
	height  int
}

func newHistory(app *App) HistoryModel {
	t := table.New(
		table.WithColumns(historyColumns(80)),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	s := table.DefaultStyles()
	s.Header = styleTableHeader
	s.Selected = styleSelected
	t.SetStyles(s)

	fi := textinput.New()
	fi.Placeholder = "filter by target name..."
	fi.CharLimit = 50

	return HistoryModel{app: app, table: t, filter: fi}
}

func historyColumns(width int) []table.Column {
	if width >= 80 {
		tsW := 18
		tgtW := 18
		dstW := 18
		statusW := 8
		sizeW := 10
		durW := width - tsW - tgtW - dstW - statusW - sizeW - 10
		if durW < 8 {
			durW = 8
		}
		return []table.Column{
			{Title: "Timestamp", Width: tsW},
			{Title: "Target", Width: tgtW},
			{Title: "Destination", Width: dstW},
			{Title: "Status", Width: statusW},
			{Title: "Size", Width: sizeW},
			{Title: "Duration", Width: durW},
		}
	} else if width >= 60 {
		return []table.Column{
			{Title: "Timestamp", Width: 18},
			{Title: "Target", Width: 18},
			{Title: "Status", Width: 8},
			{Title: "Duration", Width: width - 52},
		}
	} else if width >= 40 {
		return []table.Column{
			{Title: "Timestamp", Width: 18},
			{Title: "Status", Width: 8},
			{Title: "Duration", Width: width - 34},
		}
	}
	return []table.Column{
		{Title: "Timestamp", Width: 18},
		{Title: "Status", Width: width - 26},
	}
}

type historyLoadedMsg struct{ records []*backup.Record }

func (m HistoryModel) reload(ctx context.Context) tea.Cmd {
	filterVal := strings.TrimSpace(m.filter.Value())
	return func() tea.Msg {
		if m.app.histDB == nil {
			return historyLoadedMsg{}
		}
		recs, err := m.app.histDB.Query(ctx, filterVal, 200)
		if err != nil {
			return statusMsg{text: "history query error: " + err.Error(), isErr: true}
		}
		return historyLoadedMsg{records: recs}
	}
}

func (m HistoryModel) Init() tea.Cmd { return m.reload(context.Background()) }

func (m HistoryModel) Update(msg tea.Msg) (HistoryModel, tea.Cmd) {
	switch m.mode {
	case historyModeTable:
		return m.updateTable(msg)
	case historyModeDetail:
		return m.updateDetail(msg)
	case historyModeFilter:
		return m.updateFilter(msg)
	}
	return m, nil
}

func (m HistoryModel) updateTable(msg tea.Msg) (HistoryModel, tea.Cmd) {
	switch msg := msg.(type) {
	case historyLoadedMsg:
		m.records = msg.records
		m.table.SetRows(m.buildRows(m.width))
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			idx := m.table.Cursor()
			if idx < len(m.records) {
				m.detail = m.records[idx]
				m.mode = historyModeDetail
				return m, nil
			}
		case "f":
			m.mode = historyModeFilter
			m.filter.Focus()
			return m, textinput.Blink
		}
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m HistoryModel) updateDetail(msg tea.Msg) (HistoryModel, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "esc", "q", "enter":
			m.mode = historyModeTable
			m.detail = nil
		}
	}
	return m, nil
}

func (m HistoryModel) updateFilter(msg tea.Msg) (HistoryModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.filter.Blur()
			m.mode = historyModeTable
			return m, nil
		case "enter":
			m.filter.Blur()
			m.mode = historyModeTable
			return m, m.reload(context.Background())
		}
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	return m, cmd
}

func (m HistoryModel) View() string {
	switch m.mode {
	case historyModeDetail:
		return m.viewDetail()
	case historyModeFilter:
		return m.viewFilter()
	}
	return m.viewTable()
}

func (m HistoryModel) viewTable() string {
	filterInfo := ""
	if v := strings.TrimSpace(m.filter.Value()); v != "" {
		filterInfo = "  " + styleMuted.Render("filter: "+v)
	}
	title := styleTitle.Render("History") + filterInfo
	help := styleHelp.Render("[enter] view log  [f]ilter  [↑/↓] navigate")
	return lipgloss.JoinVertical(lipgloss.Left, title, m.table.View(), help)
}

func (m HistoryModel) viewDetail() string {
	if m.detail == nil {
		return ""
	}
	r := m.detail
	status := statusSuccess("success")
	if r.Status != "success" {
		status = statusError("failure")
	}
	header := fmt.Sprintf("%s | %s → %s | %s | %s | %dms",
		r.CreatedAt.Format("2006-01-02 15:04:05"),
		r.Target, r.Destination,
		status,
		humanBytes(r.SizeBytes),
		r.DurationMs,
	)

	var logLines []string
	if r.LogOutput != "" {
		logLines = strings.Split(r.LogOutput, "\n")
	}
	if r.ErrorMsg != "" {
		logLines = append(logLines, styleError.Render("ERROR: "+r.ErrorMsg))
	}

	logContent := strings.Join(logLines, "\n")

	return lipgloss.JoinVertical(lipgloss.Left,
		styleTitle.Render("Run Detail"),
		styleBorder.Render(header),
		"",
		logContent,
		"",
		styleHelp.Render("[esc] back"),
	)
}

func (m HistoryModel) viewFilter() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		styleTitle.Render("Filter History"),
		"",
		"  "+m.filter.View(),
		"",
		styleHelp.Render("  [enter] apply  [esc] cancel"),
	)
}

func (m *HistoryModel) resize(w, h int) HistoryModel {
	m.width = w
	m.height = h
	m.table.SetRows(nil)
	m.table.SetColumns(historyColumns(w))
	m.table.SetHeight(h - 4)
	m.table.SetRows(m.buildRows(w))
	return *m
}

func (m HistoryModel) buildRows(width int) []table.Row {
	var rows []table.Row
	for _, r := range m.records {
		status := statusSuccess("ok")
		if r.Status != "success" {
			status = statusError("fail")
		}
		dur := fmt.Sprintf("%dms", r.DurationMs)
		if r.DurationMs > 60000 {
			dur = fmt.Sprintf("%.1fm", float64(r.DurationMs)/60000)
		}
		if width >= 80 {
			rows = append(rows, table.Row{
				r.CreatedAt.Format("2006-01-02 15:04"),
				r.Target,
				r.Destination,
				status,
				humanBytes(r.SizeBytes),
				dur,
			})
		} else if width >= 60 {
			rows = append(rows, table.Row{
				r.CreatedAt.Format("2006-01-02 15:04"),
				r.Target,
				status,
				dur,
			})
		} else if width >= 40 {
			rows = append(rows, table.Row{
				r.CreatedAt.Format("2006-01-02 15:04"),
				status,
				dur,
			})
		} else {
			rows = append(rows, table.Row{
				r.CreatedAt.Format("2006-01-02 15:04"),
				status,
			})
		}
	}
	return rows
}
