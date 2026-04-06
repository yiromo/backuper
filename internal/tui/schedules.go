package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"backuper/internal/config"
)

type schedulesMode int

const (
	schedulesModeList schedulesMode = iota
	schedulesModeAdd
	schedulesModeEdit
	schedulesModeDeleteConfirm
)

type SchedulesModel struct {
	app    *App
	table  table.Model
	mode   schedulesMode
	form   scheduleForm
	errMsg string
	width  int
	height int
}

type scheduleForm struct {
	fields  []textinput.Model
	labels  []string
	cursor  int
	editIdx int
}

func newSchedules(app *App) SchedulesModel {
	t := table.New(
		table.WithColumns(schedulesColumns(80)),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	s := table.DefaultStyles()
	s.Header = styleTableHeader
	s.Selected = styleSelected
	t.SetStyles(s)
	m := SchedulesModel{app: app, table: t}
	m.refreshTable()
	return m
}

func schedulesColumns(width int) []table.Column {
	tgtW := 18
	dstW := 18
	cronW := 18
	compW := 8
	keepW := 8
	nextW := width - tgtW - dstW - cronW - compW - keepW - 10
	if nextW < 10 {
		nextW = 10
	}
	return []table.Column{
		{Title: "Target", Width: tgtW},
		{Title: "Destination", Width: dstW},
		{Title: "Cron", Width: cronW},
		{Title: "Compress", Width: compW},
		{Title: "Keep", Width: keepW},
		{Title: "Next Run", Width: nextW},
	}
}

func (m *SchedulesModel) refreshTable() {
	var rows []table.Row
	for _, s := range m.app.cfg.Schedules {
		next := "-"
		if m.app.sched != nil {
			next = m.app.sched.NextRun(s.Target, s.Destination)
		}
		keep := fmt.Sprintf("%d", s.Retention.KeepLast)
		rows = append(rows, table.Row{s.Target, s.Destination, s.Cron, s.Compress, keep, next})
	}
	m.table.SetRows(rows)
}

func (m SchedulesModel) Update(msg tea.Msg) (SchedulesModel, tea.Cmd) {
	switch m.mode {
	case schedulesModeList:
		return m.updateList(msg)
	case schedulesModeAdd, schedulesModeEdit:
		return m.updateForm(msg)
	case schedulesModeDeleteConfirm:
		return m.updateDelete(msg)
	}
	return m, nil
}

func (m SchedulesModel) updateList(msg tea.Msg) (SchedulesModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "a":
			m.mode = schedulesModeAdd
			m.form = newScheduleForm(nil, -1)
			return m, textinput.Blink
		case "e":
			idx := m.table.Cursor()
			if idx < len(m.app.cfg.Schedules) {
				s := m.app.cfg.Schedules[idx]
				m.mode = schedulesModeEdit
				m.form = newScheduleForm(&s, idx)
				return m, textinput.Blink
			}
		case "D":
			if len(m.app.cfg.Schedules) > 0 {
				m.mode = schedulesModeDeleteConfirm
			}
		}
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m SchedulesModel) updateForm(msg tea.Msg) (SchedulesModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.mode = schedulesModeList
			m.errMsg = ""
			return m, nil
		case "tab", "down":
			m.form.cursor = (m.form.cursor + 1) % len(m.form.fields)
			for i := range m.form.fields {
				m.form.fields[i].Blur()
			}
			m.form.fields[m.form.cursor].Focus()
			return m, textinput.Blink
		case "shift+tab", "up":
			m.form.cursor = (m.form.cursor - 1 + len(m.form.fields)) % len(m.form.fields)
			for i := range m.form.fields {
				m.form.fields[i].Blur()
			}
			m.form.fields[m.form.cursor].Focus()
			return m, textinput.Blink
		case "enter":
			if err := m.saveForm(); err != nil {
				m.errMsg = err.Error()
				return m, nil
			}
			m.mode = schedulesModeList
			m.errMsg = ""
			m.refreshTable()
			return m, setStatus("Schedule saved.")
		}
	}
	var cmd tea.Cmd
	m.form.fields[m.form.cursor], cmd = m.form.fields[m.form.cursor].Update(msg)
	return m, cmd
}

func (m SchedulesModel) updateDelete(msg tea.Msg) (SchedulesModel, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "y", "Y":
			idx := m.table.Cursor()
			if idx < len(m.app.cfg.Schedules) {
				m.app.cfg.Schedules = append(m.app.cfg.Schedules[:idx], m.app.cfg.Schedules[idx+1:]...)
				m.refreshTable()
			}
			m.mode = schedulesModeList
			return m, setStatus("Schedule deleted.")
		case "n", "N", "esc":
			m.mode = schedulesModeList
		}
	}
	return m, nil
}

