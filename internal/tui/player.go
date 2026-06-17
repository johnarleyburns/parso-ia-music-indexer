package tui

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type PlayState int

const (
	StateStopped PlayState = iota
	StateLoading
	StatePlaying
	StatePaused
)

type QueueItem struct {
	TrackID     int
	Title       string
	AlbumID     string
	AlbumTitle  string
	ArtURL      string
	DownloadURL string
}

type playerLoadedMsg struct {
	data []byte
}

type playerErrorMsg struct {
	err error
}

type playerTickMsg time.Time

type playerDoneMsg struct{}

type PlayerModel struct {
	engine      *PlayerEngine
	queue       []QueueItem
	currentIdx  int
	state       PlayState
	volumeLevel float64
	elapsed     time.Duration
	total       time.Duration
	errMsg      string
	doneCh      chan struct{}
	DB          *sql.DB
	Width       int
	Height      int
}

func NewPlayerModel(sqlDB *sql.DB) PlayerModel {
	return PlayerModel{
		engine:      NewPlayerEngine(),
		volumeLevel: 0.8,
		DB:          sqlDB,
	}
}

func (m PlayerModel) Init() tea.Cmd {
	return nil
}

func (m PlayerModel) Update(msg tea.Msg) (PlayerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case SwitchToPlayerMsg:
		item := QueueItem{
			TrackID:     msg.TrackID,
			Title:       msg.Title,
			AlbumID:     msg.AlbumID,
			AlbumTitle:  msg.AlbumTitle,
			ArtURL:      msg.ArtURL,
			DownloadURL: msg.DownloadURL,
		}
		m.queue = append(m.queue, item)
		m.errMsg = ""
		if m.state == StateStopped {
			m.currentIdx = len(m.queue) - 1
			m.state = StateLoading
			return m, m.loadCurrentTrackCmd()
		}
		return m, nil

	case playerLoadedMsg:
		m.doneCh = make(chan struct{})
		doneCh := m.doneCh
		err := m.engine.LoadAndPlay(msg.data, func() {
			close(doneCh)
		})
		if err != nil {
			m.state = StateStopped
			m.errMsg = err.Error()
			return m, nil
		}
		m.state = StatePlaying
		m.engine.SetVolume(m.volumeLevel)
		m.errMsg = ""
		return m, tea.Batch(playerTickCmd(), waitForTrackDone(doneCh))

	case playerErrorMsg:
		m.errMsg = msg.err.Error()
		if m.currentIdx+1 < len(m.queue) {
			m.currentIdx++
			m.state = StateLoading
			return m, m.loadCurrentTrackCmd()
		}
		m.state = StateStopped
		return m, nil

	case playerTickMsg:
		if m.state == StatePlaying || m.state == StatePaused {
			m.elapsed, m.total = m.engine.Position()
			if m.state == StatePlaying {
				return m, playerTickCmd()
			}
		}
		return m, nil

	case playerDoneMsg:
		if m.currentIdx+1 < len(m.queue) {
			m.currentIdx++
			m.state = StateLoading
			m.elapsed = 0
			m.total = 0
			return m, m.loadCurrentTrackCmd()
		}
		m.state = StateStopped
		m.elapsed = 0
		m.total = 0
		return m, nil

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m PlayerModel) handleKey(msg tea.KeyPressMsg) (PlayerModel, tea.Cmd) {
	switch msg.String() {
	case " ":
		if m.state == StatePlaying {
			m.engine.TogglePause()
			m.state = StatePaused
		} else if m.state == StatePaused {
			m.engine.TogglePause()
			m.state = StatePlaying
			return m, playerTickCmd()
		}
		return m, nil

	case "s":
		if m.state == StatePlaying || m.state == StatePaused {
			m.engine.Stop()
			m.state = StateStopped
			m.elapsed = 0
			m.total = 0
		}
		return m, nil

	case "n":
		if m.currentIdx+1 < len(m.queue) {
			m.engine.Stop()
			m.currentIdx++
			m.state = StateLoading
			m.elapsed = 0
			m.total = 0
			return m, m.loadCurrentTrackCmd()
		}
		return m, nil

	case "left":
		if m.state == StatePlaying || m.state == StatePaused {
			m.engine.Seek(-5 * time.Second)
			m.elapsed, m.total = m.engine.Position()
		}
		return m, nil

	case "right":
		if m.state == StatePlaying || m.state == StatePaused {
			m.engine.Seek(5 * time.Second)
			m.elapsed, m.total = m.engine.Position()
		}
		return m, nil

	case "up", "+", "=":
		m.volumeLevel += 0.1
		if m.volumeLevel > 1.0 {
			m.volumeLevel = 1.0
		}
		m.engine.SetVolume(m.volumeLevel)
		return m, nil

	case "down", "-":
		m.volumeLevel -= 0.1
		if m.volumeLevel < 0.0 {
			m.volumeLevel = 0.0
		}
		m.engine.SetVolume(m.volumeLevel)
		return m, nil

	case "c":
		if m.state == StateStopped {
			m.queue = nil
			m.currentIdx = 0
		} else if m.currentIdx < len(m.queue) {
			m.queue = m.queue[:m.currentIdx+1]
		}
		return m, nil
	}

	return m, nil
}

