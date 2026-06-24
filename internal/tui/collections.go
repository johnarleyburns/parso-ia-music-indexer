package tui

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/johnarleyburns/parso-ia-music-indexer/internal/db"
	"github.com/johnarleyburns/parso-ia-music-indexer/internal/ia"
	"github.com/johnarleyburns/parso-ia-music-indexer/internal/playlist"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

type collectionsMode int

const (
	colModeView collectionsMode = iota
	colModeSourceSelect
	colModeAddCollection
	colModeImportByURL
	colModeDeleteConfirm
	colModeProgress
)

type collectionsRefreshMsg struct {
	Collections []db.Collection
	Stats       map[string]db.CollectionTrackStat
	Err         error
}

type playlistProgressMsg struct {
	State   string
	Current int
	Total   int
	Done    bool
	Err     error
}

type SwitchToBrowseMsg struct {
	Query string
	Title string
}

type CollectionsModel struct {
	DB     *sql.DB
	Width  int
	Height int

	mode         collectionsMode
	table        table.Model
	loaded       bool
	refreshError string
	collections  []db.Collection

	addID    textinput.Model
	addTitle textinput.Model
	addQuery textinput.Model
	addCount textinput.Model
	addField int

	importURL textinput.Model

	deleteTarget    string
	deleteTitle     string
	selectedURL     string
	selectedSource  string
	selectedQuery   string
	progressState   string
	progressCurrent int
	progressTotal   int
	progressErr     error

	selectedColTitle string
}

func NewCollectionsModel(sqlDB *sql.DB) CollectionsModel {
	ti := func(placeholder string) textinput.Model {
		t := textinput.New()
		t.Placeholder = placeholder
		t.SetWidth(50)
		km := textinput.DefaultKeyMap()
		km.AcceptSuggestion = key.NewBinding(key.WithDisabled())
		km.NextSuggestion = key.NewBinding(key.WithDisabled())
		km.PrevSuggestion = key.NewBinding(key.WithDisabled())
		t.KeyMap = km
		return t
	}

	addID := ti("collection identifier (e.g. vinyl_mycollection)")
	addTitle := ti("collection title")
	addQuery := ti("search query (default: collection:<id>)")
	addCount := ti("expected item count (default: 1000)")
	addCount.CharLimit = 7

	urlInput := ti("playlist URL (e.g. https://archive.org/details/@user/lists/3/name)")
	urlInput.SetWidth(70)

	t := newTable(collectionsColumns())
	t.SetHeight(10)

	return CollectionsModel{
		DB:        sqlDB,
		mode:      colModeView,
		table:     t,
		addID:     addID,
		addTitle:  addTitle,
		addQuery:  addQuery,
		addCount:  addCount,
		importURL: urlInput,
	}
}

func (m CollectionsModel) Init() tea.Cmd {
	return m.doRefresh()
}

func (m CollectionsModel) InputFocused() bool {
	return m.mode == colModeAddCollection ||
		m.mode == colModeImportByURL
}

func (m CollectionsModel) Update(msg tea.Msg) (CollectionsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		tableHeight := msg.Height - 12
		if tableHeight < 5 {
			tableHeight = 5
		}
		m.table.SetHeight(tableHeight)
		if w := msg.Width - 4; w > 0 {
			m.table.SetWidth(w)
		}
	}

	switch m.mode {
	case colModeSourceSelect:
		return m.updateSourceSelect(msg)
	case colModeAddCollection:
		return m.updateAddCollection(msg)
	case colModeImportByURL:
		return m.updateImportByURL(msg)
	case colModeDeleteConfirm:
		return m.updateDeleteConfirm(msg)
	case colModeProgress:
		return m.updateProgress(msg)
	default:
		return m.updateView(msg)
	}
}

