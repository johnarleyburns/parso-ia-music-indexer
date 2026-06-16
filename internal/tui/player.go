package tui

import (
	tea "charm.land/bubbletea/v2"
)

type PlayerModel struct {
	Width  int
	Height int
}

func NewPlayerModel() PlayerModel {
	return PlayerModel{}
}

func (m PlayerModel) Init() tea.Cmd {
	return nil
}

func (m PlayerModel) Update(msg tea.Msg) (PlayerModel, tea.Cmd) {
	return m, nil
}

func (m PlayerModel) View() tea.View {
	return centerView("Player", "Stream and play IA MP3 tracks with\nplayback controls and a play queue.\nThis will be implemented in Phase 7.", m.Width, m.Height)
}