func (m PlayerModel) loadCurrentTrackCmd() tea.Cmd {
	if m.currentIdx >= len(m.queue) {
		return nil
	}
	item := m.queue[m.currentIdx]
	downloadURL := item.DownloadURL
	sqlDB := m.DB
	trackID := item.TrackID
	return func() tea.Msg {
		if downloadURL == "" && sqlDB != nil {
			var url sql.NullString
			err := sqlDB.QueryRow("SELECT download_url FROM tracks WHERE id = ?", trackID).Scan(&url)
			if err != nil || !url.Valid || url.String == "" {
				return playerErrorMsg{err: fmt.Errorf("no download URL for track %d", trackID)}
			}
			downloadURL = url.String
		}
		if downloadURL == "" {
			return playerErrorMsg{err: fmt.Errorf("no download URL available")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
		if err != nil {
			return playerErrorMsg{err: fmt.Errorf("build request: %w", err)}
		}
		req.Header.Set("User-Agent", "ParsoIAIndexer/1.0")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return playerErrorMsg{err: fmt.Errorf("download: %w", err)}
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			return playerErrorMsg{err: fmt.Errorf("download: status %d", resp.StatusCode)}
		}

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return playerErrorMsg{err: fmt.Errorf("read body: %w", err)}
		}
		if len(data) == 0 {
			return playerErrorMsg{err: fmt.Errorf("empty response")}
		}

		return playerLoadedMsg{data: data}
	}
}

func playerTickCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return playerTickMsg(t)
	})
}

func waitForTrackDone(ch <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-ch
		return playerDoneMsg{}
	}
}

