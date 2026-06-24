package tui

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/johnarleyburns/parso-ia-music-indexer/internal/db"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type statsRefreshMsg struct {
	Stats      *db.CombinedStats
	CollStats  *db.CollectionStats
	Err        error
}

type workerState struct {
	ID             string
	CurrentTask    string
	ProcessedCount int
	FailedCount    int
}

type DashboardModel struct {
	DB     *sql.DB
	Width  int
	Height int
	Stats  *db.CombinedStats

	CollectionStats       *db.CollectionStats
	CoordRunning          bool
	CoordCurrentCollection string
	CoordCollectionIdx    int
	CoordCollectionTotal  int
	CoordAlbumsDiscovered int

	ResolverCount  int
	ResolverStates map[string]*workerState

	AnalyzerCount  int
	AnalyzerStates map[string]*workerState

	CleanerCount  int
	CleanerStates map[string]*workerState

	EnhancerCount  int
	EnhancerStates map[string]*workerState

	Events []ActivityEvent
}

func NewDashboardModel(sqlDB *sql.DB) DashboardModel {
	return DashboardModel{
		DB:              sqlDB,
		Stats:           &db.CombinedStats{},
		CollectionStats: &db.CollectionStats{},
		CoordRunning:    false,
		ResolverCount:   0,
		ResolverStates:  make(map[string]*workerState),
		AnalyzerCount:   0,
		AnalyzerStates:  make(map[string]*workerState),
		CleanerCount:    0,
		CleanerStates:   make(map[string]*workerState),
		EnhancerCount:   0,
		EnhancerStates:  make(map[string]*workerState),
		Events:          make([]ActivityEvent, 0),
	}
}

func (m DashboardModel) Init() tea.Cmd {
	return m.refreshStats()
}

func isResolver(id string) bool {
	return strings.HasPrefix(id, "resolver-")
}

func isCleaner(id string) bool {
	return strings.HasPrefix(id, "cleaner-")
}

func isEnhancer(id string) bool {
	return strings.HasPrefix(id, "enhancer-")
}

