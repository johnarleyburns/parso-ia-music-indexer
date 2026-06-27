package tui

import (
	"database/sql"
	"fmt"
	"log"
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

type collectionsTickMsg struct{}

type MainModel struct {
	Config    *config.Config
	Tabs      []string
	ActiveTab int

	DB *sql.DB

	Events   chan ActivityEvent
	Controls chan ControlCmd

	Metrics  *Metrics
	DBPath   string
	Resources ResourceStats

	Dashboard   DashboardModel
	LiveLog     LiveLogModel
	Browse      BrowseModel
	Player      PlayerModel
	Collections CollectionsModel

	Help help.Model
	Keys keyMap

	Width  int
	Height int
	Ready  bool
}

func NewMainModel(cfg *config.Config, sqlDB *sql.DB, events chan ActivityEvent, controls chan ControlCmd, metrics *Metrics, dbPath string) MainModel {
	return MainModel{
		Config:    cfg,
		Tabs:      []string{"Dashboard", "Live Log", "Browse", "Player", "Collections"},
		ActiveTab: 0,
		DB:        sqlDB,
		Events:    events,
		Controls:  controls,
		Metrics:   metrics,
		DBPath:    dbPath,
		Dashboard: NewDashboardModel(sqlDB),
		LiveLog:   NewLiveLogModel(),
		Browse:    NewBrowseModel(sqlDB),
		Player:      NewPlayerModel(sqlDB),
		Collections: NewCollectionsModel(sqlDB),
		Help:        help.New(),
		Keys:      keys,
	}
}

func (m MainModel) Init() tea.Cmd {
	return tea.Batch(
		m.Dashboard.Init(),
		drainActivityEvents(m.Events),
		m.resourceTick(),
		m.collectionsTick(),
	)
}

// eventBatchMax caps how many events one drain coalesces into a single render.
const eventBatchMax = 512

// eventBatchMsg delivers a coalesced batch of activity events so the model
// updates and renders once per burst instead of once per event. This decouples
// the render rate from the event rate and prevents the render storm.
type eventBatchMsg struct {
	Events []ActivityEvent
}

// drainActivityEvents blocks for the first event, then non-blocking-drains all
// currently-queued events into a single eventBatchMsg.
func drainActivityEvents(ch <-chan ActivityEvent) tea.Cmd {
	return func() tea.Msg {
		first, ok := <-ch
		if !ok {
			return nil
		}
		batch := []ActivityEvent{first}
		for len(batch) < eventBatchMax {
			select {
			case ev, ok := <-ch:
				if !ok {
					return eventBatchMsg{Events: batch}
				}
				batch = append(batch, ev)
			default:
				return eventBatchMsg{Events: batch}
			}
		}
		return eventBatchMsg{Events: batch}
	}
}

func (m MainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Y <= 1 && msg.Button == tea.MouseLeft {
			x := msg.X
			offset := 0
			for i, tab := range m.Tabs {
				tabWidth := len(tab) + 4
				if x >= offset && x < offset+tabWidth {
					if i != m.ActiveTab {
						prevTab := m.ActiveTab
						m.ActiveTab = i
						return m, m.onTabSwitch(prevTab, m.ActiveTab)
					}
					break
				}
				offset += tabWidth
			}
		}
		return m, nil

	case tea.MouseWheelMsg:
		if m.ActiveTab == 3 {
			var cmd tea.Cmd
			m.Player, cmd = m.Player.Update(msg)
			return m, cmd
		}
		if m.ActiveTab == 4 {
			var cmd tea.Cmd
			m.Collections, cmd = m.Collections.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.KeyPressMsg:
		browseInputActive := m.ActiveTab == 2 && m.Browse.InputFocused()

		if (m.ActiveTab == 0 || m.ActiveTab == 1) && !browseInputActive {
			switch msg.String() {
			case "left":
				prevTab := m.ActiveTab
				m.ActiveTab = (m.ActiveTab - 1 + len(m.Tabs)) % len(m.Tabs)
				return m, m.onTabSwitch(prevTab, m.ActiveTab)
			case "right":
				prevTab := m.ActiveTab
				m.ActiveTab = (m.ActiveTab + 1) % len(m.Tabs)
				return m, m.onTabSwitch(prevTab, m.ActiveTab)
			}
		}

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

	collectionsInputActive := m.ActiveTab == 4 && m.Collections.InputFocused()

		switch {
		case key.Matches(msg, m.Keys.NextTab):
			if collectionsInputActive {
				break
			}
			prevTab := m.ActiveTab
			m.ActiveTab = (m.ActiveTab + 1) % len(m.Tabs)
			return m, m.onTabSwitch(prevTab, m.ActiveTab)
		case key.Matches(msg, m.Keys.PrevTab):
			if collectionsInputActive {
				break
			}
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
			case "e":
				m.Controls <- ControlCmd{Action: CmdAddEnhancer}
				return m, nil
			case "E":
				m.Controls <- ControlCmd{Action: CmdRemoveEnhancer}
				return m, nil
			case "l":
				m.Controls <- ControlCmd{Action: CmdAddCleaner}
				return m, nil
			case "L":
				m.Controls <- ControlCmd{Action: CmdRemoveCleaner}
				return m, nil
			case "F":
				m.Controls <- ControlCmd{Action: CmdResetFailed}
				return m, nil
			case "!":
				m.Controls <- ControlCmd{Action: CmdRestartWorker}
				return m, nil
			}
		}

		if m.ActiveTab == 2 {
			var cmd tea.Cmd
			m.Browse, cmd = m.Browse.Update(msg)
			return m, cmd
		}
		if m.ActiveTab == 3 {
			var cmd tea.Cmd
			m.Player, cmd = m.Player.Update(msg)
			return m, cmd
		}
		if m.ActiveTab == 4 {
			var cmd tea.Cmd
			m.Collections, cmd = m.Collections.Update(msg)
			return m, cmd
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

	case eventBatchMsg:
		for _, ev := range msg.Events {
			m.Dashboard, _ = m.Dashboard.Update(ev)
			m.LiveLog, _ = m.LiveLog.Update(ev)
		}
		return m, drainActivityEvents(m.Events)

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
		m.Collections, _ = m.Collections.Update(msg)
		return m, cmd

	case statsRefreshMsg:
		var cmd tea.Cmd
		m.Dashboard, cmd = m.Dashboard.Update(msg)
		return m, cmd

	case resourceTickMsg:
		m.Resources = msg.Stats
		return m, m.resourceTick()

	case collectionsTickMsg:
		if m.ActiveTab == 4 && m.Collections.ShouldAutoRefresh() {
			return m, tea.Batch(m.Collections.doRefresh(), m.collectionsTick())
		}
		return m, m.collectionsTick()

	case browseSearchMsg, browseSimilarMsg, browseAlbumSearchMsg, browseAlbumDetailMsg:
		var cmd tea.Cmd
		m.Browse, cmd = m.Browse.Update(msg)
		return m, cmd

	case playerLoadedMsg, playerErrorMsg, playerTickMsg, playerDoneMsg, playerStatsMsg:
		var cmd tea.Cmd
		m.Player, cmd = m.Player.Update(msg)
		return m, cmd

	case collectionsRefreshMsg:
		var cmd tea.Cmd
		m.Collections, cmd = m.Collections.Update(msg)
		return m, cmd

	case playlistProgressMsg:
		log.Printf("[collections] MainModel.Update: received playlistProgressMsg, routing to Collections.Update")
		var cmd tea.Cmd
		m.Collections, cmd = m.Collections.Update(msg)
		return m, cmd

	case SwitchToBrowseMsg:
		m.ActiveTab = 2
		var cmd tea.Cmd
		m.Browse, cmd = m.Browse.SearchForCollection(msg.Query)
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
	case 4:
		m.Collections, cmd = m.Collections.Update(msg)
	}

	return m, cmd
}

func (m MainModel) View() tea.View {
	if !m.Ready {
		return tea.NewView("initializing...")
	}

	tabBar := components.NewTabBar(m.Tabs, m.ActiveTab, m.Width)

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
	case 4:
		content = m.Collections.View().Content
	}

	helpView := m.Help.View(m.Keys)
	helpHeight := lipgloss.Height(helpView)

	statusBar := RenderStatusBar(m.Metrics, m.Dashboard.Stats, m.Resources, m.Width)
	statusHeight := lipgloss.Height(statusBar)

	contentHeight := m.Height - lipgloss.Height(tabBar.View()) - helpHeight - statusHeight
	if contentHeight < 1 {
		contentHeight = 1
	}

	if m.ActiveTab != 2 {
		contentLines := strings.Split(content, "\n")
		if len(contentLines) > contentHeight {
			contentLines = contentLines[:contentHeight]
		}
		content = strings.Join(contentLines, "\n")
	}

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
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m MainModel) resourceTick() tea.Cmd {
	dbPath := m.DBPath
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return resourceTickMsg{Stats: ComputeResourceStats(dbPath)}
	})
}

func (m MainModel) collectionsTick() tea.Cmd {
	return tea.Tick(15*time.Second, func(t time.Time) tea.Msg {
		return collectionsTickMsg{}
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
	if from == 4 {
		m.Collections.table.Blur()
		m.Collections.mode = colModeView
	}
	if to == 4 {
		var cmd tea.Cmd
		m.Collections, cmd = m.Collections.Update(tea.WindowSizeMsg{Width: m.Width, Height: m.Height})
		log.Printf("[collections] onTabSwitch: switching to tab 4, firing refresh cmd")
		return tea.Batch(cmd, m.Collections.doRefresh())
	}
	return nil
}