func (m PlayerModel) View() tea.View {
	if m.Width == 0 {
		return tea.NewView("loading...")
	}

	var b strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(Primary).MarginBottom(1)
	mutedStyle := lipgloss.NewStyle().Foreground(Muted)
	textStyle := lipgloss.NewStyle().Foreground(TextColor)

	b.WriteString(titleStyle.Render("Player"))
	b.WriteString("\n\n")

	if len(m.queue) == 0 {
		b.WriteString(mutedStyle.Italic(true).Render("  No tracks in queue. Browse and select tracks to play."))
		b.WriteString("\n")
	} else {
		current := m.queue[m.currentIdx]

		stateIcon := "\u25aa"
		stateStyle := StatusStoppedStyle
		switch m.state {
		case StatePlaying:
			stateIcon = "\u25b6"
			stateStyle = StatusRunningStyle
		case StatePaused:
			stateIcon = "\u23f8"
			stateStyle = lipgloss.NewStyle().Foreground(Warning).Bold(true)
		case StateLoading:
			stateIcon = "\u23f3"
			stateStyle = lipgloss.NewStyle().Foreground(Secondary).Bold(true)
		}

		b.WriteString(PanelTitleStyle.Render("Now Playing"))
		b.WriteString("\n\n")
		b.WriteString("  ")
		b.WriteString(stateStyle.Render(stateIcon))
		b.WriteString("  ")
		b.WriteString(textStyle.Bold(true).Render(current.Title))
		b.WriteString("\n")

		if current.AlbumTitle != "" {
			b.WriteString("     ")
			b.WriteString(mutedStyle.Render(current.AlbumTitle))
			if current.AlbumID != "" {
				b.WriteString(mutedStyle.Render(" \u00b7 " + current.AlbumID))
			}
			b.WriteString("\n")
		} else if current.AlbumID != "" {
			b.WriteString("     ")
			b.WriteString(mutedStyle.Render(current.AlbumID))
			b.WriteString("\n")
		}
		b.WriteString("\n")

		if m.state == StatePlaying || m.state == StatePaused {
			barWidth := m.Width - 30
			if barWidth < 10 {
				barWidth = 10
			}
			if barWidth > 60 {
				barWidth = 60
			}
			progress := 0.0
			if m.total > 0 {
				progress = float64(m.elapsed) / float64(m.total)
			}
			if progress > 1.0 {
				progress = 1.0
			}
			filled := int(progress * float64(barWidth))
			if filled > barWidth {
				filled = barWidth
			}

			progressStyle := lipgloss.NewStyle().Foreground(Primary)
			dimBarStyle := lipgloss.NewStyle().Foreground(Muted)

			b.WriteString("  ")
			b.WriteString(progressStyle.Render(strings.Repeat("\u2588", filled)))
			b.WriteString(dimBarStyle.Render(strings.Repeat("\u2591", barWidth-filled)))
			b.WriteString("  ")
			b.WriteString(mutedStyle.Render(fmt.Sprintf("%s / %s", formatDuration(m.elapsed), formatDuration(m.total))))
			b.WriteString("\n")
		}

		if m.state == StateLoading {
			b.WriteString("  ")
			b.WriteString(lipgloss.NewStyle().Foreground(Secondary).Render("Downloading..."))
			b.WriteString("\n")
		}

		volWidth := 10
		volFilled := int(m.volumeLevel * float64(volWidth))
		if volFilled > volWidth {
			volFilled = volWidth
		}
		b.WriteString("  ")
		b.WriteString(mutedStyle.Render("Volume: "))
		b.WriteString(lipgloss.NewStyle().Foreground(Secondary).Render(strings.Repeat("\u2588", volFilled)))
		b.WriteString(lipgloss.NewStyle().Foreground(Muted).Render(strings.Repeat("\u2591", volWidth-volFilled)))
		b.WriteString(" ")
		b.WriteString(mutedStyle.Render(fmt.Sprintf("%d%%", int(m.volumeLevel*100))))
		b.WriteString("\n\n")

		if m.errMsg != "" {
			b.WriteString("  ")
			b.WriteString(lipgloss.NewStyle().Foreground(Danger).Render("Error: " + m.errMsg))
			b.WriteString("\n\n")
		}

		if len(m.queue) > 1 {
			b.WriteString(PanelTitleStyle.Render("Queue"))
			b.WriteString("\n\n")
			for i, item := range m.queue {
				prefix := "   "
				style := mutedStyle
				if i == m.currentIdx {
					prefix = " \u25b6 "
					style = textStyle.Bold(true)
				} else if i < m.currentIdx {
					style = lipgloss.NewStyle().Foreground(Muted).Strikethrough(true)
				}
				num := fmt.Sprintf("%d. ", i+1)
				b.WriteString(prefix)
				b.WriteString(style.Render(num + truncateStr(item.Title, 50)))
				if item.AlbumTitle != "" {
					b.WriteString(mutedStyle.Render(" \u2014 " + truncateStr(item.AlbumTitle, 30)))
				}
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
	}

	hints := "  [space] play/pause  [s] stop  [n] next  [\u2190/\u2192] seek \u00b15s  [\u2191/\u2193] volume  [c] clear"
	b.WriteString(mutedStyle.Render(hints))

	content := b.String()
	if m.Height > 3 {
		content = lipgloss.Place(m.Width, m.Height-3, lipgloss.Left, lipgloss.Top, content)
	}

	return tea.NewView(content)
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d", m, s)
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
