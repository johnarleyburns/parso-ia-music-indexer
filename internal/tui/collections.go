package tui

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/johnarleyburns/parso-ia-music-indexer/internal/db"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

type collectionsMode int

const (
	colModeView collectionsMode = iota
	colModeAdd
	colModeDeleteConfirm
)

type collectionsRefreshMsg struct {
	Collections []db.Collection
	Err         error
}

type CollectionsModel struct {
	DB     *sql.DB
	Width  int
	Height int

	mode    collectionsMode
	table   table.Model
	loaded  bool

	addID    textinput.Model
	addTitle textinput.Model
	addQuery textinput.Model
	addCount textinput.Model
	addField int

	deleteTarget string
	deleteTitle  string
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

	t := newTable(collectionsColumns())
	t.SetHeight(10)

	return CollectionsModel{
		DB:       sqlDB,
		mode:     colModeView,
		table:    t,
		addID:    addID,
		addTitle: addTitle,
		addQuery: addQuery,
		addCount: addCount,
	}
}

func (m CollectionsModel) Init() tea.Cmd {
	return m.doRefresh()
}

func (m CollectionsModel) Update(msg tea.Msg) (CollectionsModel, tea.Cmd) {
	switch m.mode {
	case colModeAdd:
		return m.updateAdd(msg)
	case colModeDeleteConfirm:
		return m.updateDeleteConfirm(msg)
	default:
		return m.updateView(msg)
	}
}

func (m CollectionsModel) updateView(msg tea.Msg) (CollectionsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case collectionsRefreshMsg:
		if msg.Err == nil {
			rows := make([]table.Row, len(msg.Collections))
			for i, c := range msg.Collections {
				rows[i] = table.Row{
					c.CollectionID,
					c.Title,
					c.Category,
					fmt.Sprintf("%d", c.DiscoveredCount),
					c.Status,
				}
			}
			m.table.SetRows(rows)
			m.loaded = true
		}
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "a":
			m.mode = colModeAdd
			m.addID.SetValue("")
			m.addTitle.SetValue("")
			m.addQuery.SetValue("")
			m.addCount.SetValue("")
			m.addField = 0
			m.addID.Focus()
			return m, nil
		case "d":
			idx := m.table.Cursor()
			rows := m.table.Rows()
			if idx >= 0 && idx < len(rows) {
				row := rows[idx]
				m.mode = colModeDeleteConfirm
				m.deleteTarget = row[0]
				m.deleteTitle = row[1]
			}
			return m, nil
		case "r":
			return m, m.doRefresh()
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m CollectionsModel) updateAdd(msg tea.Msg) (CollectionsModel, tea.Cmd) {
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
	case colModeAdd:
		return tea.NewView(m.viewAdd())
	case colModeDeleteConfirm:
		return tea.NewView(m.viewDeleteConfirm())
	default:
		return tea.NewView(m.viewList())
	}
}

func (m CollectionsModel) viewList() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(Primary).MarginBottom(1)
	helpStyle := lipgloss.NewStyle().Foreground(Secondary)
	emptyStyle := lipgloss.NewStyle().Foreground(Muted).Italic(true)

	s := titleStyle.Render("Collections") + "\n\n"

	if !m.loaded {
		s += emptyStyle.Render("  Loading...")
	} else if len(m.table.Rows()) == 0 {
		s += emptyStyle.Render("  No collections. Press a to add one.")
	} else {
		s += m.table.View()
	}

	s += "\n\n" + helpStyle.Render("  [a] add  [d] delete  [r] refresh  [\u2191/\u2193] navigate")
	return s
}

func (m CollectionsModel) viewAdd() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#06b6d4")).MarginBottom(1)
	labelStyle := lipgloss.NewStyle().Foreground(Secondary)
	helpStyle := lipgloss.NewStyle().Foreground(Muted)

	s := titleStyle.Render("Add Collection") + "\n\n"

	s += labelStyle.Render("  ID:    ") + m.addID.View() + "\n"
	s += labelStyle.Render("  Title: ") + m.addTitle.View() + "\n"
	s += labelStyle.Render("  Query: ") + m.addQuery.View() + "\n"
	s += labelStyle.Render("  Count: ") + m.addCount.View() + "\n\n"

	s += helpStyle.Render("  [tab] next field  [enter] save  [esc] cancel")
	return s
}

func (m CollectionsModel) viewDeleteConfirm() string {
	warnStyle := lipgloss.NewStyle().Bold(true).Foreground(Danger)
	labelStyle := lipgloss.NewStyle().Foreground(Secondary)
	helpStyle := lipgloss.NewStyle().Foreground(Muted)

	s := warnStyle.Render("Delete Collection?") + "\n\n"
	s += fmt.Sprintf("  %s%s (%s)\n\n", labelStyle.Render("ID: "), m.deleteTarget, m.deleteTitle)
	s += helpStyle.Render("  [y] yes, delete  [n] no, cancel")
	return s
}

func (m CollectionsModel) doRefresh() tea.Cmd {
	dbConn := m.DB
	return func() tea.Msg {
		collections, err := db.GetAllCollections(dbConn)
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

func collectionsColumns() []table.Column {
	return []table.Column{
		{Title: "ID", Width: 30},
		{Title: "Title", Width: 40},
		{Title: "Category", Width: 15},
		{Title: "Albums", Width: 8},
		{Title: "Status", Width: 10},
	}
}