func (m SchedulesModel) saveForm() error {
	vals := make([]string, len(m.form.fields))
	for i, f := range m.form.fields {
		vals[i] = strings.TrimSpace(f.Value())
	}
	if vals[0] == "" {
		return fmt.Errorf("target is required")
	}
	if vals[1] == "" {
		return fmt.Errorf("destination is required")
	}
	if vals[2] == "" {
		return fmt.Errorf("cron expression is required")
	}
	keepLast := 0
	if vals[5] != "" {
		fmt.Sscanf(vals[5], "%d", &keepLast)
	}
	sched := config.ScheduleConfig{
		Target:      vals[0],
		Destination: vals[1],
		Cron:        vals[2],
		Compress:    vals[3],
		TmpDir:      vals[4],
		Retention:   config.RetentionConfig{KeepLast: keepLast},
	}
	if m.form.editIdx >= 0 && m.form.editIdx < len(m.app.cfg.Schedules) {
		m.app.cfg.Schedules[m.form.editIdx] = sched
	} else {
		m.app.cfg.Schedules = append(m.app.cfg.Schedules, sched)
	}
	return nil
}

func (m SchedulesModel) View() string {
	switch m.mode {
	case schedulesModeAdd, schedulesModeEdit:
		return m.viewForm()
	case schedulesModeDeleteConfirm:
		return m.viewDelete()
	}
	return m.viewList()
}

func (m SchedulesModel) viewList() string {
	title := styleTitle.Render("Schedules")
	help := styleHelp.Render("[a]dd  [e]dit  [D]elete  [↑/↓] navigate")
	return lipgloss.JoinVertical(lipgloss.Left, title, m.table.View(), help)
}

func (m SchedulesModel) viewForm() string {
	title := "Add Schedule"
	if m.mode == schedulesModeEdit {
		title = "Edit Schedule"
	}
	var sb strings.Builder
	sb.WriteString(styleTitle.Render(title) + "\n\n")
	for i, f := range m.form.fields {
		label := m.form.labels[i]
		if i == m.form.cursor {
			label = styleAccent.Render("> " + label)
		} else {
			label = "  " + label
		}
		sb.WriteString(fmt.Sprintf("  %-25s %s\n", label+":", f.View()))
	}
	if len(m.form.fields) > 2 {
		cronVal := strings.TrimSpace(m.form.fields[2].Value())
		if cronVal != "" {
			sb.WriteString("\n  " + styleMuted.Render("Cron: "+cronVal+" — "+describeCron(cronVal)))
		}
	}
	if m.errMsg != "" {
		sb.WriteString("\n" + styleError.Render("  Error: "+m.errMsg))
	}
	sb.WriteString("\n" + styleHelp.Render("  [tab] next field  [enter] save  [esc] cancel"))
	return sb.String()
}

func (m SchedulesModel) viewDelete() string {
	idx := m.table.Cursor()
	label := ""
	if idx < len(m.app.cfg.Schedules) {
		s := m.app.cfg.Schedules[idx]
		label = fmt.Sprintf("%s → %s", s.Target, s.Destination)
	}
	return styleTitle.Render("Delete Schedule") + "\n\n" +
		fmt.Sprintf("  Delete schedule %q? ", styleError.Render(label)) +
		styleWarning.Render("[y]") + "es / " +
		styleMuted.Render("[n]") + "o"
}

func newScheduleForm(s *config.ScheduleConfig, editIdx int) scheduleForm {
	labels := []string{"target", "destination", "cron expression", "compress (gzip/none)", "tmp_dir", "keep_last"}
	values := make([]string, len(labels))
	if s != nil {
		values[0] = s.Target
		values[1] = s.Destination
		values[2] = s.Cron
		values[3] = s.Compress
		values[4] = s.TmpDir
		values[5] = fmt.Sprintf("%d", s.Retention.KeepLast)
	} else {
		values[3] = "gzip"
		values[4] = "/tmp"
		values[5] = "7"
	}
	fields := make([]textinput.Model, len(labels))
	for i := range fields {
		ti := textinput.New()
		ti.SetValue(values[i])
		ti.CharLimit = 100
		if i == 0 {
			ti.Focus()
		}
		fields[i] = ti
	}
	return scheduleForm{fields: fields, labels: labels, cursor: 0, editIdx: editIdx}
}

func describeCron(expr string) string {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return "custom schedule"
	}
	min, hour, dom, month, dow := parts[0], parts[1], parts[2], parts[3], parts[4]
	if dom == "*" && month == "*" && dow == "*" {
		if min == "0" {
			return fmt.Sprintf("every day at %s:00", hour)
		}
		return fmt.Sprintf("every day at %s:%s", hour, min)
	}
	if dow != "*" {
		days := map[string]string{"0": "Sun", "1": "Mon", "2": "Tue", "3": "Wed", "4": "Thu", "5": "Fri", "6": "Sat"}
		if d, ok := days[dow]; ok {
			return fmt.Sprintf("every %s at %s:%s", d, hour, min)
		}
	}
	return expr
}
