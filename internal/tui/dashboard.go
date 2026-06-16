package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type DashboardModel struct {
	Width  int
	Height int
}

func NewDashboardModel() DashboardModel {
	return DashboardModel{}
}

func (m DashboardModel) Init() tea.Cmd {
	return nil
}

func (m DashboardModel) Update(msg tea.Msg) (DashboardModel, tea.Cmd) {
	return m, nil
}

func (m DashboardModel) View() tea.View {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7c3aed")).MarginBottom(1)
	bodyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280")).Italic(true)

	s := titleStyle.Render("Dashboard") + "\n\n"
	s += bodyStyle.Render("Database stats, coordinator/worker controls,") + "\n"
	s += bodyStyle.Render("and live activity feed will appear here in Phase 2–3.")

	return tea.NewView(lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, s))
}
