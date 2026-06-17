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

const browsePageSize = 50

type BrowseMode int

const (
	ModeAlbums BrowseMode = iota
	ModeAlbumDetail
	ModeTracks
)

type browseAlbumSearchMsg struct {
	Albums     []db.AlbumResult
	TotalCount int
	Query      string
	Err        error
}

type browseAlbumDetailMsg struct {
	Album  *db.AlbumResult
	Tracks []db.TrackDetail
	Err    error
}

type browseSearchMsg struct {
	Tracks     []db.TrackResult
	TotalCount int
	Query      string
	Err        error
}

type browseSimilarMsg struct {
	SourceTrackID int
	Tracks        []db.SimilarTrack
	Err           error
}

type SwitchToPlayerMsg struct {
	TrackID     int
	Title       string
	AlbumID     string
	AlbumTitle  string
	ArtURL      string
	DownloadURL string
}

type BrowseModel struct {
	DB           *sql.DB
	Width        int
	Height       int
	mode         BrowseMode
	searchInput  textinput.Model
	table        table.Model
	inputFocused bool
	lastQuery    string
	totalCount   int
	page         int
	loaded       bool

	albumResults []db.AlbumResult
	trackResults []db.TrackResult

	detailAlbum  *db.AlbumResult
	detailTracks []db.TrackDetail

	similarityMode bool
	similarSource  int
	similarResults []db.SimilarTrack

	artCache       *ArtCache
	currentArt     string
	currentArtID   string
	lastCursor     int
}

func NewBrowseModel(sqlDB *sql.DB, artCache *ArtCache) BrowseModel {
	ti := textinput.New()
	ti.Placeholder = "Search albums..."
	ti.Prompt = "/ "
	ti.SetWidth(60)
	km := textinput.DefaultKeyMap()
	km.AcceptSuggestion = key.NewBinding(key.WithDisabled())
	km.NextSuggestion = key.NewBinding(key.WithDisabled())
	km.PrevSuggestion = key.NewBinding(key.WithDisabled())
	ti.KeyMap = km

	t := newTable(albumColumns())

	return BrowseModel{
		DB:           sqlDB,
		searchInput:  ti,
		table:        t,
		inputFocused: true,
		mode:         ModeAlbums,
		page:         0,
		artCache:     artCache,
		lastCursor:   -1,
	}
}

func newTable(cols []table.Column) table.Model {
	tableKM := table.DefaultKeyMap()
	tableKM.PageDown = key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down"))
	tableKM.PageUp = key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up"))

	t := table.New(
		table.WithColumns(cols),
		table.WithRows([]table.Row{}),
		table.WithHeight(10),
		table.WithFocused(false),
		table.WithKeyMap(tableKM),
	)

	s := table.DefaultStyles()
	s.Header = lipgloss.NewStyle().Bold(true).Foreground(Secondary).Padding(0, 1).
		Border(lipgloss.Border{Bottom: "─"}, false, false, true, false).BorderForeground(Muted)
	s.Cell = lipgloss.NewStyle().Padding(0, 1)
	s.Selected = lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color("#ffffff")).Background(Primary).Padding(0, 1)
	t.SetStyles(s)
	return t
}

func (m BrowseModel) InputFocused() bool {
	return m.inputFocused
}

func (m BrowseModel) Init() tea.Cmd {
	return nil
}