func (m CollectionsModel) updateView(msg tea.Msg) (CollectionsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case collectionsRefreshMsg:
		log.Printf("[collections] updateView: received collectionsRefreshMsg, err=%v, count=%d", msg.Err, len(msg.Collections))
		if msg.Err != nil {
			m.refreshError = msg.Err.Error()
			m.loaded = true
		} else {
			m.refreshError = ""
			m.collections = msg.Collections
			rows := make([]table.Row, len(msg.Collections))
			for i, c := range msg.Collections {
				source := c.SourceType
				if source == "" {
					source = "collection"
				}
				st := msg.Stats[c.CollectionID]
				pct := "-"
				if st.Total > 0 {
					pct = fmt.Sprintf("%d%%", st.Analyzed*100/st.Total)
				}
				rows[i] = table.Row{
					c.Title,
					source,
					c.IAURL(),
					c.CollectionID,
					fmt.Sprintf("%d", st.Total),
					fmt.Sprintf("%d", st.Analyzed),
					pct,
					c.Status,
				}
			}
			m.table.SetRows(rows)
			log.Printf("[collections] updateView: table.SetRows called with %d rows, table.Rows()=%d", len(rows), len(m.table.Rows()))
			m.loaded = true
			m.table.Focus()
		}
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "c":
			m.mode = colModeSourceSelect
			return m, nil
		case "d":
			idx := m.table.Cursor()
			rows := m.table.Rows()
			if idx >= 0 && idx < len(rows) {
				row := rows[idx]
				m.mode = colModeDeleteConfirm
				m.deleteTarget = row[3]
				m.deleteTitle = row[0]
			}
			return m, nil
		case "r":
			return m, m.doRefresh()
		case "v":
			idx := m.table.Cursor()
			rows := m.table.Rows()
			if idx >= 0 && idx < len(rows) {
				row := rows[idx]
				colID := row[3]
				m.selectedColTitle = row[0]
				query := fmt.Sprintf("collection:%s", colID)
				return m, func() tea.Msg {
					return SwitchToBrowseMsg{Query: query, Title: m.selectedColTitle}
				}
			}
			return m, nil
		case "o":
			idx := m.table.Cursor()
			rows := m.table.Rows()
			if idx >= 0 && idx < len(rows) {
				row := rows[idx]
				url := row[2]
				openBrowser(url)
			}
			return m, nil
		case "s":
			idx := m.table.Cursor()
			if idx >= 0 && idx < len(m.collections) {
				col := m.collections[idx]
				if col.SourceType == "playlist" || col.SourceType == "simplelist" {
					m.mode = colModeProgress
					m.progressState = "Starting sync..."
					m.progressCurrent = 0
					m.progressTotal = 0
					m.progressErr = nil
					return m, m.doSyncPlaylist(col)
				}
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m CollectionsModel) updateSourceSelect(msg tea.Msg) (CollectionsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "a":
			m.mode = colModeAddCollection
			m.addID.SetValue("")
			m.addTitle.SetValue("")
			m.addQuery.SetValue("")
			m.addCount.SetValue("")
			m.addField = 0
			m.addID.Focus()
			return m, nil
		case "u":
			m.mode = colModeImportByURL
			m.importURL.SetValue("")
			m.importURL.Focus()
			return m, nil
		case "esc":
			m.mode = colModeView
			return m, nil
		}
	}
	return m, nil
}

func (m CollectionsModel) updateAddCollection(msg tea.Msg) (CollectionsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.mode = colModeView
			return m, nil
		case "tab":
			m.addField = (m.addField + 1) % 4
			m.addID.Blur()
			m.addTitle.Blur()
			m.addQuery.Blur()
			m.addCount.Blur()
			switch m.addField {
			case 0:
				m.addID.Focus()
			case 1:
				m.addTitle.Focus()
			case 2:
				m.addQuery.Focus()
			case 3:
				m.addCount.Focus()
			}
			return m, nil
		case "enter":
			m.mode = colModeView
			id := strings.TrimSpace(m.addID.Value())
			title := strings.TrimSpace(m.addTitle.Value())
			if id == "" || title == "" {
				return m, nil
			}
			query := strings.TrimSpace(m.addQuery.Value())
			if query == "" {
				query = fmt.Sprintf("collection:%s", id)
			}
			count := 1000
			if s := strings.TrimSpace(m.addCount.Value()); s != "" {
				fmt.Sscanf(s, "%d", &count)
			}
			return m, m.doInsert(db.CollectionInsert{
				CollectionID:  id,
				Title:         title,
				Query:         query,
				ExpectedCount: count,
				SourceType:    "collection",
			})
		}
	}

	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.addID, cmd = m.addID.Update(msg)
	cmds = append(cmds, cmd)
	m.addTitle, cmd = m.addTitle.Update(msg)
	cmds = append(cmds, cmd)
	m.addQuery, cmd = m.addQuery.Update(msg)
	cmds = append(cmds, cmd)
	m.addCount, cmd = m.addCount.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m CollectionsModel) updateImportByURL(msg tea.Msg) (CollectionsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.mode = colModeView
			return m, nil
		case "enter":
			rawURL := strings.TrimSpace(m.importURL.Value())
			if rawURL == "" {
				return m, nil
			}
			m.mode = colModeProgress
			m.progressState = "Starting import..."
			m.progressCurrent = 0
			m.progressTotal = 0
			m.progressErr = nil
			return m, m.doImportPatronList(rawURL)
		}
	}

	var cmd tea.Cmd
	m.importURL, cmd = m.importURL.Update(msg)
	return m, cmd
}

