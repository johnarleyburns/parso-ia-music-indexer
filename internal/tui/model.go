package tui

import (
	"fmt"

	"github.com/johnarleyburns/parso-ia-music-indexer/internal/config"
	"github.com/johnarleyburns/parso-ia-music-indexer/internal/tui/components"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
)

type keyMap struct {
	NextTab key.Binding
	PrevTab key.Binding
	Help    key.Binding
	Quit    key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.NextTab, k.PrevTab, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.NextTab, k.PrevTab},
		{k.Help, k.Quit},
	}
}

var keys = keyMap{
	NextTab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "next tab"),
	),
	PrevTab: key.NewBinding(
		key.WithKeys("shift+tab"),
		key.WithHelp("shift+tab", "prev tab"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "toggle help"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}

type MainModel struct {
	Config   *config.Config
	Tabs     []string
	ActiveTab int

	Dashboard DashboardModel
	LiveLog   LiveLogModel
	Browse    BrowseModel
	Player    PlayerModel

	Help   help.Model
	Keys   keyMap

	Width  int
	Height int
	Ready  bool
}

func NewMainModel(cfg *config.Config) MainModel {
	return MainModel{
		Config:   cfg,
		Tabs:     []string{"Dashboard", "Live Log", "Browse", "Player"},
		ActiveTab: 0,
		Dashboard: NewDashboardModel(),
		LiveLog:   NewLiveLogModel(),
		Browse:    NewBrowseModel(),
		Player:    NewPlayerModel(),
		Help:     help.New(),
		Keys:     keys,
	}
}

func (m MainModel) Init() tea.Cmd {
	return nil
}

func (m MainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.Keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.Keys.NextTab):
			m.ActiveTab = (m.ActiveTab + 1) % len(m.Tabs)
			return m, nil
		case key.Matches(msg, m.Keys.PrevTab):
			m.ActiveTab = (m.ActiveTab - 1 + len(m.Tabs)) % len(m.Tabs)
			return m, nil
		case key.Matches(msg, m.Keys.Help):
			m.Help.ShowAll = !m.Help.ShowAll
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.Help.SetWidth(msg.Width)
		m.Ready = true
		m.Dashboard.Width = msg.Width
		m.Dashboard.Height = msg.Height
		m.LiveLog.Width = msg.Width
		m.LiveLog.Height = msg.Height
		m.Browse.Width = msg.Width
		m.Browse.Height = msg.Height
		m.Player.Width = msg.Width
		m.Player.Height = msg.Height
		return m, nil
	}

	return m, nil
}

func (m MainModel) View() tea.View {
	if !m.Ready {
		return tea.NewView("initializing...")
	}

	tabBar := components.NewTabBar(m.Tabs, m.ActiveTab)

	var content string
	switch m.ActiveTab {
	case 0:
		content = m.Dashboard.View().Content
	case 1:
		content = m.LiveLog.View().Content
	case 2:
		content = m.Browse.View().Content
	case 3:
		content = m.Player.View().Content
	}

	helpView := m.Help.View(m.Keys)
	helpHeight := lipgloss.Height(helpView)

	contentHeight := m.Height - lipgloss.Height(tabBar.View()) - helpHeight - 1
	if contentHeight < 1 {
		contentHeight = 1
	}

	panelStyle := lipgloss.NewStyle().
		Width(m.Width).
		Height(contentHeight)

	view := lipgloss.JoinVertical(
		lipgloss.Left,
		tabBar.View(),
		panelStyle.Render(content),
		helpView,
	)

	v := tea.NewView(view)
	v.WindowTitle = fmt.Sprintf("timbre — %s", m.Tabs[m.ActiveTab])
	v.AltScreen = true
	return v
}
