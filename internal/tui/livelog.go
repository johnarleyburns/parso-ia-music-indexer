package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type LiveLogModel struct {
	Width      int
	Height     int
	Events     []ActivityEvent
	ScrollOff  int
	AutoScroll bool
}

func NewLiveLogModel() LiveLogModel {
	return LiveLogModel{
		Events:     make([]ActivityEvent, 0),
		AutoScroll: true,
	}
}

func (m LiveLogModel) Init() tea.Cmd {
	return nil
}

func (m LiveLogModel) Update(msg tea.Msg) (LiveLogModel, tea.Cmd) {
	switch msg := msg.(type) {
	case ActivityEvent:
		m.Events = append(m.Events, msg)
		if len(m.Events) > 1000 {
			m.Events = m.Events[len(m.Events)-1000:]
		}
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "S":
			m.AutoScroll = !m.AutoScroll
			return m, nil
		case "up":
			if m.ScrollOff > 0 {
				m.ScrollOff--
			}
			return m, nil
		case "down":
			m.ScrollOff++
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil
	}

	return m, nil
}

func (m LiveLogModel) View() tea.View {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7c3aed")).MarginBottom(1)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280"))

	header := titleStyle.Render("Live Log")

	scrollStatus := "on"
	if !m.AutoScroll {
		scrollStatus = "off"
	}
	header += "  " + mutedStyle.Render(fmt.Sprintf("[auto-scroll: %s] [↑↓ scroll] [S toggle]", scrollStatus))

	if len(m.Events) == 0 {
		return centerView("Live Log", "A real-time scrollable feed of all indexing\nevents will appear here in Phase 3.\n\nPress s on Dashboard tab to start.", m.Width, m.Height)
	}

	viewHeight := m.Height - 5
	if viewHeight < 5 {
		viewHeight = 5
	}

	feedWidth := m.Width - 4
	if feedWidth < 40 {
		feedWidth = 40
	}

	feed := RenderActivityFeed(m.Events, feedWidth, 0)
	lines := strings.Split(feed, "\n")

	totalLines := len(lines)
	visLines := viewHeight
	if visLines > totalLines {
		visLines = totalLines
	}

	if m.AutoScroll {
		m.ScrollOff = totalLines - visLines
		if m.ScrollOff < 0 {
			m.ScrollOff = 0
		}
	}

	if m.ScrollOff > totalLines-visLines {
		m.ScrollOff = totalLines - visLines
	}
	if m.ScrollOff < 0 {
		m.ScrollOff = 0
	}

	visible := lines[m.ScrollOff : m.ScrollOff+visLines]

	var panelContent string
	panelContent += header + "\n\n"
	panelContent += strings.Join(visible, "\n")

	if totalLines > visLines {
		scrollPct := float64(m.ScrollOff) / float64(totalLines-visLines) * 100
		panelContent += "\n" + mutedStyle.Render(fmt.Sprintf("── %d/%d (%.0f%%) ──", m.ScrollOff+visLines, totalLines, scrollPct))
	}

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#6b7280")).
		Padding(1, 2).
		Width(m.Width - 2)

	content := panel.Render(panelContent)

	if m.Height > 3 {
		content = lipgloss.Place(m.Width, m.Height-3, lipgloss.Left, lipgloss.Top, content)
	}

	return tea.NewView(content)
}
