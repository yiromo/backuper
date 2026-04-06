package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var (
	colorPrimary = lipgloss.Color("#7C3AED") // purple
	colorSuccess = lipgloss.Color("#10B981") // green
	colorError   = lipgloss.Color("#EF4444") // red
	colorWarning = lipgloss.Color("#F59E0B") // amber
	colorMuted   = lipgloss.Color("#6B7280") // gray
	colorAccent  = lipgloss.Color("#06B6D4") // cyan
	colorFg      = lipgloss.Color("#E5E7EB") // soft white
	colorBg      = lipgloss.Color("#111827") // near-black
	colorBgLight = lipgloss.Color("#1F2937") // elevated surface
	colorBorder  = lipgloss.Color("#374151") // subtle borders

	styleTitle = lipgloss.NewStyle().
			Foreground(colorFg).
			Bold(true).
			MarginBottom(1)

	styleSelected = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	styleSuccess = lipgloss.NewStyle().Foreground(colorSuccess)
	styleError   = lipgloss.NewStyle().Foreground(colorError)
	styleWarning = lipgloss.NewStyle().Foreground(colorWarning)
	styleMuted   = lipgloss.NewStyle().Foreground(colorMuted)
	styleAccent  = lipgloss.NewStyle().Foreground(colorAccent)

	styleStatusBar = lipgloss.NewStyle().
			Background(colorBgLight).
			Foreground(colorMuted).
			Padding(0, 1)

	styleStatusErr = lipgloss.NewStyle().
			Background(colorError).
			Foreground(colorFg).
			Padding(0, 1)

	styleBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	styleTableHeader = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	styleHelp = lipgloss.NewStyle().
			Foreground(colorMuted).
			MarginTop(1)
)

func navItem(key, label string, active bool) string {
	keyStr := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(key)
	base := lipgloss.NewStyle().Background(colorBgLight).Padding(0, 1)
	if active {
		labelStr := lipgloss.NewStyle().Foreground(colorFg).Bold(true).Render(label)
		return base.Render(keyStr + " " + labelStr)
	}
	labelStr := lipgloss.NewStyle().Foreground(colorMuted).Render(label)
	return base.Render(keyStr + " " + labelStr)
}

func statusSuccess(s string) string { return styleSuccess.Render(s) }
func statusError(s string) string   { return styleError.Render(s) }

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
