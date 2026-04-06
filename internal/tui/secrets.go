package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type secretsStep int

const (
	secretsStepMenu secretsStep = iota
	secretsStepSetRef
	secretsStepSetValue
	secretsStepDeleteConfirm
)

type SecretsModel struct {
	app    *App
	step   secretsStep
	refs   []string
	cursor int

	refInput textinput.Model
	valInput textinput.Model
	errMsg   string
}

func newSecrets(app *App) SecretsModel {
	ri := textinput.New()
	ri.Placeholder = "secret_ref name"
	ri.CharLimit = 80

	vi := textinput.New()
	vi.Placeholder = "password / value"
	vi.EchoMode = textinput.EchoPassword
	vi.CharLimit = 200

	m := SecretsModel{app: app, refInput: ri, valInput: vi}
	m.loadRefs()
	return m
}

func (m *SecretsModel) loadRefs() {
	if m.app.store == nil {
		return
	}
	refs, _ := m.app.store.List()
	m.refs = refs
}

func (m SecretsModel) capturesInput() bool {
	return m.step == secretsStepSetRef || m.step == secretsStepSetValue
}

func (m SecretsModel) Update(msg tea.Msg) (SecretsModel, tea.Cmd) {
	switch m.step {
	case secretsStepMenu:
		return m.updateMenu(msg)
	case secretsStepSetRef:
		return m.updateSetRef(msg)
	case secretsStepSetValue:
		return m.updateSetValue(msg)
	case secretsStepDeleteConfirm:
		return m.updateDelete(msg)
	}
	return m, nil
}

func (m SecretsModel) updateMenu(msg tea.Msg) (SecretsModel, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.refs)-1 {
				m.cursor++
			}
		case "a":
			m.refInput.SetValue("")
			m.valInput.SetValue("")
			m.step = secretsStepSetRef
			m.refInput.Focus()
			return m, textinput.Blink
		case "D":
			if len(m.refs) > 0 {
				m.step = secretsStepDeleteConfirm
			}
		case "esc":
			return m, switchTo(viewTargets)
		}
	}
	return m, nil
}

func (m SecretsModel) updateSetRef(msg tea.Msg) (SecretsModel, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			if strings.TrimSpace(m.refInput.Value()) == "" {
				m.errMsg = "ref name cannot be empty"
				return m, nil
			}
			m.errMsg = ""
			m.refInput.Blur()
			m.valInput.SetValue("")
			m.step = secretsStepSetValue
			m.valInput.Focus()
			return m, textinput.Blink
		case "esc":
			m.step = secretsStepMenu
			m.errMsg = ""
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.refInput, cmd = m.refInput.Update(msg)
	return m, cmd
}

func (m SecretsModel) updateSetValue(msg tea.Msg) (SecretsModel, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			ref := strings.TrimSpace(m.refInput.Value())
			val := m.valInput.Value()
			if val == "" {
				m.errMsg = "value cannot be empty"
				return m, nil
			}
			if m.app.store == nil {
				m.errMsg = "secrets store not initialised"
				return m, nil
			}
			if err := m.app.store.Set(ref, val); err != nil {
				m.errMsg = err.Error()
				return m, nil
			}
			m.step = secretsStepMenu
			m.errMsg = ""
			m.valInput.Blur()
			m.loadRefs()
			return m, setStatus(fmt.Sprintf("Secret %q saved.", ref))
		case "esc":
			m.step = secretsStepSetRef
			m.valInput.Blur()
			m.refInput.Focus()
			return m, textinput.Blink
		}
	}
	var cmd tea.Cmd
	m.valInput, cmd = m.valInput.Update(msg)
	return m, cmd
}

func (m SecretsModel) updateDelete(msg tea.Msg) (SecretsModel, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "y", "Y":
			if m.cursor < len(m.refs) {
				ref := m.refs[m.cursor]
				if m.app.store != nil {
					if err := m.app.store.Delete(ref); err != nil {
						m.errMsg = err.Error()
					} else {
						m.loadRefs()
						if m.cursor >= len(m.refs) && m.cursor > 0 {
							m.cursor--
						}
					}
				}
			}
			m.step = secretsStepMenu
			return m, setStatus("Secret deleted.")
		case "n", "N", "esc":
			m.step = secretsStepMenu
		}
	}
	return m, nil
}

func (m SecretsModel) View() string {
	switch m.step {
	case secretsStepSetRef:
		return m.viewSetRef()
	case secretsStepSetValue:
		return m.viewSetValue()
	case secretsStepDeleteConfirm:
		return m.viewDelete()
	}
	return m.viewMenu()
}

func (m SecretsModel) viewMenu() string {
	var sb strings.Builder
	sb.WriteString(styleTitle.Render("Secrets Store") + "\n")
	sb.WriteString(styleMuted.Render("  Values are never displayed. Keys only.") + "\n\n")

	if len(m.refs) == 0 {
		sb.WriteString(styleMuted.Render("  No secrets stored yet.") + "\n")
	}
	mask := styleMuted.Render("●●●●●●●●")
	for i, ref := range m.refs {
		if i == m.cursor {
			sb.WriteString(styleAccent.Render("  ▸ ") + lipgloss.NewStyle().Foreground(colorFg).Bold(true).Render(fmt.Sprintf("%-36s", ref)) + "  " + mask + "\n")
		} else {
			sb.WriteString("    " + styleMuted.Render(fmt.Sprintf("%-36s", ref)) + "  " + mask + "\n")
		}
	}
	if m.errMsg != "" {
		sb.WriteString("\n" + styleError.Render("  "+m.errMsg))
	}
	sb.WriteString("\n" + styleHelp.Render("[a] add/update  [D] delete  [esc] back"))
	return sb.String()
}

func (m SecretsModel) viewSetRef() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		styleTitle.Render("Set Secret — Step 1: Ref Name"),
		"",
		"  "+m.refInput.View(),
		"",
		styleHelp.Render("  [enter] next  [esc] cancel"),
		errorLine(m.errMsg),
	)
}

func (m SecretsModel) viewSetValue() string {
	ref := strings.TrimSpace(m.refInput.Value())
	return lipgloss.JoinVertical(lipgloss.Left,
		styleTitle.Render(fmt.Sprintf("Set Secret — Step 2: Value for %q", ref)),
		styleMuted.Render("  Input is masked and never stored in plaintext."),
		"",
		"  "+m.valInput.View(),
		"",
		styleHelp.Render("  [enter] save  [esc] back"),
		errorLine(m.errMsg),
	)
}

func (m SecretsModel) viewDelete() string {
	ref := ""
	if m.cursor < len(m.refs) {
		ref = m.refs[m.cursor]
	}
	return styleTitle.Render("Delete Secret") + "\n\n" +
		fmt.Sprintf("  Delete secret %q? ", styleError.Render(ref)) +
		styleWarning.Render("[y]") + "es / " +
		styleMuted.Render("[n]") + "o"
}

func errorLine(msg string) string {
	if msg == "" {
		return ""
	}
	return styleError.Render("  Error: " + msg)
}
