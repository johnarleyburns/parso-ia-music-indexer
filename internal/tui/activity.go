package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	eventQueueAddedStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#06b6d4"))
	eventCoordStartedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))
	eventCoordStoppedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280"))
	eventCoordProgressStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#a78bfa"))
	eventWorkerStartedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#eab308"))
	eventWorkerStoppedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280"))
	eventAnalysisStartedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#eab308"))
	eventAnalysisCompleteStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))
	eventAnalysisFailedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444"))
	eventAlbumResolvingStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#a78bfa"))
	eventAlbumResolvedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))
	eventAlbumFailedStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444"))
	eventInfoStyle             = lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280"))
	eventTimeStyle             = lipgloss.NewStyle().Foreground(lipgloss.Color("#4b5563"))
)

func eventStyle(t EventType) lipgloss.Style {
	switch t {
	case EventQueueAdded:
		return eventQueueAddedStyle
	case EventCoordStarted:
		return eventCoordStartedStyle
	case EventCoordStopped:
		return eventCoordStoppedStyle
	case EventCoordProgress:
		return eventCoordProgressStyle
	case EventWorkerStarted:
		return eventWorkerStartedStyle
	case EventWorkerStopped:
		return eventWorkerStoppedStyle
	case EventAnalysisStarted:
		return eventAnalysisStartedStyle
	case EventAnalysisComplete:
		return eventAnalysisCompleteStyle
	case EventAnalysisFailed:
		return eventAnalysisFailedStyle
	case EventAlbumResolving:
		return eventAlbumResolvingStyle
	case EventAlbumResolved:
		return eventAlbumResolvedStyle
	case EventAlbumFailed:
		return eventAlbumFailedStyle
	default:
		return eventInfoStyle
	}
}

func eventPrefix(t EventType) string {
	switch t {
	case EventQueueAdded:
		return "+"
	case EventCoordStarted:
		return "\u25b6"
	case EventCoordStopped:
		return "\u25aa"
	case EventCoordProgress:
		return "\u2192"
	case EventWorkerStarted:
		return "\u2699"
	case EventWorkerStopped:
		return "\u2715"
	case EventAnalysisStarted:
		return "\u25b7"
	case EventAnalysisComplete:
		return "\u2714"
	case EventAnalysisFailed:
		return "\u2718"
	case EventAlbumResolving:
		return "\u21bb"
	case EventAlbumResolved:
		return "\u2611"
	case EventAlbumFailed:
		return "\u2612"
	default:
		return "\u2022"
	}
}

func RenderActivityFeed(events []ActivityEvent, width, maxItems int) string {
	if len(events) == 0 {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6b7280")).
			Italic(true).
			Render("No events yet. Press s to start coordinator.")
	}

	start := 0
	if maxItems > 0 && len(events) > maxItems {
		start = len(events) - maxItems
	}

	var lines []string
	for _, e := range events[start:] {
		prefix := eventPrefix(e.Type)
		style := eventStyle(e.Type)
		ts := e.Timestamp.Format("15:04:05")

		line := fmt.Sprintf("%s %s  %s",
			eventTimeStyle.Render(ts),
			style.Render(prefix),
			e.Message,
		)

		if width > 0 {
			line = lipgloss.NewStyle().Width(width).Render(line)
			if lipgloss.Width(line) > width {
				line = line[:width-1] + "\u2026"
			}
		}

		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}
