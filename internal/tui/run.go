package tui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"backuper/internal/backup"
)

type runStep int

const (
	runStepPickTarget runStep = iota
	runStepPickDest
	runStepRunning
	runStepDone
)

type RunModel struct {
	app    *App
	step   runStep
	width  int
	height int

	spinnerFrame int

	targetIdx int
	destIdx   int

	chosenTarget string
	chosenDest   string

	logLines []string
	logCh    chan string
	doneCh   chan struct{}
	mu       sync.Mutex
	running  bool
	lastRec  *backup.Record
	lastErr  error
}

func newRun(app *App) RunModel {
	return RunModel{app: app, step: runStepPickTarget}
}

func (m RunModel) withTarget(target string) RunModel {
	m.chosenTarget = target
	m.step = runStepPickDest
	m.destIdx = 0
	return m
}

func (m RunModel) capturesInput() bool { return false }

type logLineMsg struct{ line string }
type backupDoneMsg struct {
	rec *backup.Record
	err error
}
type spinnerTickMsg struct{}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func waitForLog(ch chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return nil // channel closed; done msg sent separately
		}
		return logLineMsg{line: line}
	}
}

func (m RunModel) Init() tea.Cmd { return nil }

func (m RunModel) Update(msg tea.Msg) (RunModel, tea.Cmd) {
	switch msg := msg.(type) {
	case logLineMsg:
		m.logLines = append(m.logLines, msg.line)
		// Keep scrolling to bottom by trimming old lines.
		if len(m.logLines) > 500 {
			m.logLines = m.logLines[len(m.logLines)-500:]
		}
		return m, waitForLog(m.logCh)

	case backupDoneMsg:
		m.running = false
		m.lastRec = msg.rec
		m.lastErr = msg.err
		m.step = runStepDone
		m.spinnerFrame = 0
		if msg.err != nil {
			return m, setStatusErr("Backup failed: " + msg.err.Error())
		}
		return m, setStatus("Backup completed successfully.")

	case spinnerTickMsg:
		if m.running {
			m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
			return m, tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
				return spinnerTickMsg{}
			})
		}

	case tea.KeyMsg:
		switch m.step {
		case runStepPickTarget:
			return m.updatePickTarget(msg)
		case runStepPickDest:
			return m.updatePickDest(msg)
		case runStepRunning:
			// Allow abort via ctrl+c (handled globally).
		case runStepDone:
			switch msg.String() {
			case "esc", "enter", "r":
				m.step = runStepPickTarget
				m.logLines = nil
				m.chosenTarget = ""
				m.chosenDest = ""
			}
		}
	}
	return m, nil
}

func (m RunModel) updatePickTarget(msg tea.KeyMsg) (RunModel, tea.Cmd) {
	targets := m.app.cfg.Targets
	if len(targets) == 0 {
		return m, setStatusErr("No targets configured.")
	}
	switch msg.String() {
	case "up", "k":
		if m.targetIdx > 0 {
			m.targetIdx--
		}
	case "down", "j":
		if m.targetIdx < len(targets)-1 {
			m.targetIdx++
		}
	case "enter":
		m.chosenTarget = targets[m.targetIdx].Name
		m.step = runStepPickDest
		m.destIdx = 0
	case "esc":
		return m, switchTo(viewDashboard)
	}
	return m, nil
}

func (m RunModel) updatePickDest(msg tea.KeyMsg) (RunModel, tea.Cmd) {
	dests := m.app.cfg.Destinations
	if len(dests) == 0 {
		return m, setStatusErr("No destinations configured.")
	}
	switch msg.String() {
	case "up", "k":
		if m.destIdx > 0 {
			m.destIdx--
		}
	case "down", "j":
		if m.destIdx < len(dests)-1 {
			m.destIdx++
		}
	case "enter":
		m.chosenDest = dests[m.destIdx].Name
		return m.startBackup()
	case "esc":
		m.step = runStepPickTarget
	}
	return m, nil
}

func (m RunModel) startBackup() (RunModel, tea.Cmd) {
	m.step = runStepRunning
	m.running = true
	m.logLines = nil
	m.logCh = make(chan string, 200)
	m.doneCh = make(chan struct{})

	targetName := m.chosenTarget
	destName := m.chosenDest
	logCh := m.logCh

	return m, tea.Batch(
		func() tea.Msg {
			cw := &chanWriter{ch: logCh}
			var rec *backup.Record
			var err error

			if m.app.sched != nil {
				rec, err = m.app.sched.RunNow(context.Background(), targetName, destName, cw)
			} else {
				err = fmt.Errorf("scheduler not initialised")
			}
			close(logCh)
			return backupDoneMsg{rec: rec, err: err}
		},
		waitForLog(m.logCh),
		tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
			return spinnerTickMsg{}
		}),
	)
}

