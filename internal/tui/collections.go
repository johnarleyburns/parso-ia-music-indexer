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
	colModeCreatePlaylist
	colModeImportPlaylist
	colModeImportByURL
	colModeDeleteConfirm
	colModeProgress
)

type collectionsRefreshMsg struct {
	Collections []db.Collection
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

	addID    textinput.Model
	addTitle textinput.Model
	addQuery textinput.Model
	addCount textinput.Model
	addField int

	playlistName  textinput.Model
	playlistQuery textinput.Model
	playlistLimit textinput.Model
	plField       int

	importParent textinput.Model
	importList   textinput.Model
	importTitle  textinput.Model
	imField      int

	importURL      textinput.Model
	importURLTitle textinput.Model
	urlField       int

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

	plName := ti("playlist name (e.g. Gamelan Finds)")
	plQuery := ti("IA search query (e.g. subject:gamelan mediatype:audio)")
	plLimit := ti("max items (default: 50)")
	plLimit.CharLimit = 5

	imParent := ti("parent collection ID (e.g. fav-username)")
	imList := ti("list name on IA (e.g. favorites)")
	imTitle := ti("display title")

	urlInput := ti("playlist URL (e.g. https://archive.org/details/@user/lists/3/name)")
	urlInput.SetWidth(70)
	urlTitle := ti("display title (optional)")

	t := newTable(collectionsColumns())
	t.SetHeight(10)

	return CollectionsModel{
		DB:             sqlDB,
		mode:           colModeView,
		table:          t,
		addID:          addID,
		addTitle:       addTitle,
		addQuery:       addQuery,
		addCount:       addCount,
		playlistName:   plName,
		playlistQuery:  plQuery,
		playlistLimit:  plLimit,
		importParent:   imParent,
		importList:     imList,
		importTitle:    imTitle,
		importURL:      urlInput,
		importURLTitle: urlTitle,
	}
}

func (m CollectionsModel) Init() tea.Cmd {
	return m.doRefresh()
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
	case colModeCreatePlaylist:
		return m.updateCreatePlaylist(msg)
	case colModeImportPlaylist:
		return m.updateImportPlaylist(msg)
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
			rows := make([]table.Row, len(msg.Collections))
			for i, c := range msg.Collections {
				source := c.SourceType
				if source == "" {
					source = "collection"
				}
				rows[i] = table.Row{
					c.Title,
					source,
					c.IAURL(),
					c.CollectionID,
					fmt.Sprintf("%d", c.DiscoveredCount),
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
		case "s":
			m.mode = colModeCreatePlaylist
			m.playlistName.SetValue("")
			m.playlistQuery.SetValue("")
			m.playlistLimit.SetValue("")
			m.plField = 0
			m.playlistName.Focus()
			return m, nil
		case "i":
			m.mode = colModeImportPlaylist
			m.importParent.SetValue("")
			m.importList.SetValue("")
			m.importTitle.SetValue("")
			m.imField = 0
			m.importParent.Focus()
			return m, nil
		case "u":
			m.mode = colModeImportByURL
			m.importURL.SetValue("")
			m.importURLTitle.SetValue("")
			m.urlField = 0
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

func (m CollectionsModel) updateCreatePlaylist(msg tea.Msg) (CollectionsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.mode = colModeView
			return m, nil
		case "tab":
			m.plField = (m.plField + 1) % 3
			m.playlistName.Blur()
			m.playlistQuery.Blur()
			m.playlistLimit.Blur()
			switch m.plField {
			case 0:
				m.playlistName.Focus()
			case 1:
				m.playlistQuery.Focus()
			case 2:
				m.playlistLimit.Focus()
			}
			return m, nil
		case "enter":
			name := strings.TrimSpace(m.playlistName.Value())
			query := strings.TrimSpace(m.playlistQuery.Value())
			if name == "" || query == "" {
				return m, nil
			}
			limit := 50
			if s := strings.TrimSpace(m.playlistLimit.Value()); s != "" {
				fmt.Sscanf(s, "%d", &limit)
			}
			m.mode = colModeProgress
			m.progressState = "Starting..."
			m.progressCurrent = 0
			m.progressTotal = limit
			m.progressErr = nil
			return m, m.doCreatePlaylist(name, query, limit)
		}
	}

	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.playlistName, cmd = m.playlistName.Update(msg)
	cmds = append(cmds, cmd)
	m.playlistQuery, cmd = m.playlistQuery.Update(msg)
	cmds = append(cmds, cmd)
	m.playlistLimit, cmd = m.playlistLimit.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m CollectionsModel) updateImportPlaylist(msg tea.Msg) (CollectionsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.mode = colModeView
			return m, nil
		case "tab":
			m.imField = (m.imField + 1) % 3
			m.importParent.Blur()
			m.importList.Blur()
			m.importTitle.Blur()
			switch m.imField {
			case 0:
				m.importParent.Focus()
			case 1:
				m.importList.Focus()
			case 2:
				m.importTitle.Focus()
			}
			return m, nil
		case "enter":
			parent := strings.TrimSpace(m.importParent.Value())
			listName := strings.TrimSpace(m.importList.Value())
			title := strings.TrimSpace(m.importTitle.Value())
			if parent == "" || listName == "" {
				return m, nil
			}
			m.mode = colModeProgress
			m.progressState = "Starting import..."
			m.progressCurrent = 0
			m.progressTotal = 0
			m.progressErr = nil
			return m, m.doImportPlaylist(parent, listName, title)
		}
	}

	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.importParent, cmd = m.importParent.Update(msg)
	cmds = append(cmds, cmd)
	m.importList, cmd = m.importList.Update(msg)
	cmds = append(cmds, cmd)
	m.importTitle, cmd = m.importTitle.Update(msg)
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
		case "tab":
			m.urlField = (m.urlField + 1) % 2
			m.importURL.Blur()
			m.importURLTitle.Blur()
			switch m.urlField {
			case 0:
				m.importURL.Focus()
			case 1:
				m.importURLTitle.Focus()
			}
			return m, nil
		case "enter":
			rawURL := strings.TrimSpace(m.importURL.Value())
			title := strings.TrimSpace(m.importURLTitle.Value())
			parent, listName := parsePlaylistURL(rawURL)
			if parent == "" || listName == "" {
				return m, nil
			}
			if title == "" {
				title = listName
			}
			m.mode = colModeProgress
			m.progressState = "Starting import..."
			m.progressCurrent = 0
			m.progressTotal = 0
			m.progressErr = nil
			return m, m.doImportPlaylist(parent, listName, title)
		}
	}

	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.importURL, cmd = m.importURL.Update(msg)
	cmds = append(cmds, cmd)
	m.importURLTitle, cmd = m.importURLTitle.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
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
	case colModeCreatePlaylist:
		return tea.NewView(m.viewCreatePlaylist())
	case colModeImportPlaylist:
		return tea.NewView(m.viewImportPlaylist())
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

	s += "\n" + helpStyle.Render("  [c] add source  [v] view in browse  [o] open URL  [d] delete  [r] refresh  [↑/↓] navigate")
	return s
}