func (m DashboardModel) Update(msg tea.Msg) (DashboardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case statsRefreshMsg:
		if msg.Err == nil {
			m.Stats = msg.Stats
			if msg.CollStats != nil {
				m.CollectionStats = msg.CollStats
			}
		}
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			s, err := db.GetCombinedStats(m.DB)
			cs, _ := db.GetCollectionStats(m.DB)
			return statsRefreshMsg{Stats: s, CollStats: cs, Err: err}
		})

	case ActivityEvent:
		m.Events = append(m.Events, msg)
		if len(m.Events) > 100 {
			m.Events = m.Events[len(m.Events)-100:]
		}
		switch msg.Type {
		case EventCoordStarted:
			m.CoordRunning = true
		case EventCoordStopped:
			m.CoordRunning = false
			m.CoordCurrentCollection = ""
		case EventCoordProgress:
			m.CoordAlbumsDiscovered = msg.Count
			m.CoordCollectionTotal = msg.Total
		case EventCollectionStarted:
			m.CoordCurrentCollection = msg.CollectionID
		case EventCollectionProgress:
			m.CoordCurrentCollection = msg.CollectionID
			m.CoordAlbumsDiscovered = msg.Count
		case EventCollectionCompleted:
			m.CoordCurrentCollection = ""
		case EventCollectionFailed:
			m.CoordCurrentCollection = ""
	case EventWorkerStarted:
		if msg.WorkerID != "" {
			if isResolver(msg.WorkerID) {
				m.ResolverCount++
				m.ResolverStates[msg.WorkerID] = &workerState{ID: msg.WorkerID}
			} else if isCleaner(msg.WorkerID) {
				m.CleanerCount++
				m.CleanerStates[msg.WorkerID] = &workerState{ID: msg.WorkerID}
			} else if isEnhancer(msg.WorkerID) {
				m.EnhancerCount++
				m.EnhancerStates[msg.WorkerID] = &workerState{ID: msg.WorkerID}
			} else {
				m.AnalyzerCount++
				m.AnalyzerStates[msg.WorkerID] = &workerState{ID: msg.WorkerID}
			}
		}
	case EventWorkerStopped:
		if msg.WorkerID != "" {
			if isResolver(msg.WorkerID) {
				if m.ResolverCount > 0 {
					m.ResolverCount--
				}
				delete(m.ResolverStates, msg.WorkerID)
			} else if isCleaner(msg.WorkerID) {
				if m.CleanerCount > 0 {
					m.CleanerCount--
				}
				delete(m.CleanerStates, msg.WorkerID)
			} else if isEnhancer(msg.WorkerID) {
				if m.EnhancerCount > 0 {
					m.EnhancerCount--
				}
				delete(m.EnhancerStates, msg.WorkerID)
			} else {
				if m.AnalyzerCount > 0 {
					m.AnalyzerCount--
				}
				delete(m.AnalyzerStates, msg.WorkerID)
			}
		} else {
			if strings.Contains(msg.Message, "Resolver") {
				if m.ResolverCount > 0 {
					m.ResolverCount--
				}
			} else if strings.Contains(msg.Message, "Cleaner") {
				if m.CleanerCount > 0 {
					m.CleanerCount--
				}
			} else if strings.Contains(msg.Message, "Enhancer") {
				if m.EnhancerCount > 0 {
					m.EnhancerCount--
				}
			} else {
				if m.AnalyzerCount > 0 {
					m.AnalyzerCount--
				}
			}
		}
		case EventAlbumResolving:
			if msg.WorkerID != "" {
				if ws, ok := m.ResolverStates[msg.WorkerID]; ok {
					ws.CurrentTask = msg.Identifier
				}
			}
		case EventAlbumResolved:
			if msg.WorkerID != "" {
				if ws, ok := m.ResolverStates[msg.WorkerID]; ok {
					ws.ProcessedCount++
					ws.CurrentTask = ""
				}
			}
		case EventAlbumFailed:
			if msg.WorkerID != "" {
				if ws, ok := m.ResolverStates[msg.WorkerID]; ok {
					ws.FailedCount++
					ws.CurrentTask = ""
				}
				if ws, ok := m.CleanerStates[msg.WorkerID]; ok {
					ws.FailedCount++
					ws.CurrentTask = ""
				}
			}
		case EventAlbumUnavailable:
			if msg.WorkerID != "" {
				if ws, ok := m.ResolverStates[msg.WorkerID]; ok {
					ws.FailedCount++
					ws.CurrentTask = ""
				}
				if ws, ok := m.CleanerStates[msg.WorkerID]; ok {
					ws.FailedCount++
					ws.CurrentTask = ""
				}
			}
		case EventAlbumPrechecked:
			if msg.WorkerID != "" {
				if ws, ok := m.CleanerStates[msg.WorkerID]; ok {
					ws.ProcessedCount++
					ws.CurrentTask = ""
				}
			}
		case EventAnalysisStarted:
			if msg.WorkerID != "" {
				if ws, ok := m.AnalyzerStates[msg.WorkerID]; ok {
					ws.CurrentTask = msg.Identifier
				}
				if ws, ok := m.EnhancerStates[msg.WorkerID]; ok {
					ws.CurrentTask = msg.Identifier
				}
			}
		case EventAnalysisComplete:
			if msg.WorkerID != "" {
				if ws, ok := m.AnalyzerStates[msg.WorkerID]; ok {
					ws.ProcessedCount++
					ws.CurrentTask = ""
				}
				if ws, ok := m.EnhancerStates[msg.WorkerID]; ok {
					ws.ProcessedCount++
					ws.CurrentTask = ""
				}
			}
		case EventAnalysisFailed:
			if msg.WorkerID != "" {
				if ws, ok := m.AnalyzerStates[msg.WorkerID]; ok {
					ws.FailedCount++
					ws.CurrentTask = ""
				}
				if ws, ok := m.EnhancerStates[msg.WorkerID]; ok {
					ws.FailedCount++
					ws.CurrentTask = ""
				}
			}
		case EventAnalysisUnavailable:
			if msg.WorkerID != "" {
				if ws, ok := m.AnalyzerStates[msg.WorkerID]; ok {
					ws.FailedCount++
					ws.CurrentTask = ""
				}
			}
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil
	}

	return m, nil
}

