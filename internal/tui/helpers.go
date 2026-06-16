package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func centerView(title, body string, w, h int) tea.View {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7c3aed")).MarginBottom(1)
	bodyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280")).Italic(true)

	s := titleStyle.Render(title) + "\n\n"
	s += bodyStyle.Render(body)

	return tea.NewView(lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, s))
}