func (m CollectionsModel) viewSourceSelect() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(Primary).MarginBottom(1)
	helpStyle := lipgloss.NewStyle().Foreground(Muted)

	s := titleStyle.Render("Add Source") + "\n\n"
	s += "  [a] Add IA Collection (discover from scrape)\n"
	s += "  [s] Create Playlist from Search\n"
	s += "  [i] Import Existing Playlist from IA\n"
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

func (m CollectionsModel) viewCreatePlaylist() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(Success).MarginBottom(1)
	labelStyle := lipgloss.NewStyle().Foreground(Secondary)
	helpStyle := lipgloss.NewStyle().Foreground(Muted)

	s := titleStyle.Render("Create Playlist from Search") + "\n\n"

	s += labelStyle.Render("  Name:  ") + m.playlistName.View() + "\n"
	s += labelStyle.Render("  Query: ") + m.playlistQuery.View() + "\n"
	s += labelStyle.Render("  Limit: ") + m.playlistLimit.View() + "\n\n"

	s += helpStyle.Render("  [tab] next field  [enter] create  [esc] cancel")
	return s
}

func (m CollectionsModel) viewImportPlaylist() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(Success).MarginBottom(1)
	labelStyle := lipgloss.NewStyle().Foreground(Secondary)
	helpStyle := lipgloss.NewStyle().Foreground(Muted)

	s := titleStyle.Render("Import Existing Playlist") + "\n\n"

	s += labelStyle.Render("  Parent: ") + m.importParent.View() + "\n"
	s += labelStyle.Render("  List:   ") + m.importList.View() + "\n"
	s += labelStyle.Render("  Title:  ") + m.importTitle.View() + "\n\n"

	s += helpStyle.Render("  [tab] next field  [enter] import  [esc] cancel")
	return s
}

func (m CollectionsModel) viewImportByURL() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(Success).MarginBottom(1)
	labelStyle := lipgloss.NewStyle().Foreground(Secondary)
	helpStyle := lipgloss.NewStyle().Foreground(Muted)

	s := titleStyle.Render("Import Playlist by URL") + "\n\n"

	s += labelStyle.Render("  URL:   ") + m.importURL.View() + "\n"
	s += labelStyle.Render("  Title: ") + m.importURLTitle.View() + "\n\n"

	s += helpStyle.Render("  [tab] next field  [enter] import  [esc] cancel")
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

