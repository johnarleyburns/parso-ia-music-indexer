package tui

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/johnarleyburns/parso-ia-music-indexer/internal/db"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type statsRefreshMsg struct {
	Stats *db.CombinedStats
	Err   error
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

	CoordRunning      bool
	CoordIndexedCount int
	CoordCursor       string

	ResolverCount  int
	ResolverStates map[string]*workerState

	AnalyzerCount  int
	AnalyzerStates map[string]*workerState

	Events []ActivityEvent
}

func NewDashboardModel(sqlDB *sql.DB) DashboardModel {
	return DashboardModel{
		DB:             sqlDB,
		Stats:          &db.CombinedStats{},
		CoordRunning:   false,
		ResolverCount:  0,
		ResolverStates: make(map[string]*workerState),
		AnalyzerCount:  0,
		AnalyzerStates: make(map[string]*workerState),
		Events:         make([]ActivityEvent, 0),
	}
}

func (m DashboardModel) Init() tea.Cmd {
	return m.refreshStats()
}

func isResolver(id string) bool {
	return strings.HasPrefix(id, "resolver-")
}

func (m DashboardModel) Update(msg tea.Msg) (DashboardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case statsRefreshMsg:
		if msg.Err == nil {
			m.Stats = msg.Stats
		}
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			s, err := db.GetCombinedStats(m.DB)
			return statsRefreshMsg{Stats: s, Err: err}
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
		case EventCoordProgress:
			m.CoordIndexedCount = msg.Count
			if msg.Cursor != "" {
				m.CoordCursor = msg.Cursor
			}
		case EventWorkerStarted:
			if msg.WorkerID != "" {
				if isResolver(msg.WorkerID) {
					m.ResolverCount++
					m.ResolverStates[msg.WorkerID] = &workerState{ID: msg.WorkerID}
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
			}
		case EventAnalysisStarted:
			if msg.WorkerID != "" {
				if ws, ok := m.AnalyzerStates[msg.WorkerID]; ok {
					ws.CurrentTask = msg.Identifier
				}
			}
		case EventAnalysisComplete:
			if msg.WorkerID != "" {
				if ws, ok := m.AnalyzerStates[msg.WorkerID]; ok {
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

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7c3aed")).MarginBottom(1)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9ca3af"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#e5e7eb"))
	pendingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#06b6d4")).Bold(true)
	completeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))
	failedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444"))
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

	albumPanel := panelBorder.Width(leftWidth / 2)
	albumContent := lipgloss.NewStyle().Bold(true).Foreground(Secondary).Render("Albums") + "\n\n"
	albumContent += drawRow("Total:     ", fmt.Sprintf("%d", stats.Albums.Total), valueStyle) + "\n"
	albumContent += drawRow("Pending:   ", fmt.Sprintf("%d", stats.Albums.Pending), pendingStyle) + "\n"
	albumContent += drawRow("Resolving: ", fmt.Sprintf("%d", stats.Albums.Resolving), resolvingStyle) + "\n"
	albumContent += drawRow("Resolved:  ", fmt.Sprintf("%d", stats.Albums.Resolved), completeStyle) + "\n"
	albumContent += drawRow("Failed:    ", fmt.Sprintf("%d", stats.Albums.Failed), failedStyle)

	trackPanel := panelBorder.Width(leftWidth / 2)
	trackContent := lipgloss.NewStyle().Bold(true).Foreground(Secondary).Render("Tracks") + "\n\n"
	trackContent += drawRow("Total:      ", fmt.Sprintf("%d", stats.Tracks.Total), valueStyle) + "\n"
	trackContent += drawRow("Pending:    ", fmt.Sprintf("%d", stats.Tracks.Pending), pendingStyle) + "\n"
	trackContent += drawRow("Processing: ", fmt.Sprintf("%d", stats.Tracks.Processing), processingStyle) + "\n"
	trackContent += drawRow("Completed:  ", fmt.Sprintf("%d", stats.Tracks.Completed), completeStyle) + "\n"
	trackContent += drawRow("Failed:     ", fmt.Sprintf("%d", stats.Tracks.Failed), failedStyle)

	statsRow := lipgloss.JoinHorizontal(lipgloss.Top,
		albumPanel.Render(albumContent),
		trackPanel.Render(trackContent),
	)

	rightContent := m.buildRightPanel(titleStyle, panelBorder, rightWidth)

	body := lipgloss.JoinHorizontal(lipgloss.Top, statsRow, rightContent)

	content := titleStyle.Render("Dashboard") + "\n\n" + body

	return tea.NewView(content)
}

func (m DashboardModel) refreshStats() tea.Cmd {
	return func() tea.Msg {
		s, err := db.GetCombinedStats(m.DB)
		return statsRefreshMsg{Stats: s, Err: err}
	}
}

func (m DashboardModel) buildRightPanel(titleStyle, panelBorder lipgloss.Style, width int) string {
	sectionTitle := func(name string) string {
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#06b6d4")).Render(name)
	}

	coordSection := m.buildCoordinatorSection(sectionTitle)
	resolverSection := m.buildPoolSection(sectionTitle, "Resolver Pool", m.ResolverCount, m.ResolverStates, "[r] add  [R] remove")
	analyzerSection := m.buildPoolSection(sectionTitle, "Analyzer Pool", m.AnalyzerCount, m.AnalyzerStates, "[w] add  [W] remove")

	controlsHeight := lipgloss.Height(coordSection) + lipgloss.Height(resolverSection) + lipgloss.Height(analyzerSection) + 4
	availHeight := m.Height - 7
	feedHeight := availHeight - controlsHeight
	if feedHeight < 4 {
		feedHeight = 4
	}

	feedContent := sectionTitle("Activity Feed") + "\n"
	feedContent += RenderActivityFeed(m.Events, width-4, feedHeight-1)

	controlsPanel := panelBorder.Width(width).Render(
		coordSection + "\n" + resolverSection + "\n" + analyzerSection,
	)
	feedPanel := panelBorder.Width(width).Render(feedContent)

	return lipgloss.JoinVertical(lipgloss.Left, controlsPanel, feedPanel)
}

func (m DashboardModel) buildCoordinatorSection(sectionTitle func(string) string) string {
	runningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e")).Bold(true)
	stoppedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9ca3af"))

	s := sectionTitle("Coordinator") + "\n"

	var status string
	if m.CoordRunning {
		status = runningStyle.Render("\u25b6 Running")
	} else {
		status = stoppedStyle.Render("\u25aa Stopped")
	}

	cursorDisplay := "—"
	if m.CoordCursor != "" {
		if len(m.CoordCursor) > 24 {
			cursorDisplay = m.CoordCursor[:24] + "..."
		} else {
			cursorDisplay = m.CoordCursor
		}
	}

	s += fmt.Sprintf("  Status: %s\n", status)
	s += mutedStyle.Render(fmt.Sprintf("  Indexed: %d  |  Cursor: %s\n", m.CoordIndexedCount, cursorDisplay))
	s += mutedStyle.Render("  [s] start  [x] stop  [F] reset failed")

	return s
}

func (m DashboardModel) buildPoolSection(sectionTitle func(string) string, name string, count int, states map[string]*workerState, controls string) string {
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9ca3af"))
	processingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#eab308"))

	s := sectionTitle(name) + "\n"
	s += fmt.Sprintf("  Active: %d\n", count)

	for _, w := range states {
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