func (m BrowseModel) Update(msg tea.Msg) (BrowseModel, tea.Cmd) {
	switch msg := msg.(type) {
	case artLoadedMsg:
		if m.artCache != nil {
			m.artCache.StoreEncoded(msg.Identifier, msg.Cols, msg.Rows, msg.Encoded)
		}
		if msg.Identifier == m.currentArtID {
			m.currentArt = msg.Encoded
		}
		return m, nil

	case browseAlbumSearchMsg:
		if msg.Err != nil || msg.Query != m.lastQuery {
			return m, nil
		}
		m.totalCount = msg.TotalCount
		m.albumResults = msg.Albums
		m.similarityMode = false
		rows := make([]table.Row, len(msg.Albums))
		for i, a := range msg.Albums {
			qs := "---"
			if a.AvgQuality > 0 {
				qs = fmt.Sprintf("%.3f", a.AvgQuality)
			}
			rows[i] = table.Row{
				a.Title,
				a.Creator,
				a.Collection,
				fmt.Sprintf("%d/%d", a.CompletedCount, a.TrackCount),
				formatDownloads(a.Downloads),
				qs,
			}
		}
		m.table.SetColumns(albumColumns())
		m.table.SetRows(rows)
		m.table.GotoTop()
		m.loaded = true
		m.lastCursor = 0
		if len(msg.Albums) > 0 {
			return m, m.loadAlbumArt(msg.Albums[0].IAIdentifier, msg.Albums[0].ArtURL)
		}
		return m, nil

	case browseAlbumDetailMsg:
		if msg.Err != nil {
			return m, nil
		}
		m.mode = ModeAlbumDetail
		m.detailAlbum = msg.Album
		m.detailTracks = msg.Tracks
		m.similarityMode = false
		rows := make([]table.Row, len(msg.Tracks))
		for i, t := range msg.Tracks {
			num := ""
			if t.TrackNumber > 0 {
				num = fmt.Sprintf("%d", t.TrackNumber)
			}
			qs := "—"
			if t.Status == "completed" && t.QualityScore > 0 {
				qs = fmt.Sprintf("%.3f", t.QualityScore)
			}
			rows[i] = table.Row{num, t.Title, qs, t.Status}
		}
		m.table.SetColumns(albumDetailColumns())
		m.table.SetRows(rows)
		m.table.GotoTop()
		m.inputFocused = false
		m.searchInput.Blur()
		m.table.Focus()
		return m, m.loadAlbumArt(msg.Album.IAIdentifier, msg.Album.ArtURL)

	case browseSearchMsg:
		if msg.Err != nil || msg.Query != m.lastQuery {
			return m, nil
		}
		m.totalCount = msg.TotalCount
		m.trackResults = msg.Tracks
		m.similarityMode = false
		m.similarResults = nil
		rows := make([]table.Row, len(msg.Tracks))
		for i, t := range msg.Tracks {
			rows[i] = table.Row{t.Title, t.AlbumTitle, fmt.Sprintf("%.3f", t.QualityScore)}
		}
		m.table.SetColumns(trackColumns())
		m.table.SetRows(rows)
		m.table.GotoTop()
		m.loaded = true
		return m, nil

	case browseSimilarMsg:
		if msg.Err != nil {
			return m, nil
		}
		m.similarityMode = true
		m.similarSource = msg.SourceTrackID
		m.similarResults = msg.Tracks
		rows := make([]table.Row, len(msg.Tracks))
		for i, t := range msg.Tracks {
			rows[i] = table.Row{t.Title, t.AlbumID, fmt.Sprintf("%.3f", t.QualityScore), fmt.Sprintf("%.4f", t.Distance)}
		}
		m.table.SetColumns(similarColumns())
		m.table.SetRows(rows)
		m.table.GotoTop()
		return m, nil

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.searchInput.SetWidth(msg.Width - 8)
		tableHeight := msg.Height - 12
		if tableHeight < 5 {
			tableHeight = 5
		}
		m.table.SetHeight(tableHeight)
		m.table.SetWidth(msg.Width - 4)
		return m, nil

	case tea.KeyPressMsg:
		if m.inputFocused {
			return m.handleInputKey(msg)
		}
		return m.handleTableKey(msg)
	}

	return m, nil
}

func (m BrowseModel) handleInputKey(msg tea.KeyPressMsg) (BrowseModel, tea.Cmd) {
	switch msg.String() {
	case "tab":
		m.inputFocused = false
		m.searchInput.Blur()
		m.table.Focus()
		return m, nil
	case "esc":
		m.inputFocused = false
		m.searchInput.Blur()
		m.table.Focus()
		return m, nil
	case "enter":
		m.inputFocused = false
		m.searchInput.Blur()
		m.table.Focus()
		return m, nil
	}

	prevVal := m.searchInput.Value()
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)

	if m.searchInput.Value() != prevVal {
		m.page = 0
		m.lastQuery = m.searchInput.Value()
		return m, tea.Batch(cmd, m.doModeSearch(m.searchInput.Value()))
	}

	return m, cmd
}

