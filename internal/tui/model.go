package tui

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

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

type resourceTickMsg struct {
	Stats ResourceStats
}

type MainModel struct {
	Config    *config.Config
	Tabs      []string
	ActiveTab int

	DB *sql.DB

	Events   chan ActivityEvent
	Controls chan ControlCmd

	ArtCache *ArtCache
	Metrics  *Metrics
	DBPath   string
	ArtDir   string
	Resources ResourceStats

	Dashboard DashboardModel
	LiveLog   LiveLogModel
	Browse    BrowseModel
	Player    PlayerModel

	Help help.Model
	Keys keyMap

	Width  int
	Height int
	Ready  bool
}

func NewMainModel(cfg *config.Config, sqlDB *sql.DB, events chan ActivityEvent, controls chan ControlCmd, artCache *ArtCache, metrics *Metrics, dbPath, artDir string) MainModel {
	return MainModel{
		Config:    cfg,
		Tabs:      []string{"Dashboard", "Live Log", "Browse", "Player"},
		ActiveTab: 0,
		DB:        sqlDB,
		Events:    events,
		Controls:  controls,
		ArtCache:  artCache,
		Metrics:   metrics,
		DBPath:    dbPath,
		ArtDir:    artDir,
		Dashboard: NewDashboardModel(sqlDB),
		LiveLog:   NewLiveLogModel(),
		Browse:    NewBrowseModel(sqlDB, artCache),
		Player:    NewPlayerModel(sqlDB, artCache),
		Help:      help.New(),
		Keys:      keys,
	}
}

func (m MainModel) Init() tea.Cmd {
	return tea.Batch(
		m.Dashboard.Init(),
		waitForActivityEvent(m.Events),
		m.resourceTick(),
	)
}

func waitForActivityEvent(ch <-chan ActivityEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return nil
		}
		return event
	}
}

