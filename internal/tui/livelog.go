package tui

import (
	tea "charm.land/bubbletea/v2"
)

type LiveLogModel struct {
	Width  int
	Height int
}

func NewLiveLogModel() LiveLogModel {
	return LiveLogModel{}
}

func (m LiveLogModel) Init() tea.Cmd {
	return nil
}

func (m LiveLogModel) Update(msg tea.Msg) (LiveLogModel, tea.Cmd) {
	return m, nil
}

func (m LiveLogModel) View() tea.View {
	return centerView("Live Log", "A real-time scrollable feed of all indexing\nevents will appear here in Phase 3.", m.Width, m.Height)
}