func (m DashboardModel) View() tea.View {
	stats := m.Stats
	if stats == nil {
		stats = &db.CombinedStats{}
	}
	collStats := m.CollectionStats
	if collStats == nil {
		collStats = &db.CollectionStats{}
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7c3aed")).MarginBottom(1)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9ca3af"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#e5e7eb"))
	pendingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#06b6d4")).Bold(true)
	completeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))
	failedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444"))
	unavailableStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280"))
	processingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#eab308"))
	resolvingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#a78bfa"))

	drawRow := func(label, value string, style lipgloss.Style) string {
		return fmt.Sprintf("%s %s", labelStyle.Render(label), style.Render(value))
	}

	panelBorder := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#6b7280")).
		Padding(1, 2)

	leftWidth := 50
	rightWidth := m.Width - leftWidth - 4
	if rightWidth < 25 {
		rightWidth = 25
	}

	collPanel := panelBorder.Width(leftWidth)
	collContent := lipgloss.NewStyle().Bold(true).Foreground(Secondary).Render("Collections") + "\n\n"
	collContent += drawRow("Total:       ", fmt.Sprintf("%d", collStats.Total), valueStyle) + "\n"
	collContent += drawRow("Pending:     ", fmt.Sprintf("%d", collStats.Pending), pendingStyle) + "\n"
	collContent += drawRow("Discovering: ", fmt.Sprintf("%d", collStats.Discovering), processingStyle) + "\n"
	collContent += drawRow("Discovered:  ", fmt.Sprintf("%d", collStats.Discovered), completeStyle) + "\n"
	collContent += drawRow("Failed:      ", fmt.Sprintf("%d", collStats.Failed), failedStyle)

	statsPanel := panelBorder.Width(leftWidth / 2)

	albumContent := lipgloss.NewStyle().Bold(true).Foreground(Secondary).Render("Albums") + "\n\n"
	albumContent += drawRow("Total:     ", fmt.Sprintf("%d", stats.Albums.Total), valueStyle) + "\n"
	albumContent += drawRow("Pending:   ", fmt.Sprintf("%d", stats.Albums.Pending), pendingStyle) + "\n"
	albumContent += drawRow("Resolving: ", fmt.Sprintf("%d", stats.Albums.Resolving), resolvingStyle) + "\n"
	albumContent += drawRow("Resolved:  ", fmt.Sprintf("%d", stats.Albums.Resolved), completeStyle) + "\n"
	albumContent += drawRow("Failed:    ", fmt.Sprintf("%d", stats.Albums.Failed), failedStyle) + "\n"
	albumContent += drawRow("Unavail:   ", fmt.Sprintf("%d", stats.Albums.Unavailable), unavailableStyle)

	trackContent := lipgloss.NewStyle().Bold(true).Foreground(Secondary).Render("Tracks") + "\n\n"
	trackContent += drawRow("Total:      ", fmt.Sprintf("%d", stats.Tracks.Total), valueStyle) + "\n"
	trackContent += drawRow("Pending:    ", fmt.Sprintf("%d", stats.Tracks.Pending), pendingStyle) + "\n"
	trackContent += drawRow("Processing: ", fmt.Sprintf("%d", stats.Tracks.Processing), processingStyle) + "\n"
	trackContent += drawRow("Completed:  ", fmt.Sprintf("%d", stats.Tracks.Completed), completeStyle) + "\n"
	trackContent += drawRow("Failed:     ", fmt.Sprintf("%d", stats.Tracks.Failed), failedStyle) + "\n"
	trackContent += drawRow("Unavail:    ", fmt.Sprintf("%d", stats.Tracks.Unavailable), unavailableStyle) + "\n"
	trackContent += drawRow("No Tags:    ", fmt.Sprintf("%d", stats.Tracks.UntaggedCount), pendingStyle)

	statsRow := lipgloss.JoinHorizontal(lipgloss.Top,
		statsPanel.Render(albumContent),
		statsPanel.Render(trackContent),
	)

	leftContent := lipgloss.JoinVertical(lipgloss.Left,
		collPanel.Render(collContent),
		statsRow,
	)

	rightContent := m.buildRightPanel(titleStyle, panelBorder, rightWidth)

	body := lipgloss.JoinHorizontal(lipgloss.Top, leftContent, rightContent)

	content := titleStyle.Render("Dashboard") + "\n\n" + body

	return tea.NewView(content)
}

