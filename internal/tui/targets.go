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

type targetsMode int

const (
	targetsModeList targetsMode = iota
	targetsModeAdd
	targetsModeEdit
	targetsModeDeleteConfirm
)

type TargetsModel struct {
	app    *App
	table  table.Model
	mode   targetsMode
	form   targetForm
	width  int
	height int
	errMsg string
}

type targetForm struct {
	fields  []textinput.Model
	labels  []string
	cursor  int
	editIdx int // -1 for new
}

func newTargets(app *App) TargetsModel {
	t := table.New(
		table.WithColumns(targetsColumns(80)),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	s := table.DefaultStyles()
	s.Header = styleTableHeader
	s.Selected = styleSelected
	t.SetStyles(s)
	m := TargetsModel{app: app, table: t}
	m.refreshTable()
	return m
}

func targetsColumns(width int) []table.Column {
	if width >= 80 {
		nameW := 20
		typeW := 12
		nsW := 18
		userW := 14
		refW := width - nameW - typeW - nsW - userW - 8
		if refW < 10 {
			refW = 10
		}
		return []table.Column{
			{Title: "Name", Width: nameW},
			{Title: "Type", Width: typeW},
			{Title: "Namespace/DB", Width: nsW},
			{Title: "DB User", Width: userW},
			{Title: "Secret Ref", Width: refW},
		}
	} else if width >= 60 {
		return []table.Column{
			{Title: "Name", Width: 20},
			{Title: "Type", Width: 12},
			{Title: "Namespace/DB", Width: 18},
			{Title: "Secret Ref", Width: width - 58},
		}
	} else if width >= 40 {
		return []table.Column{
			{Title: "Name", Width: 20},
			{Title: "Namespace/DB", Width: 18},
			{Title: "Secret Ref", Width: width - 46},
		}
	}
	return []table.Column{
		{Title: "Name", Width: 20},
		{Title: "Secret Ref", Width: width - 28},
	}
}

func (m *TargetsModel) refreshTable() {
	if m.width == 0 {
		m.width = 80
	}
	m.table.SetRows(m.buildRows(m.width))
}

func (m TargetsModel) buildRows(width int) []table.Row {
	var rows []table.Row
	for _, t := range m.app.cfg.Targets {
		ns := t.Namespace
		if ns == "" {
			ns = t.DBName
		}
		if width >= 80 {
			rows = append(rows, table.Row{t.Name, t.Type, ns, t.DBUser, t.SecretRef})
		} else if width >= 60 {
			rows = append(rows, table.Row{t.Name, t.Type, ns, t.SecretRef})
		} else if width >= 40 {
			rows = append(rows, table.Row{t.Name, ns, t.SecretRef})
		} else {
			rows = append(rows, table.Row{t.Name, t.SecretRef})
		}
	}
	return rows
}

func (m TargetsModel) capturesInput() bool { return m.mode != targetsModeList }

func (m TargetsModel) Update(msg tea.Msg) (TargetsModel, tea.Cmd) {
	switch m.mode {
	case targetsModeList:
		return m.updateList(msg)
	case targetsModeAdd, targetsModeEdit:
		return m.updateForm(msg)
	case targetsModeDeleteConfirm:
		return m.updateDelete(msg)
	}
	return m, nil
}

func (m TargetsModel) updateList(msg tea.Msg) (TargetsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "a":
			m.mode = targetsModeAdd
			m.form = newTargetForm(nil, -1)
			return m, textinput.Blink
		case "e":
			idx := m.table.Cursor()
			if idx < len(m.app.cfg.Targets) {
				tgt := m.app.cfg.Targets[idx]
				m.mode = targetsModeEdit
				m.form = newTargetForm(&tgt, idx)
				return m, textinput.Blink
			}
		case "D":
			if len(m.app.cfg.Targets) > 0 {
				m.mode = targetsModeDeleteConfirm
			}
		case "r", "enter":
			if row := m.table.SelectedRow(); row != nil {
				return m, switchToRun(row[0])
			}
		case "S":
			return m, switchTo(viewSecrets)
		}
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m TargetsModel) updateForm(msg tea.Msg) (TargetsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.mode = targetsModeList
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
			m.mode = targetsModeList
			m.errMsg = ""
			m.refreshTable()
			return m, setStatus("Target saved.")
		}
	}
	var cmd tea.Cmd
	m.form.fields[m.form.cursor], cmd = m.form.fields[m.form.cursor].Update(msg)
	return m, cmd
}