func (m RunModel) View() string {
	switch m.step {
	case runStepPickTarget:
		return m.viewPickTarget()
	case runStepPickDest:
		return m.viewPickDest()
	case runStepRunning:
		return m.viewLog()
	case runStepDone:
		return m.viewDone()
	}
	return ""
}

func (m RunModel) viewPickTarget() string {
	var sb strings.Builder
	sb.WriteString(styleTitle.Render("Run Backup — Select Target") + "\n\n")
	for i, t := range m.app.cfg.Targets {
		name := fmt.Sprintf("%s (%s/%s)", t.Name, t.Engine, t.Runtime)
		if i == m.targetIdx {
			sb.WriteString(styleAccent.Render("  ▸ ") + lipgloss.NewStyle().Foreground(colorFg).Bold(true).Render(name) + "\n")
		} else {
			sb.WriteString(styleMuted.Render("    "+name) + "\n")
		}
	}
	if len(m.app.cfg.Targets) == 0 {
		sb.WriteString(styleMuted.Render("  No targets configured.") + "\n")
	}
	sb.WriteString("\n" + styleHelp.Render("[↑/↓] navigate  [enter] select  [esc] cancel"))
	return sb.String()
}

func (m RunModel) viewPickDest() string {
	var sb strings.Builder
	sb.WriteString(styleTitle.Render(fmt.Sprintf("Run Backup — Select Destination (target: %s)", styleAccent.Render(m.chosenTarget))) + "\n\n")
	for i, d := range m.app.cfg.Destinations {
		name := fmt.Sprintf("%s (%s)", d.Name, d.Type)
		if i == m.destIdx {
			sb.WriteString(styleAccent.Render("  ▸ ") + lipgloss.NewStyle().Foreground(colorFg).Bold(true).Render(name) + "\n")
		} else {
			sb.WriteString(styleMuted.Render("    "+name) + "\n")
		}
	}
	if len(m.app.cfg.Destinations) == 0 {
		sb.WriteString(styleMuted.Render("  No destinations configured.") + "\n")
	}
	sb.WriteString("\n" + styleHelp.Render("[↑/↓] navigate  [enter] start  [esc] back"))
	return sb.String()
}

func (m RunModel) viewLog() string {
	header := fmt.Sprintf("Running: %s → %s",
		styleAccent.Render(m.chosenTarget),
		styleAccent.Render(m.chosenDest))

	visible := m.logLines
	maxLines := m.height - 6
	if maxLines < 1 {
		maxLines = 10
	}
	if len(visible) > maxLines {
		visible = visible[len(visible)-maxLines:]
	}

	var logSB strings.Builder
	for _, line := range visible {
		if strings.HasPrefix(line, "ERROR") {
			logSB.WriteString(styleError.Render(line) + "\n")
		} else if strings.HasPrefix(line, "  ") {
			logSB.WriteString(styleMuted.Render(line) + "\n")
		} else {
			logSB.WriteString(styleAccent.Render(line) + "\n")
		}
	}

	frame := spinnerFrames[m.spinnerFrame]
	spinner := styleAccent.Render(frame+" ") + styleMuted.Render("running...")
	return lipgloss.JoinVertical(lipgloss.Left,
		styleTitle.Render(header),
		"",
		logSB.String(),
		"",
		spinner,
	)
}

func (m RunModel) viewDone() string {
	statusLine := styleSuccess.Render("✓ Backup completed successfully!")
	if m.lastErr != nil {
		statusLine = styleError.Render("✗ Backup failed: " + m.lastErr.Error())
	}
	visible := m.logLines
	maxLines := m.height - 8
	if maxLines < 1 {
		maxLines = 10
	}
	if len(visible) > maxLines {
		visible = visible[len(visible)-maxLines:]
	}
	var logSB strings.Builder
	for _, line := range visible {
		if strings.HasPrefix(line, "ERROR") {
			logSB.WriteString(styleError.Render(line) + "\n")
		} else {
			logSB.WriteString(line + "\n")
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		statusLine,
		"",
		logSB.String(),
		"",
		styleHelp.Render("[enter/r] run another  [esc] back"),
	)
}

func (m *RunModel) resize(w, h int) RunModel {
	m.width = w
	m.height = h
	return *m
}

// chanWriter implements io.Writer by sending each line to a channel.
type chanWriter struct {
	ch  chan string
	buf strings.Builder
}

func (w *chanWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	for {
		s := w.buf.String()
		idx := strings.IndexByte(s, '\n')
		if idx < 0 {
			break
		}
		line := s[:idx]
		w.ch <- line
		w.buf.Reset()
		w.buf.WriteString(s[idx+1:])
	}
	return len(p), nil
}

var _ io.Writer = (*chanWriter)(nil)