func (m CollectionsModel) doRefresh() tea.Cmd {
	dbConn := m.DB
	return func() tea.Msg {
		log.Printf("[collections] doRefresh: querying DB for all collections")
		collections, err := db.GetAllCollections(dbConn)
		log.Printf("[collections] doRefresh: got %d collections, err=%v", len(collections), err)
		return collectionsRefreshMsg{Collections: collections, Err: err}
	}
}

func (m CollectionsModel) doInsert(c db.CollectionInsert) tea.Cmd {
	dbConn := m.DB
	return func() tea.Msg {
		if err := db.InsertCollection(dbConn, c); err != nil {
			return collectionsRefreshMsg{Err: err}
		}
		collections, err := db.GetAllCollections(dbConn)
		return collectionsRefreshMsg{Collections: collections, Err: err}
	}
}

func (m CollectionsModel) doDelete(collectionID string) tea.Cmd {
	dbConn := m.DB
	return func() tea.Msg {
		if err := db.RemoveCollection(dbConn, collectionID); err != nil {
			return collectionsRefreshMsg{Err: err}
		}
		collections, err := db.GetAllCollections(dbConn)
		return collectionsRefreshMsg{Collections: collections, Err: err}
	}
}

func (m CollectionsModel) doCreatePlaylist(name, query string, limit int) tea.Cmd {
	sqlDB := m.DB
	return func() tea.Msg {
		creds, err := loadIACredentials()
		if err != nil {
			return playlistProgressMsg{Done: true, Err: err}
		}

		iaClient := newIAClient()

		input := playlist.CreateInput{
			Name:  name,
			Query: query,
			Limit: limit,
		}

		_, err = playlist.CreateFromSearch(
			&db.DB{Conn: sqlDB},
			iaClient,
			creds,
			input,
			func(state string, current, total int) {
				log.Printf("[collections] progress: %s (%d/%d)", state, current, total)
			},
		)

		if err != nil {
			return playlistProgressMsg{Done: true, Err: err}
		}

		return playlistProgressMsg{
			State:   fmt.Sprintf("Created playlist %q with %d items", name, limit),
			Current: limit,
			Total:   limit,
			Done:    true,
		}
	}
}

func (m CollectionsModel) doImportPlaylist(parent, listName, title string) tea.Cmd {
	sqlDB := m.DB
	return func() tea.Msg {
		iaClient := newIAClient()

		input := playlist.ImportInput{
			ParentID: parent,
			ListName: listName,
			Title:    title,
		}

		count, err := playlist.ImportExistingPlaylist(
			&db.DB{Conn: sqlDB},
			iaClient,
			input,
			func(state string, current, total int) {
				log.Printf("[collections] import progress: %s (%d/%d)", state, current, total)
			},
		)

		if err != nil {
			return playlistProgressMsg{Done: true, Err: err}
		}

		return playlistProgressMsg{
			State:   fmt.Sprintf("Imported %d items from %s/%s", count, parent, listName),
			Current: count,
			Total:   count,
			Done:    true,
		}
	}
}

func collectionsColumns() []table.Column {
	return []table.Column{
		{Title: "Title", Width: 30},
		{Title: "Source", Width: 12},
		{Title: "URL", Width: 40},
		{Title: "ID", Width: 25},
		{Title: "Items", Width: 8},
		{Title: "Status", Width: 10},
	}
}

func parsePlaylistURL(rawURL string) (parentID, listName string) {
	u := strings.TrimSpace(rawURL)
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "archive.org/details/")

	parts := strings.Split(u, "/")
	var user string
	for i, p := range parts {
		if strings.HasPrefix(p, "@") {
			user = p
			if i+1 < len(parts) {
				rest := parts[i+1:]
				for j, r := range rest {
					if r == "lists" && j+2 < len(rest) {
						listName = rest[j+2]
						break
					}
				}
			}
			break
		}
	}
	if strings.HasPrefix(listName, "@") || listName == "lists" || listName == "" {
		parts := strings.Split(u, "/")
		var foundUser bool
		var foundLists bool
		for _, p := range parts {
			if strings.HasPrefix(p, "@") {
				user = p
				foundUser = true
			} else if foundUser && p == "lists" {
				foundLists = true
			} else if foundLists && isNumeric(p) {
				continue
			} else if foundLists && p != "" {
				listName = p
				break
			}
		}
	}
	if user == "" || listName == "" {
		return "", ""
	}
	return user, listName
}

func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
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

func loadIACredentials() (*ia.IACredentials, error) {
	return ia.LoadCredentials()
}

func newIAClient() *http.Client {
	return ia.NewClient(60 * time.Second)
}
