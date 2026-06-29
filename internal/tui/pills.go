package tui

import (
	"database/sql"
	"fmt"

	"github.com/johnarleyburns/parso-ia-music-indexer/internal/db"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
)

type pillsMode int

const (
	pillModeList pillsMode = iota
	pillModeTracks
)

type pillsRefreshMsg struct {
	Pills []db.PillWithCount
	Err   error
}

type pillsTracksMsg struct {
	PillID string
	Label  string
	Tracks []db.PillTrack
	Err    error
}

type PillsModel struct {
	DB     *sql.DB
	Width  int
	Height int

	mode         pillsMode
	loaded       bool
	refreshError string

	pillsTable table.Model
	pills      []db.PillWithCount

	tracksTable  table.Model
	tracks       []db.PillTrack
	currentLabel string
	tracksError  string
}

func NewPillsModel(sqlDB *sql.DB) PillsModel {
	pt := newTable(pillsColumns())
	pt.SetHeight(10)
	tt := newTable(pillTrackColumns())
	tt.SetHeight(10)
	return PillsModel{
		DB:          sqlDB,
		mode:        pillModeList,
		pillsTable:  pt,
		tracksTable: tt,
	}
}

func (m PillsModel) Init() tea.Cmd {
	return m.doRefresh()
}

// InputFocused reports whether a text input is capturing keys. The Pills tab has
// no text input, so tab navigation is never trapped.
func (m PillsModel) InputFocused() bool { return false }

