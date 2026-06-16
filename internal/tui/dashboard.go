package tui

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/johnarleyburns/parso-ia-music-indexer/internal/db"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type statsRefreshMsg struct {
	Stats *db.QueueStats
	Err   error
}

type DashboardModel struct {
	DB     *sql.DB
	Width  int
	Height int
	Stats  *db.QueueStats
}

func NewDashboardModel(sqlDB *sql.DB) DashboardModel {
	return DashboardModel{
		DB:    sqlDB,
		Stats: &db.QueueStats{},
	}
}

func (m DashboardModel) Init() tea.Cmd {
	return m.refreshStats()
}

func (m DashboardModel) Update(msg tea.Msg) (DashboardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case statsRefreshMsg:
		if msg.Err == nil {
			m.Stats = msg.Stats
		}
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			s, err := db.GetStats(m.DB)
			return statsRefreshMsg{Stats: s, Err: err}
		})

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
		stats = &db.QueueStats{}
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7c3aed")).MarginBottom(1)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9ca3af"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#e5e7eb"))
	pendingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#06b6d4")).Bold(true)
	completeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))
	failedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444"))
	processingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#eab308"))

	drawRow := func(label, value string, style lipgloss.Style) string {
		return fmt.Sprintf("%s %s", labelStyle.Render(label), style.Render(value))
	}

	statsPanel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#6b7280")).
		Padding(1, 2).
		Width(35)

	statsContent := titleStyle.Render("Database") + "\n\n"
	statsContent += drawRow("Total:      ", fmt.Sprintf("%d", stats.Total), valueStyle) + "\n"
	statsContent += drawRow("Pending:    ", fmt.Sprintf("%d", stats.Pending), pendingStyle) + "\n"
	statsContent += drawRow("Processing: ", fmt.Sprintf("%d", stats.Processing), processingStyle) + "\n"
	statsContent += drawRow("Completed:  ", fmt.Sprintf("%d", stats.Completed), completeStyle) + "\n"
	statsContent += drawRow("Failed:     ", fmt.Sprintf("%d", stats.Failed), failedStyle)

	rightPanel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#6b7280")).
		Padding(1, 2)

	rightContent := titleStyle.Render("Controls") + "\n\n"
	rightContent += lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280")).Italic(true).Render("Coordinator and worker controls\nwill appear here in Phase 3.")

	left := statsPanel.Render(statsContent)
	right := rightPanel.Width(m.Width-35-4).Render(rightContent)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	content := titleStyle.Render("Dashboard") + "\n\n" + body

	if m.Height > 3 {
		content = lipgloss.Place(m.Width, m.Height-3, lipgloss.Left, lipgloss.Top, content)
	}

	return tea.NewView(content)
}

func (m DashboardModel) refreshStats() tea.Cmd {
	return func() tea.Msg {
		s, err := db.GetStats(m.DB)
		return statsRefreshMsg{Stats: s, Err: err}
	}
}
