package tui

import "charm.land/lipgloss/v2"

var (
	// Color palette
	Primary   = lipgloss.Color("#7c3aed") // Purple
	Secondary = lipgloss.Color("#06b6d4") // Cyan
	Success   = lipgloss.Color("#22c55e") // Green
	Warning   = lipgloss.Color("#eab308") // Yellow
	Danger    = lipgloss.Color("#ef4444") // Red
	Muted     = lipgloss.Color("#6b7280") // Gray
	TextColor = lipgloss.Color("#e5e7eb") // Light gray
	BgColor   = lipgloss.Color("#1f2937") // Dark blue-gray

	// Tab styles
	ActiveTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Primary).
			Border(lipgloss.Border{Bottom: "━"}, false, false, true, false).
			BorderForeground(Primary).
			Padding(0, 2)

	InactiveTabStyle = lipgloss.NewStyle().
				Foreground(Muted).
				Padding(0, 2)

	// Panel styles
	PanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Muted).
			Padding(1, 2)

	PanelTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Secondary)

	// Help footer style
	HelpStyle = lipgloss.NewStyle().
			Foreground(Muted).
			Border(lipgloss.Border{Top: "─"}, false, true, false, false).
			BorderForeground(Muted).
			Padding(0, 1)

	// Misc
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Primary).
			MarginBottom(1)

	PlaceholderStyle = lipgloss.NewStyle().
				Foreground(Muted).
				Italic(true)

	StatusRunningStyle = lipgloss.NewStyle().
				Foreground(Success).
				Bold(true)

	StatusStoppedStyle = lipgloss.NewStyle().
				Foreground(Muted)
)
