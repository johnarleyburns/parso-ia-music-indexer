package components

import (
	"charm.land/lipgloss/v2"
)

type TabBar struct {
	Tabs      []string
	ActiveTab int
	Width     int
	styles    TabBarStyles
}

type TabBarStyles struct {
	Active   lipgloss.Style
	Inactive lipgloss.Style
	Bar      lipgloss.Style
}

func DefaultTabBarStyles() TabBarStyles {
	return TabBarStyles{
		Active: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7c3aed")).
			Border(lipgloss.Border{Bottom: "━"}, false, false, true, false).
			BorderForeground(lipgloss.Color("#7c3aed")).
			Padding(0, 2),
		Inactive: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6b7280")).
			Padding(0, 2),
		Bar: lipgloss.NewStyle().
			PaddingBottom(0),
	}
}

func NewTabBar(tabs []string, active int, width int) TabBar {
	return TabBar{
		Tabs:      tabs,
		ActiveTab: active,
		Width:     width,
		styles:    DefaultTabBarStyles(),
	}
}

func (t *TabBar) SetActive(idx int) {
	if idx >= 0 && idx < len(t.Tabs) {
		t.ActiveTab = idx
	}
}

func (t *TabBar) View() string {
	var rendered []string
	for i, tab := range t.Tabs {
		if i == t.ActiveTab {
			rendered = append(rendered, t.styles.Active.Render(tab))
		} else {
			rendered = append(rendered, t.styles.Inactive.Render(tab))
		}
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
	return t.styles.Bar.MaxWidth(t.Width).Render(row)
}