func (m DashboardModel) refreshStats() tea.Cmd {
	return func() tea.Msg {
		s, err := db.GetCombinedStats(m.DB)
		cs, _ := db.GetCollectionStats(m.DB)
		return statsRefreshMsg{Stats: s, CollStats: cs, Err: err}
	}
}

func (m DashboardModel) buildRightPanel(titleStyle, panelBorder lipgloss.Style, width int) string {
	sectionTitle := func(name string) string {
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#06b6d4")).Render(name)
	}

	coordSection := m.buildCoordinatorSection(sectionTitle)
	resolverSection := m.buildPoolSection(sectionTitle, "Resolver Pool", m.ResolverCount, m.ResolverStates, "[r] add  [R] remove")
	analyzerSection := m.buildPoolSection(sectionTitle, "Analyzer Pool", m.AnalyzerCount, m.AnalyzerStates, "[w] add  [W] remove")
	cleanerSection := m.buildPoolSection(sectionTitle, "Cleaner Pool", m.CleanerCount, m.CleanerStates, "[c] add  [C] remove")
	enhancerControls := fmt.Sprintf("[e] add  [E] remove  ·  Remaining: %d", m.Stats.Tracks.UntaggedCount)
	if m.Stats == nil {
		enhancerControls = "[e] add  [E] remove  ·  Remaining: -"
	}
	enhancerSection := m.buildPoolSection(sectionTitle, "Enhancer Pool", m.EnhancerCount, m.EnhancerStates, enhancerControls)

	const panelOverhead = 4 // 2 border + 2 padding

	controlsContentHeight := lipgloss.Height(coordSection) + lipgloss.Height(resolverSection) + lipgloss.Height(analyzerSection) + lipgloss.Height(cleanerSection) + lipgloss.Height(enhancerSection) + 3

	availBodyHeight := m.Height - 9 // tab(1) + status(4) + help(1) + title+gaps(3)

	feedContentHeight := availBodyHeight - controlsContentHeight - panelOverhead - panelOverhead
	if feedContentHeight < 3 {
		feedContentHeight = 3
	}

	feedContent := sectionTitle("Activity Feed") + "\n"
	feedContent += RenderActivityFeed(m.Events, width-4, feedContentHeight-1)

	controlsPanel := panelBorder.Width(width).Render(
		coordSection + "\n" + resolverSection + "\n" + analyzerSection + "\n" + cleanerSection + "\n" + enhancerSection,
	)
	feedPanel := panelBorder.Width(width).Render(feedContent)

	return lipgloss.JoinVertical(lipgloss.Left, controlsPanel, feedPanel)
}

func (m DashboardModel) buildCoordinatorSection(sectionTitle func(string) string) string {
	runningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e")).Bold(true)
	stoppedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9ca3af"))

	s := sectionTitle("Coordinator") + "\n"

	if m.CoordRunning {
		s += fmt.Sprintf("  Status: %s\n", runningStyle.Render("running"))
	} else {
		s += fmt.Sprintf("  Status: %s\n", stoppedStyle.Render("stopped"))
	}

	collDisplay := ""
	if m.CoordCurrentCollection != "" {
		collDisplay = m.CoordCurrentCollection
		if len(collDisplay) > 30 {
			collDisplay = collDisplay[:30] + "..."
		}
	}
	s += mutedStyle.Render(fmt.Sprintf("  Collection: %s\n", collDisplay))

	s += mutedStyle.Render("  [s] start  [x] stop  [F] reset failed")

	return s
}

func (m DashboardModel) buildPoolSection(sectionTitle func(string) string, name string, count int, states map[string]*workerState, controls string) string {
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9ca3af"))
	processingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#eab308"))

	s := sectionTitle(name) + "\n"
	s += fmt.Sprintf("  Active: %d\n", count)

	ids := make([]string, 0, len(states))
	for id := range states {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		w := states[id]
		line := mutedStyle.Render(fmt.Sprintf("    %s: %d ok / %d fail",
			w.ID, w.ProcessedCount, w.FailedCount))
		if w.CurrentTask != "" {
			short := w.CurrentTask
			if len(short) > 20 {
				short = short[:20] + "..."
			}
			line += processingStyle.Render(fmt.Sprintf(" → %s", short))
		}
		s += line + "\n"
	}

	s += mutedStyle.Render("  " + controls)

	return s
}