func (m PillsModel) Update(msg tea.Msg) (PillsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		tableHeight := msg.Height - 12
		if tableHeight < 5 {
			tableHeight = 5
		}
		m.pillsTable.SetHeight(tableHeight)
		m.tracksTable.SetHeight(tableHeight)
		if w := msg.Width - 4; w > 0 {
			m.pillsTable.SetWidth(w)
			m.tracksTable.SetWidth(w)
		}
		return m, nil

	case pillsRefreshMsg:
		if msg.Err != nil {
			m.refreshError = msg.Err.Error()
			m.loaded = true
			return m, nil
		}
		m.refreshError = ""
		m.pills = msg.Pills
		rows := make([]table.Row, len(msg.Pills))
		for i, p := range msg.Pills {
			active := "·"
			if p.Active {
				active = "\u2714"
			}
			rows[i] = table.Row{
				p.Label,
				fmt.Sprintf("%d", p.LibraryCount),
				active,
				p.Keywords,
			}
		}
		m.pillsTable.SetRows(rows)
		m.loaded = true
		m.pillsTable.Focus()
		return m, nil

	case pillsTracksMsg:
		m.currentLabel = msg.Label
		if msg.Err != nil {
			m.tracksError = msg.Err.Error()
		} else {
			m.tracksError = ""
			m.tracks = msg.Tracks
			rows := make([]table.Row, len(msg.Tracks))
			for i, t := range msg.Tracks {
				rows[i] = table.Row{
					t.Title,
					t.AlbumCreator,
					t.AlbumTitle,
					fmt.Sprintf("%.2f", t.ListenabilityScore),
					fmt.Sprintf("%.2f", t.QualityScore),
				}
			}
			m.tracksTable.SetRows(rows)
			m.tracksTable.Focus()
		}
		m.mode = pillModeTracks
		return m, nil

	case tea.KeyPressMsg:
		if m.mode == pillModeTracks {
			switch msg.String() {
			case "esc":
				m.mode = pillModeList
				m.pillsTable.Focus()
				return m, nil
			case "enter":
				idx := m.tracksTable.Cursor()
				if idx >= 0 && idx < len(m.tracks) {
					t := m.tracks[idx]
					return m, func() tea.Msg {
						return SwitchToPlayerMsg{
							TrackID:     t.TrackID,
							Title:       t.Title,
							AlbumID:     t.AlbumID,
							AlbumTitle:  t.AlbumTitle,
							DownloadURL: t.DownloadURL,
						}
					}
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.tracksTable, cmd = m.tracksTable.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "r":
			return m, m.doRefresh()
		case "enter":
			idx := m.pillsTable.Cursor()
			if idx >= 0 && idx < len(m.pills) {
				p := m.pills[idx]
				return m, m.doLoadTracks(p.Label, p.Keywords)
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.pillsTable, cmd = m.pillsTable.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	if m.mode == pillModeTracks {
		m.tracksTable, cmd = m.tracksTable.Update(msg)
	} else {
		m.pillsTable, cmd = m.pillsTable.Update(msg)
	}
	return m, cmd
}

func (m PillsModel) View() tea.View {
	if m.mode == pillModeTracks {
		return tea.NewView(m.viewTracks())
	}
	return tea.NewView(m.viewList())
}

func (m PillsModel) viewList() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(Primary).MarginBottom(1)
	helpStyle := lipgloss.NewStyle().Foreground(Secondary)
	emptyStyle := lipgloss.NewStyle().Foreground(Muted).Italic(true)
	errorStyle := lipgloss.NewStyle().Foreground(Danger)

	active := 0
	for _, p := range m.pills {
		if p.Active {
			active++
		}
	}

	s := titleStyle.Render(fmt.Sprintf("Genre Pills (%d active / %d total)", active, len(m.pills))) + "\n"

	if !m.loaded {
		s += emptyStyle.Render("  Loading...")
	} else if m.refreshError != "" {
		s += errorStyle.Render("  Error: " + m.refreshError)
	} else if len(m.pillsTable.Rows()) == 0 {
		s += emptyStyle.Render("  No pills. They seed automatically on startup.")
	} else {
		s += m.pillsTable.View()
	}

	s += "\n" + helpStyle.Render("  \u2714 = enough listenable music to surface  ·  [enter] matching tracks  [r] recalculate  [\u2191/\u2193] navigate")
	return s
}

func (m PillsModel) viewTracks() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(Primary).MarginBottom(1)
	helpStyle := lipgloss.NewStyle().Foreground(Secondary)
	emptyStyle := lipgloss.NewStyle().Foreground(Muted).Italic(true)
	errorStyle := lipgloss.NewStyle().Foreground(Danger)

	s := titleStyle.Render("Matching tracks \u2014 "+m.currentLabel) + "\n"

	if m.tracksError != "" {
		s += errorStyle.Render("  Error: " + m.tracksError)
	} else if len(m.tracksTable.Rows()) == 0 {
		s += emptyStyle.Render("  No matching tracks yet for this pill.")
	} else {
		s += m.tracksTable.View()
	}

	s += "\n" + helpStyle.Render("  [enter] play  [esc] back to pills  [\u2191/\u2193] navigate")
	return s
}

func (m PillsModel) doRefresh() tea.Cmd {
	dbConn := m.DB
	return func() tea.Msg {
		db.SeedPillsIfEmpty(dbConn)
		pills, err := db.ListPillsWithCoverage(dbConn)
		return pillsRefreshMsg{Pills: pills, Err: err}
	}
}

func (m PillsModel) doLoadTracks(label, keywords string) tea.Cmd {
	dbConn := m.DB
	return func() tea.Msg {
		tracks, err := db.TracksForPill(dbConn, keywords, 100)
		return pillsTracksMsg{Label: label, Tracks: tracks, Err: err}
	}
}

func pillsColumns() []table.Column {
	return []table.Column{
		{Title: "Pill", Width: 22},
		{Title: "Albums", Width: 8},
		{Title: "Active", Width: 7},
		{Title: "Keywords", Width: 44},
	}
}

func pillTrackColumns() []table.Column {
	return []table.Column{
		{Title: "Track", Width: 34},
		{Title: "Artist", Width: 22},
		{Title: "Album", Width: 26},
		{Title: "Listen", Width: 7},
		{Title: "Qual", Width: 6},
	}
}