func (m CollectionsModel) updateProgress(msg tea.Msg) (CollectionsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case playlistProgressMsg:
		m.progressState = msg.State
		m.progressCurrent = msg.Current
		m.progressTotal = msg.Total
		if msg.Err != nil {
			m.progressErr = msg.Err
		}
		if msg.Done {
			refreshCmd := m.doRefresh()
			if msg.Err != nil {
				return m, refreshCmd
			}
			m.mode = colModeView
			return m, refreshCmd
		}
		return m, nil
	case tea.KeyPressMsg:
		if msg.String() == "esc" {
			m.mode = colModeView
			return m, nil
		}
	}
	return m, nil
}

func (m CollectionsModel) updateDeleteConfirm(msg tea.Msg) (CollectionsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "y", "Y":
			m.mode = colModeView
			return m, m.doDelete(m.deleteTarget)
		case "n", "N", "esc":
			m.mode = colModeView
			return m, nil
		}
	}
	return m, nil
}

func (m CollectionsModel) View() tea.View {
	switch m.mode {
	case colModeSourceSelect:
		return tea.NewView(m.viewSourceSelect())
	case colModeAddCollection:
		return tea.NewView(m.viewAddCollection())
	case colModeImportByURL:
		return tea.NewView(m.viewImportByURL())
	case colModeDeleteConfirm:
		return tea.NewView(m.viewDeleteConfirm())
	case colModeProgress:
		return tea.NewView(m.viewProgress())
	default:
		return tea.NewView(m.viewList())
	}
}

func (m CollectionsModel) viewList() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(Primary).MarginBottom(1)
	helpStyle := lipgloss.NewStyle().Foreground(Secondary)
	emptyStyle := lipgloss.NewStyle().Foreground(Muted).Italic(true)
	errorStyle := lipgloss.NewStyle().Foreground(Danger)
	urlStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#06b6d4")).Underline(true)

	s := titleStyle.Render("Collections & Playlists") + "\n"

	if !m.loaded {
		s += emptyStyle.Render("  Loading...")
	} else if m.refreshError != "" {
		s += errorStyle.Render("  Error: " + m.refreshError)
	} else if len(m.table.Rows()) == 0 {
		s += emptyStyle.Render("  No collections. Press c to add one.")
	} else {
		tv := m.table.View()
		s += tv

		idx := m.table.Cursor()
			rows := m.table.Rows()
			if idx >= 0 && idx < len(rows) {
				row := rows[idx]
				if len(row) > 2 {
					url := row[2]
					maxW := m.Width - 4
					if maxW < 20 {
						maxW = 20
					}
					s += "\n  " + urlStyle.MaxWidth(maxW).Render(url)
				}
			}
	}

	s += "\n" + helpStyle.Render("  [c] add source  [v] view in browse  [o] open URL  [s] sync playlist  [d] delete  [r] refresh  [↑/↓] navigate")
	return s
}

func (m CollectionsModel) viewSourceSelect() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(Primary).MarginBottom(1)
	helpStyle := lipgloss.NewStyle().Foreground(Muted)

	s := titleStyle.Render("Add Source") + "\n\n"
	s += "  [a] Add IA Collection (discover from scrape)\n"
	s += "  [u] Import Playlist by URL\n\n"
	s += helpStyle.Render("  [esc] cancel")
	return s
}

func (m CollectionsModel) viewAddCollection() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#06b6d4")).MarginBottom(1)
	labelStyle := lipgloss.NewStyle().Foreground(Secondary)
	helpStyle := lipgloss.NewStyle().Foreground(Muted)

	s := titleStyle.Render("Add IA Collection") + "\n\n"

	s += labelStyle.Render("  ID:    ") + m.addID.View() + "\n"
	s += labelStyle.Render("  Title: ") + m.addTitle.View() + "\n"
	s += labelStyle.Render("  Query: ") + m.addQuery.View() + "\n"
	s += labelStyle.Render("  Count: ") + m.addCount.View() + "\n\n"

	s += helpStyle.Render("  [tab] next field  [enter] save  [esc] cancel")
	return s
}

func (m CollectionsModel) viewImportByURL() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(Success).MarginBottom(1)
	labelStyle := lipgloss.NewStyle().Foreground(Secondary)
	helpStyle := lipgloss.NewStyle().Foreground(Muted)

	s := titleStyle.Render("Import Playlist by URL") + "\n\n"

	s += labelStyle.Render("  URL: ") + m.importURL.View() + "\n\n"
	s += helpStyle.Render("  Title is taken from the IA playlist name.") + "\n\n"

	s += helpStyle.Render("  [enter] import  [esc] cancel")
	return s
}