func (m TargetsModel) updateDelete(msg tea.Msg) (TargetsModel, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "y", "Y":
			idx := m.table.Cursor()
			if idx < len(m.app.cfg.Targets) {
				m.app.cfg.Targets = append(m.app.cfg.Targets[:idx], m.app.cfg.Targets[idx+1:]...)
				m.refreshTable()
			}
			m.mode = targetsModeList
			return m, setStatus("Target deleted.")
		case "n", "N", "esc":
			m.mode = targetsModeList
		}
	}
	return m, nil
}

func (m TargetsModel) saveForm() error {
	vals := make([]string, len(m.form.fields))
	for i, f := range m.form.fields {
		vals[i] = strings.TrimSpace(f.Value())
	}
	if vals[0] == "" {
		return fmt.Errorf("name is required")
	}
	if vals[1] != "kubernetes" && vals[1] != "local" {
		return fmt.Errorf("type must be 'kubernetes' or 'local'")
	}
	if vals[3] == "" {
		return fmt.Errorf("db_user is required")
	}
	tgt := config.TargetConfig{
		Name:        vals[0],
		Type:        vals[1],
		Namespace:   vals[2],
		PodSelector: vals[3],
		DBUser:      vals[4],
		DBName:      vals[5],
		SecretRef:   vals[6],
	}
	if m.form.editIdx >= 0 && m.form.editIdx < len(m.app.cfg.Targets) {
		m.app.cfg.Targets[m.form.editIdx] = tgt
	} else {
		m.app.cfg.Targets = append(m.app.cfg.Targets, tgt)
	}
	return nil
}

func (m TargetsModel) View() string {
	switch m.mode {
	case targetsModeAdd, targetsModeEdit:
		return m.viewForm()
	case targetsModeDeleteConfirm:
		return m.viewDelete()
	}
	return m.viewList()
}

func (m TargetsModel) viewList() string {
	title := styleTitle.Render("Targets")
	help := styleHelp.Render("[a]dd  [e]dit  [D]elete  [enter/r] run now  [S]ecrets")
	return lipgloss.JoinVertical(lipgloss.Left, title, m.table.View(), help)
}

func (m TargetsModel) viewForm() string {
	title := "Add Target"
	if m.mode == targetsModeEdit {
		title = "Edit Target"
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
		sb.WriteString(fmt.Sprintf("  %-20s %s\n", label+":", f.View()))
	}
	if m.errMsg != "" {
		sb.WriteString("\n" + styleError.Render("  Error: "+m.errMsg))
	}
	sb.WriteString("\n" + styleHelp.Render("  [tab] next field  [enter] save  [esc] cancel"))
	return sb.String()
}

func (m TargetsModel) viewDelete() string {
	idx := m.table.Cursor()
	name := ""
	if idx < len(m.app.cfg.Targets) {
		name = m.app.cfg.Targets[idx].Name
	}
	return styleTitle.Render("Delete Target") + "\n\n" +
		fmt.Sprintf("  Delete target %q? ", styleError.Render(name)) +
		styleWarning.Render("[y]") + "es / " +
		styleMuted.Render("[n]") + "o"
}

func (m *TargetsModel) resize(w, h int) TargetsModel {
	m.width = w
	m.height = h
	m.table.SetRows(nil)
	m.table.SetColumns(targetsColumns(w))
	m.table.SetHeight(h - 4)
	m.table.SetRows(m.buildRows(w))
	return *m
}

func newTargetForm(tgt *config.TargetConfig, editIdx int) targetForm {
	labels := []string{"name", "type (kubernetes/local)", "namespace", "pod_selector", "db_user", "db_name", "secret_ref"}
	values := make([]string, len(labels))
	if tgt != nil {
		values[0] = tgt.Name
		values[1] = tgt.Type
		values[2] = tgt.Namespace
		values[3] = tgt.PodSelector
		values[4] = tgt.DBUser
		values[5] = tgt.DBName
		values[6] = tgt.SecretRef
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
	return targetForm{fields: fields, labels: labels, cursor: 0, editIdx: editIdx}
}
