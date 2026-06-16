package tui

import (
	tea "charm.land/bubbletea/v2"
)

type BrowseModel struct {
	Width  int
	Height int
}

func NewBrowseModel() BrowseModel {
	return BrowseModel{}
}

func (m BrowseModel) Init() tea.Cmd {
	return nil
}

func (m BrowseModel) Update(msg tea.Msg) (BrowseModel, tea.Cmd) {
	return m, nil
}

func (m BrowseModel) View() tea.View {
	return centerView("Browse", "Search for tracks, view indexed results,\nand trigger vector similarity queries.\nThis will be implemented in Phase 6.", m.Width, m.Height)
}