func (m CollectionsModel) viewProgress() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(Primary).MarginBottom(1)
	barStyle := lipgloss.NewStyle().Foreground(Success)
	mutedStyle := lipgloss.NewStyle().Foreground(Muted)
	errorStyle := lipgloss.NewStyle().Foreground(Danger)

	s := titleStyle.Render("Progress") + "\n\n"

	s += "  " + m.progressState + "\n\n"

	if m.progressTotal > 0 {
		pct := float64(m.progressCurrent) / float64(m.progressTotal)
		barWidth := 40
		filled := int(pct * float64(barWidth))
		if filled > barWidth {
			filled = barWidth
		}
		bar := barStyle.Render(strings.Repeat("█", filled)) + mutedStyle.Render(strings.Repeat("░", barWidth-filled))
		s += fmt.Sprintf("  %s %d/%d\n", bar, m.progressCurrent, m.progressTotal)
	}

	if m.progressErr != nil {
		s += "\n" + errorStyle.Render("  Error: "+m.progressErr.Error())
	}

	s += "\n\n" + mutedStyle.Render("  [esc] back")
	return s
}

func (m CollectionsModel) viewDeleteConfirm() string {
	warnStyle := lipgloss.NewStyle().Bold(true).Foreground(Danger)
	labelStyle := lipgloss.NewStyle().Foreground(Secondary)
	helpStyle := lipgloss.NewStyle().Foreground(Muted)

	s := warnStyle.Render("Delete?") + "\n\n"
	s += fmt.Sprintf("  %s%s (%s)\n\n", labelStyle.Render("ID: "), m.deleteTarget, m.deleteTitle)
	s += helpStyle.Render("  [y] yes, delete  [n] no, cancel")
	return s
}

func loadCollectionsWithStats(dbConn *sql.DB) collectionsRefreshMsg {
	collections, err := db.GetAllCollections(dbConn)
	if err != nil {
		return collectionsRefreshMsg{Err: err}
	}
	stats, err := db.GetCollectionTrackStats(dbConn)
	if err != nil {
		return collectionsRefreshMsg{Err: err}
	}
	return collectionsRefreshMsg{Collections: collections, Stats: stats}
}

func (m CollectionsModel) doRefresh() tea.Cmd {
	dbConn := m.DB
	return func() tea.Msg {
		log.Printf("[collections] doRefresh: querying DB for all collections")
		msg := loadCollectionsWithStats(dbConn)
		log.Printf("[collections] doRefresh: got %d collections, err=%v", len(msg.Collections), msg.Err)
		return msg
	}
}

func (m CollectionsModel) doInsert(c db.CollectionInsert) tea.Cmd {
	dbConn := m.DB
	return func() tea.Msg {
		if err := db.InsertCollection(dbConn, c); err != nil {
			return collectionsRefreshMsg{Err: err}
		}
		return loadCollectionsWithStats(dbConn)
	}
}

func (m CollectionsModel) doDelete(collectionID string) tea.Cmd {
	dbConn := m.DB
	return func() tea.Msg {
		if err := db.RemoveCollection(dbConn, collectionID); err != nil {
			return collectionsRefreshMsg{Err: err}
		}
		return loadCollectionsWithStats(dbConn)
	}
}

func (m CollectionsModel) doSyncPlaylist(col db.Collection) tea.Cmd {
	sqlDB := m.DB
	return func() tea.Msg {
		iaClient := newIAClient()

		count, err := playlist.SyncPlaylist(
			&db.DB{Conn: sqlDB},
			iaClient,
			col,
			func(state string, current, total int) {
				log.Printf("[collections] sync progress: %s (%d/%d)", state, current, total)
			},
		)

		if err != nil {
			return playlistProgressMsg{Done: true, Err: err}
		}

		return playlistProgressMsg{
			State:   fmt.Sprintf("Synced %d items from IA", count),
			Current: count,
			Total:   count,
			Done:    true,
		}
	}
}

func (m CollectionsModel) doImportPatronList(rawURL string) tea.Cmd {
	sqlDB := m.DB
	return func() tea.Msg {
		iaClient := newIAClient()

		count, err := playlist.ImportPatronList(
			&db.DB{Conn: sqlDB},
			iaClient,
			rawURL,
			"",
			func(state string, current, total int) {
				log.Printf("[collections] patron import progress: %s (%d/%d)", state, current, total)
			},
		)

		if err != nil {
			return playlistProgressMsg{Done: true, Err: err}
		}

		return playlistProgressMsg{
			State:   fmt.Sprintf("Imported %d items from patron list", count),
			Current: count,
			Total:   count,
			Done:    true,
		}
	}
}

func collectionsColumns() []table.Column {
	return []table.Column{
		{Title: "Title", Width: 28},
		{Title: "Source", Width: 10},
		{Title: "URL", Width: 30},
		{Title: "ID", Width: 20},
		{Title: "Tracks", Width: 8},
		{Title: "Analyzed", Width: 9},
		{Title: "% Done", Width: 7},
		{Title: "Status", Width: 10},
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	cmd.Start()
}

func newIAClient() *http.Client {
	return ia.NewClient(60 * time.Second)
}
