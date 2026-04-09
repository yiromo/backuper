package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var (
	colorPrimary = lipgloss.AdaptiveColor{Light: "#6D28D9", Dark: "#7C3AED"} // purple
	colorSuccess = lipgloss.AdaptiveColor{Light: "#059669", Dark: "#10B981"} // green
	colorError   = lipgloss.AdaptiveColor{Light: "#DC2626", Dark: "#EF4444"} // red
	colorWarning = lipgloss.AdaptiveColor{Light: "#D97706", Dark: "#F59E0B"} // amber
	colorMuted   = lipgloss.AdaptiveColor{Light: "#9CA3AF", Dark: "#6B7280"} // gray
	colorAccent  = lipgloss.AdaptiveColor{Light: "#0891B2", Dark: "#06B6D4"} // cyan
	colorFg      = lipgloss.AdaptiveColor{Light: "#111827", Dark: "#E5E7EB"} // text
	colorBg      = lipgloss.AdaptiveColor{Light: "#F9FAFB", Dark: "#111827"} // bg
	colorBgLight = lipgloss.AdaptiveColor{Light: "#E5E7EB", Dark: "#1F2937"} // elevated bg
	colorBorder  = lipgloss.AdaptiveColor{Light: "#D1D5DB", Dark: "#374151"} // borders

	styleApp   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorBorder).Padding(0, 1)
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
	keyStyle := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	if active {
		return lipgloss.NewStyle().
			Background(colorPrimary).
			Foreground(colorBg).
			Padding(0, 1).
			Render(keyStyle.Foreground(colorBg).Render(key) + " " + lipgloss.NewStyle().Bold(true).Render(label))
	}

	return lipgloss.NewStyle().
		Foreground(colorMuted).
		Padding(0, 1).
		Render(keyStyle.Render(key) + " " + label)
}

func statusSuccess(s string) string { return styleSuccess.Render(s) }
func statusError(s string) string   { return styleError.Render(s) }

func renderLogo() string {
	return lipgloss.NewStyle().
		Foreground(colorPrimary).
		Bold(true).
		Render("bkp") +
		lipgloss.NewStyle().Foreground(colorMuted).Render(" backuper") +
		"\n"
}

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