func (m BrowseModel) handleTableKey(msg tea.KeyPressMsg) (BrowseModel, tea.Cmd) {
	switch msg.String() {
	case "/":
		if m.mode != ModeAlbumDetail {
			m.inputFocused = true
			m.table.Blur()
			return m, m.searchInput.Focus()
		}
		return m, nil

	case "tab":
		if m.mode != ModeAlbumDetail {
			m.inputFocused = true
			m.table.Blur()
			return m, m.searchInput.Focus()
		}
		return m, nil

	case "m":
		if m.similarityMode {
			return m, nil
		}
		if m.mode == ModeAlbums {
			m.mode = ModeTracks
			m.loaded = false
			m.searchInput.Placeholder = "Search tracks..."
			m.lastQuery = m.searchInput.Value()
			return m, m.doTrackSearch(m.searchInput.Value())
		} else if m.mode == ModeTracks {
			m.mode = ModeAlbums
			m.loaded = false
			m.searchInput.Placeholder = "Search albums..."
			m.lastQuery = m.searchInput.Value()
			return m, m.doAlbumSearch(m.searchInput.Value())
		}
		return m, nil

	case "enter":
		return m.handleEnter()

	case "p":
		return m.handlePlay()

	case "a":
		if m.mode == ModeTracks && !m.similarityMode {
			idx := m.table.Cursor()
			if idx >= 0 && idx < len(m.trackResults) {
				return m, m.doAlbumDetail(m.trackResults[idx].AlbumID)
			}
		}
		return m, nil

	case "v":
		return m.handleSimilar()

	case "esc":
		if m.similarityMode {
			m.similarityMode = false
			m.similarSource = 0
			m.similarResults = nil
			if m.mode == ModeAlbumDetail && m.detailAlbum != nil {
				return m, m.doAlbumDetail(m.detailAlbum.IAIdentifier)
			}
			m.lastQuery = m.searchInput.Value()
			return m, m.doModeSearch(m.searchInput.Value())
		}
		if m.mode == ModeAlbumDetail {
			m.mode = ModeAlbums
			m.inputFocused = true
			m.table.Blur()
			m.lastQuery = m.searchInput.Value()
			m.searchInput.Placeholder = "Search albums..."
			return m, tea.Batch(m.searchInput.Focus(), m.doAlbumSearch(m.searchInput.Value()))
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	if m.mode == ModeAlbums && !m.similarityMode && m.table.Cursor() != m.lastCursor {
		m.lastCursor = m.table.Cursor()
		if m.lastCursor >= 0 && m.lastCursor < len(m.albumResults) {
			a := m.albumResults[m.lastCursor]
			artCmd := m.loadAlbumArt(a.IAIdentifier, a.ArtURL)
			if artCmd != nil {
				return m, tea.Batch(cmd, artCmd)
			}
		}
	}
	return m, cmd
}

func (m BrowseModel) handleEnter() (BrowseModel, tea.Cmd) {
	idx := m.table.Cursor()
	if idx < 0 {
		return m, nil
	}

	if m.mode == ModeAlbums && !m.similarityMode {
		if idx < len(m.albumResults) {
			return m, m.doAlbumDetail(m.albumResults[idx].IAIdentifier)
		}
		return m, nil
	}

	return m.handlePlay()
}

func (m BrowseModel) handlePlay() (BrowseModel, tea.Cmd) {
	idx := m.table.Cursor()
	if idx < 0 {
		return m, nil
	}

	if m.similarityMode && idx < len(m.similarResults) {
		t := m.similarResults[idx]
		return m, func() tea.Msg {
			return SwitchToPlayerMsg{TrackID: t.TrackID, Title: t.Title, AlbumID: t.AlbumID}
		}
	}

	if m.mode == ModeTracks && idx < len(m.trackResults) {
		t := m.trackResults[idx]
		return m, func() tea.Msg {
			return SwitchToPlayerMsg{
				TrackID: t.TrackID, Title: t.Title, AlbumID: t.AlbumID,
				AlbumTitle: t.AlbumTitle, DownloadURL: t.DownloadURL,
			}
		}
	}

	if m.mode == ModeAlbumDetail && idx < len(m.detailTracks) {
		t := m.detailTracks[idx]
		if t.Status != "completed" && t.Status != "pending" {
			return m, nil
		}
		albumTitle := ""
		artURL := ""
		albumID := ""
		if m.detailAlbum != nil {
			albumTitle = m.detailAlbum.Title
			artURL = m.detailAlbum.ArtURL
			albumID = m.detailAlbum.IAIdentifier
		}
		return m, func() tea.Msg {
			return SwitchToPlayerMsg{
				TrackID: t.ID, Title: t.Title, AlbumID: albumID,
				AlbumTitle: albumTitle, ArtURL: artURL, DownloadURL: t.DownloadURL,
			}
		}
	}

	return m, nil
}

func (m BrowseModel) handleSimilar() (BrowseModel, tea.Cmd) {
	if m.similarityMode {
		return m, nil
	}
	idx := m.table.Cursor()
	if idx < 0 {
		return m, nil
	}

	if m.mode == ModeTracks && idx < len(m.trackResults) {
		return m, m.doSimilarity(m.trackResults[idx].TrackID)
	}

	if m.mode == ModeAlbumDetail && idx < len(m.detailTracks) {
		t := m.detailTracks[idx]
		if t.Status == "completed" {
			return m, m.doSimilarity(t.ID)
		}
	}

	return m, nil
}

func (m BrowseModel) doModeSearch(query string) tea.Cmd {
	if m.mode == ModeAlbums {
		return m.doAlbumSearch(query)
	}
	return m.doTrackSearch(query)
}

func (m BrowseModel) doAlbumSearch(query string) tea.Cmd {
	sqlDB := m.DB
	pg := m.page
	return func() tea.Msg {
		albums, total, err := db.SearchAlbums(sqlDB, query, browsePageSize, pg*browsePageSize)
		return browseAlbumSearchMsg{Albums: albums, TotalCount: total, Query: query, Err: err}
	}
}

func (m BrowseModel) doTrackSearch(query string) tea.Cmd {
	sqlDB := m.DB
	pg := m.page
	return func() tea.Msg {
		tracks, total, err := db.SearchCompletedTracks(sqlDB, query, browsePageSize, pg*browsePageSize)
		return browseSearchMsg{Tracks: tracks, TotalCount: total, Query: query, Err: err}
	}
}

func (m BrowseModel) doAlbumDetail(albumID string) tea.Cmd {
	sqlDB := m.DB
	return func() tea.Msg {
		album, err := db.GetAlbumByID(sqlDB, albumID)
		if err != nil {
			return browseAlbumDetailMsg{Err: err}
		}
		tracks, err := db.GetAlbumTracks(sqlDB, albumID)
		if err != nil {
			return browseAlbumDetailMsg{Err: err}
		}
		return browseAlbumDetailMsg{Album: album, Tracks: tracks}
	}
}

func (m BrowseModel) doSimilarity(trackID int) tea.Cmd {
	sqlDB := m.DB
	return func() tea.Msg {
		tracks, err := db.QuerySimilar(sqlDB, trackID, 5)
		return browseSimilarMsg{SourceTrackID: trackID, Tracks: tracks, Err: err}
	}
}

func (m *BrowseModel) loadAlbumArt(identifier, artURL string) tea.Cmd {
	m.currentArtID = identifier
	if m.artCache == nil {
		m.currentArt = ""
		return nil
	}
	cols, rows := ArtColsSmall, ArtRowsSmall
	if m.mode == ModeAlbumDetail {
		cols, rows = ArtColsLarge, ArtRowsLarge
	}
	if enc, ok := m.artCache.GetCached(identifier, cols, rows); ok {
		m.currentArt = enc
		return nil
	}
	m.currentArt = ""
	return m.artCache.LoadArtCmd(identifier, artURL, cols, rows)
}

func (m BrowseModel) Activate() (BrowseModel, tea.Cmd) {
	if !m.loaded {
		m.lastQuery = ""
		m.inputFocused = true
		return m, tea.Batch(m.searchInput.Focus(), m.doModeSearch(""))
	}
	if m.mode != ModeAlbumDetail {
		m.inputFocused = true
		m.table.Blur()
		return m, m.searchInput.Focus()
	}
	m.table.Focus()
	return m, nil
}

func (m BrowseModel) View() tea.View {
	if m.Width == 0 {
		return tea.NewView("loading...")
	}

	var b strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(Primary).MarginBottom(1)
	mutedStyle := lipgloss.NewStyle().Foreground(Muted)

	switch {
	case m.similarityMode:
		b.WriteString(titleStyle.Render("Similar Tracks"))
		b.WriteString("  ")
		b.WriteString(mutedStyle.Render(fmt.Sprintf("to track #%d", m.similarSource)))
	case m.mode == ModeAlbumDetail && m.detailAlbum != nil:
		m.renderAlbumDetailHeader(&b, titleStyle, mutedStyle)
	case m.mode == ModeAlbums:
		modeIndicator := lipgloss.NewStyle().Foreground(Secondary).Bold(true).Render("[Albums]")
		modeAlt := mutedStyle.Render(" Tracks")
		b.WriteString(titleStyle.Render("Browse"))
		b.WriteString("  ")
		b.WriteString(modeIndicator)
		b.WriteString(modeAlt)
		b.WriteString("  ")
		b.WriteString(mutedStyle.Render(fmt.Sprintf("%d albums", m.totalCount)))
	case m.mode == ModeTracks:
		modeAlt := mutedStyle.Render("Albums ")
		modeIndicator := lipgloss.NewStyle().Foreground(Secondary).Bold(true).Render("[Tracks]")
		b.WriteString(titleStyle.Render("Browse"))
		b.WriteString("  ")
		b.WriteString(modeAlt)
		b.WriteString(modeIndicator)
		b.WriteString("  ")
		b.WriteString(mutedStyle.Render(fmt.Sprintf("%d tracks", m.totalCount)))
	}
	b.WriteString("\n\n")

	if m.mode != ModeAlbumDetail || m.similarityMode {
		searchStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).BorderForeground(Muted).Padding(0, 1).Width(m.Width - 4)
		if m.inputFocused {
			searchStyle = searchStyle.BorderForeground(Primary)
		}
		b.WriteString(searchStyle.Render(m.searchInput.View()))
		b.WriteString("\n\n")
	}

	if m.mode == ModeAlbums && !m.similarityMode && len(m.albumResults) > 0 {
		idx := m.table.Cursor()
		if idx < len(m.albumResults) {
			a := m.albumResults[idx]
			artStr := m.getArtDisplay(a.Title, ArtColsSmall, ArtRowsSmall)
			infoStyle := lipgloss.NewStyle().Foreground(TextColor)
			info := infoStyle.Bold(true).Render(a.Title) + "\n"
			info += mutedStyle.Render(a.Creator)
			if a.Collection != "" {
				info += mutedStyle.Render(" \u00b7 " + a.Collection)
			}
			info += mutedStyle.Render(fmt.Sprintf(" \u00b7 %d/%d tracks", a.CompletedCount, a.TrackCount))
			if a.Downloads > 0 {
				info += mutedStyle.Render(fmt.Sprintf(" \u00b7 %s downloads", formatDownloads(a.Downloads)))
			}
			if a.AvgQuality > 0 {
				info += mutedStyle.Render(fmt.Sprintf(" \u00b7 avg quality %.3f", a.AvgQuality))
			}
			preview := lipgloss.JoinHorizontal(lipgloss.Top, artStr, "  ", info)
			b.WriteString(preview)
			b.WriteString("\n\n")
		}
	}

	if !m.loaded && m.mode != ModeAlbumDetail {
		b.WriteString(mutedStyle.Italic(true).Render("  Loading..."))
	} else if len(m.table.Rows()) == 0 {
		b.WriteString(m.emptyMessage(mutedStyle))
	} else {
		b.WriteString(m.table.View())
	}

	b.WriteString("\n")
	b.WriteString(m.buildHints(mutedStyle))

	content := b.String()
	if m.Height > 3 {
		content = lipgloss.Place(m.Width, m.Height-3, lipgloss.Left, lipgloss.Top, content)
	}

	return tea.NewView(content)
}

func (m BrowseModel) renderAlbumDetailHeader(b *strings.Builder, titleStyle, mutedStyle lipgloss.Style) {
	a := m.detailAlbum
	b.WriteString(mutedStyle.Render("\u2190 Back [esc]"))
	b.WriteString("\n\n")

	artStr := m.getArtDisplay(a.Title, ArtColsLarge, ArtRowsLarge)
	infoLines := titleStyle.Render(a.Title) + "\n"
	info := a.Creator
	if a.Collection != "" {
		if info != "" {
			info += " \u00b7 "
		}
		info += a.Collection
	}
	info += fmt.Sprintf(" \u00b7 %d tracks", a.TrackCount)
	if a.CompletedCount > 0 {
		info += fmt.Sprintf(" (%d completed)", a.CompletedCount)
	}
	if a.Downloads > 0 {
		info += fmt.Sprintf(" \u00b7 %s downloads", formatDownloads(a.Downloads))
	}
	if a.AvgQuality > 0 {
		info += fmt.Sprintf(" \u00b7 avg quality %.3f", a.AvgQuality)
	}
	infoLines += mutedStyle.Render(info)

	if artStr != "" {
		combined := lipgloss.JoinHorizontal(lipgloss.Top, artStr, "  ", infoLines)
		b.WriteString(combined)
	} else {
		b.WriteString(infoLines)
	}
	b.WriteString("\n")
}

func (m BrowseModel) getArtDisplay(title string, cols, rows int) string {
	if m.currentArt != "" && m.artCache != nil && m.artCache.IsSupported() {
		if len(m.currentArt) > 0 && m.currentArt[0] == '\x1b' {
			return m.currentArt
		}
	}
	return RenderArtPlaceholder(title, cols, rows)
}

func (m BrowseModel) emptyMessage(style lipgloss.Style) string {
	if m.mode == ModeAlbumDetail {
		return style.Italic(true).Render("  No tracks in this album.")
	}
	if m.searchInput.Value() != "" {
		return style.Italic(true).Render(fmt.Sprintf("  No results matching \"%s\"", m.searchInput.Value()))
	}
	if m.mode == ModeAlbums {
		return style.Italic(true).Render("  No albums resolved yet. Start the coordinator and workers.")
	}
	return style.Italic(true).Render("  No tracks indexed yet. Start the coordinator and workers.")
}

func (m BrowseModel) buildHints(style lipgloss.Style) string {
	if m.inputFocused {
		return style.Render("  [tab/esc] focus table  [enter] focus table")
	}
	if m.similarityMode {
		return style.Render("  [esc] back  [enter/p] play  [\u2191\u2193] navigate")
	}
	switch m.mode {
	case ModeAlbums:
		return style.Render("  [enter] view album  [m] switch to tracks  [tab//] search  [\u2191\u2193] navigate")
	case ModeAlbumDetail:
		return style.Render("  [enter/p] play  [v] similar  [esc] back  [\u2191\u2193] navigate")
	case ModeTracks:
		return style.Render("  [enter/p] play  [a] view album  [v] similar  [m] switch to albums  [tab//] search  [\u2191\u2193] navigate")
	}
	return ""
}

func albumColumns() []table.Column {
	return []table.Column{
		{Title: "Album", Width: 30},
		{Title: "Artist", Width: 18},
		{Title: "Collection", Width: 13},
		{Title: "Tracks", Width: 8},
		{Title: "DLs", Width: 8},
		{Title: "Avg Q", Width: 8},
	}
}

func formatDownloads(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func albumDetailColumns() []table.Column {
	return []table.Column{
		{Title: "#", Width: 4},
		{Title: "Track", Width: 40},
		{Title: "Quality", Width: 10},
		{Title: "Status", Width: 12},
	}
}

func trackColumns() []table.Column {
	return []table.Column{
		{Title: "Track", Width: 30},
		{Title: "Album", Width: 30},
		{Title: "Quality", Width: 10},
	}
}

func similarColumns() []table.Column {
	return []table.Column{
		{Title: "Track", Width: 28},
		{Title: "Album", Width: 24},
		{Title: "Quality", Width: 10},
		{Title: "Distance", Width: 10},
	}
}
