package tui

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/BourgeoisBear/rasterm"
	"golang.org/x/image/draw"
)

type artLoadedMsg struct {
	Identifier string
	Encoded    string
	Cols       int
	Rows       int
}

type ArtCache struct {
	mu       sync.Mutex
	dir      string
	encoded  map[string]string
	protocol string
}

func NewArtCache(cacheDir string) *ArtCache {
	os.MkdirAll(cacheDir, 0o755)
	return &ArtCache{
		dir:      cacheDir,
		encoded:  make(map[string]string),
		protocol: detectProtocol(),
	}
}

func detectProtocol() string {
	if os.Getenv("TIMBRE_NO_ART") != "" {
		return "none"
	}

	term := os.Getenv("TERM")
	termProgram := os.Getenv("TERM_PROGRAM")

	inTmux := os.Getenv("TMUX") != ""
	inScreen := os.Getenv("STY") != ""

	if term == "xterm-kitty" && !inTmux && !inScreen {
		return "kitty"
	}
	if (termProgram == "iTerm.app" || termProgram == "WezTerm" ||
		os.Getenv("GHOSTTY_RESOURCES_DIR") != "" || termProgram == "ghostty") && !inScreen {
		return "iterm"
	}
	return "none"
}

func (c *ArtCache) IsSupported() bool {
	return c.protocol != "none"
}

func (c *ArtCache) GetCached(identifier string, cols, rows int) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := fmt.Sprintf("%s:%dx%d", identifier, cols, rows)
	enc, ok := c.encoded[key]
	return enc, ok
}

func (c *ArtCache) LoadArtCmd(identifier, artURL string, cols, rows int) tea.Cmd {
	return func() tea.Msg {
		encoded := c.loadAndEncode(identifier, artURL, cols, rows)
		return artLoadedMsg{Identifier: identifier, Encoded: encoded, Cols: cols, Rows: rows}
	}
}

func (c *ArtCache) StoreEncoded(identifier string, cols, rows int, encoded string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := fmt.Sprintf("%s:%dx%d", identifier, cols, rows)
	c.encoded[key] = encoded
}

func (c *ArtCache) loadAndEncode(identifier, artURL string, cols, rows int) string {
	img := c.loadImage(identifier, artURL)
	if img == nil {
		return ""
	}

	pixW := cols * 8
	pixH := rows * 16
	resized := resizeImage(img, pixW, pixH)

	return c.encodeImage(resized, cols, rows)
}

func (c *ArtCache) loadImage(identifier, artURL string) image.Image {
	path := filepath.Join(c.dir, identifier+".jpg")

	if f, err := os.Open(path); err == nil {
		defer f.Close()
		img, _, err := image.Decode(f)
		if err == nil {
			return img
		}
	}

	if artURL == "" {
		return nil
	}

	resp, err := http.Get(artURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return nil
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil || len(data) == 0 {
		return nil
	}

	os.WriteFile(path, data, 0o644)

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return img
}

func resizeImage(src image.Image, width, height int) image.Image {
	if width <= 0 || height <= 0 {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

func (c *ArtCache) encodeImage(img image.Image, cols, rows int) string {
	var buf bytes.Buffer

	switch c.protocol {
	case "kitty":
		opts := rasterm.KittyImgOpts{
			DstCols: uint32(cols),
			DstRows: uint32(rows),
		}
		if err := rasterm.KittyWriteImage(&buf, img, opts); err != nil {
			return ""
		}
	case "iterm":
		if err := rasterm.ItermWriteImage(&buf, img); err != nil {
			return ""
		}
	default:
		return ""
	}

	encoded := buf.String()
	if len(encoded) == 0 || !strings.Contains(encoded, "\x1b") {
		return ""
	}

	return encoded
}

func RenderArtPlaceholder(title string, cols, rows int) string {
	if cols <= 0 || rows <= 0 {
		return ""
	}
	ch := " "
	if len(title) > 0 {
		ch = strings.ToUpper(title[:1])
	}

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Muted).
		Width(cols).
		Height(rows).
		Align(lipgloss.Center, lipgloss.Center).
		Foreground(Primary).
		Bold(true)

	return borderStyle.Render(ch)
}

const (
	ArtColsSmall  = 8
	ArtRowsSmall  = 4
	ArtColsLarge  = 12
	ArtRowsLarge  = 6
)