func (m MainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		browseInputActive := m.ActiveTab == 2 && m.Browse.InputFocused()

		if key.Matches(msg, m.Keys.Quit) && msg.String() == "ctrl+c" {
			m.Player.engine.Close()
			m.Controls <- ControlCmd{Action: CmdShutdown}
			return m, tea.Quit
		}

		if !browseInputActive {
			switch {
			case key.Matches(msg, m.Keys.Quit):
				m.Player.engine.Close()
				m.Controls <- ControlCmd{Action: CmdShutdown}
				return m, tea.Quit
			case key.Matches(msg, m.Keys.Help):
				m.Help.ShowAll = !m.Help.ShowAll
				return m, nil
			}
		}

		switch {
		case key.Matches(msg, m.Keys.NextTab):
			prevTab := m.ActiveTab
			m.ActiveTab = (m.ActiveTab + 1) % len(m.Tabs)
			return m, m.onTabSwitch(prevTab, m.ActiveTab)
		case key.Matches(msg, m.Keys.PrevTab):
			prevTab := m.ActiveTab
			m.ActiveTab = (m.ActiveTab - 1 + len(m.Tabs)) % len(m.Tabs)
			return m, m.onTabSwitch(prevTab, m.ActiveTab)
		}

		if m.ActiveTab == 0 {
			switch msg.String() {
			case "s":
				m.Controls <- ControlCmd{Action: CmdStartCoordinator}
				return m, nil
			case "x":
				m.Controls <- ControlCmd{Action: CmdStopCoordinator}
				return m, nil
			case "r":
				m.Controls <- ControlCmd{Action: CmdAddResolver}
				return m, nil
			case "R":
				m.Controls <- ControlCmd{Action: CmdRemoveResolver}
				return m, nil
			case "w":
				m.Controls <- ControlCmd{Action: CmdAddWorker}
				return m, nil
			case "W":
				m.Controls <- ControlCmd{Action: CmdRemoveWorker}
				return m, nil
			case "F":
				m.Controls <- ControlCmd{Action: CmdResetFailed}
				return m, nil
			}
		}

	case SwitchToPlayerMsg:
		m.ActiveTab = 3
		var cmd tea.Cmd
		m.Player, cmd = m.Player.Update(msg)
		return m, cmd

	case SwitchToAlbumMsg:
		m.ActiveTab = 2
		var cmd tea.Cmd
		m.Browse, cmd = m.Browse.Update(msg)
		return m, cmd

	case ActivityEvent:
		var cmd1, cmd2 tea.Cmd
		m.Dashboard, cmd1 = m.Dashboard.Update(msg)
		m.LiveLog, cmd2 = m.LiveLog.Update(msg)
		return m, tea.Batch(waitForActivityEvent(m.Events), cmd1, cmd2)

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.Help.SetWidth(msg.Width)
		m.Ready = true

		var cmd tea.Cmd
		m.Dashboard, cmd = m.Dashboard.Update(msg)
		m.LiveLog, _ = m.LiveLog.Update(msg)
		m.Browse, _ = m.Browse.Update(msg)
		m.Player, _ = m.Player.Update(msg)
		return m, cmd

	case statsRefreshMsg:
		var cmd tea.Cmd
		m.Dashboard, cmd = m.Dashboard.Update(msg)
		return m, cmd

	case resourceTickMsg:
		m.Resources = msg.Stats
		return m, m.resourceTick()

	case browseSearchMsg, browseSimilarMsg, browseAlbumSearchMsg, browseAlbumDetailMsg:
		var cmd tea.Cmd
		m.Browse, cmd = m.Browse.Update(msg)
		return m, cmd

	case artLoadedMsg:
		var cmd1, cmd2 tea.Cmd
		m.Browse, cmd1 = m.Browse.Update(msg)
		m.Player, cmd2 = m.Player.Update(msg)
		return m, tea.Batch(cmd1, cmd2)

	case playerLoadedMsg, playerErrorMsg, playerTickMsg, playerDoneMsg, playerStatsMsg:
		var cmd tea.Cmd
		m.Player, cmd = m.Player.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	switch m.ActiveTab {
	case 0:
		m.Dashboard, cmd = m.Dashboard.Update(msg)
	case 1:
		m.LiveLog, cmd = m.LiveLog.Update(msg)
	case 2:
		m.Browse, cmd = m.Browse.Update(msg)
	case 3:
		m.Player, cmd = m.Player.Update(msg)
	}

	return m, cmd
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

	statusBar := RenderStatusBar(m.Metrics, m.Dashboard.Stats, m.Resources, m.Width)
	statusHeight := lipgloss.Height(statusBar)

	contentHeight := m.Height - lipgloss.Height(tabBar.View()) - helpHeight - statusHeight
	if contentHeight < 1 {
		contentHeight = 1
	}

	contentLines := strings.Split(content, "\n")
	if len(contentLines) > contentHeight {
		contentLines = contentLines[:contentHeight]
	}
	content = strings.Join(contentLines, "\n")

	panelStyle := lipgloss.NewStyle().
		Width(m.Width).
		Height(contentHeight)

	view := lipgloss.JoinVertical(
		lipgloss.Left,
		tabBar.View(),
		panelStyle.Render(content),
		statusBar,
		helpView,
	)

	v := tea.NewView(view)
	v.WindowTitle = fmt.Sprintf("timbre \u2014 %s", m.Tabs[m.ActiveTab])
	v.AltScreen = true
	return v
}

func (m MainModel) resourceTick() tea.Cmd {
	dbPath := m.DBPath
	artDir := m.ArtDir
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return resourceTickMsg{Stats: ComputeResourceStats(dbPath, artDir)}
	})
}

func (m *MainModel) onTabSwitch(from, to int) tea.Cmd {
	if to == 2 {
		var cmd tea.Cmd
		m.Browse, cmd = m.Browse.Activate()
		return cmd
	}
	if from == 2 {
		m.Browse.searchInput.Blur()
		m.Browse.table.Blur()
		m.Browse.inputFocused = false
	}
	return nil
}
