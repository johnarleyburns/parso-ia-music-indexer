package tui

import (
	"image"
	"image/color"
	"net/http"
	"strings"
	"testing"
)

const testIAIdentifier = "cd_kind-of-blue_miles-davis"
const testArtURL = "https://archive.org/services/img/" + testIAIdentifier

func testImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 4), B: 128, A: 255})
		}
	}
	return img
}

func TestArtFetchAndDecode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test")
	}

	resp, err := http.Get(testArtURL)
	if err != nil {
		t.Fatalf("fetch art URL: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	img, format, err := image.Decode(resp.Body)
	if err != nil {
		t.Fatalf("decode image: %v", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		t.Fatalf("image has zero dimensions: %dx%d", bounds.Dx(), bounds.Dy())
	}

	t.Logf("fetched %s image: %dx%d", format, bounds.Dx(), bounds.Dy())
}

func TestResizeImage(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			src.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}

	resized := resizeImage(src, 50, 30)
	bounds := resized.Bounds()
	if bounds.Dx() != 50 || bounds.Dy() != 30 {
		t.Errorf("expected 50x30, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestResizeImageZeroDimensions(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 100))
	resized := resizeImage(src, 0, 0)
	if resized != src {
		t.Error("expected original image returned for zero dimensions")
	}
}

func TestEncodeImageKitty(t *testing.T) {
	cache := &ArtCache{protocol: "kitty", encoded: make(map[string]string)}
	img := testImage()
	encoded := cache.encodeImage(img, 8, 4)
	if encoded == "" {
		t.Fatal("kitty encoding produced empty output")
	}
	if encoded[0] != '\x1b' {
		t.Errorf("expected ESC prefix, got byte %#x", encoded[0])
	}
	t.Logf("kitty encoded: %d bytes", len(encoded))
}

func TestEncodeImageIterm(t *testing.T) {
	cache := &ArtCache{protocol: "iterm", encoded: make(map[string]string)}
	img := testImage()
	encoded := cache.encodeImage(img, 8, 4)
	if encoded == "" {
		t.Fatal("iterm encoding produced empty output")
	}
	if encoded[0] != '\x1b' {
		t.Errorf("expected ESC prefix, got byte %#x", encoded[0])
	}
	t.Logf("iterm encoded: %d bytes", len(encoded))
}

func TestEncodeImageProtocolNone(t *testing.T) {
	cache := &ArtCache{protocol: "none", encoded: make(map[string]string)}
	img := testImage()
	encoded := cache.encodeImage(img, 8, 4)
	if encoded != "" {
		t.Error("expected empty output for protocol 'none'")
	}
}

func TestFullArtPipelineKitty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test")
	}

	dir := t.TempDir()
	cache := &ArtCache{
		dir:      dir,
		encoded:  make(map[string]string),
		protocol: "kitty",
	}

	encoded := cache.loadAndEncode(testIAIdentifier, testArtURL, ArtColsLarge, ArtRowsLarge)
	if encoded == "" {
		t.Fatal("full pipeline produced empty output for kitty protocol")
	}
	if encoded[0] != '\x1b' {
		t.Fatal("encoded output missing ESC sequence")
	}
	t.Logf("full pipeline kitty: %d bytes", len(encoded))
}

func TestFullArtPipelineIterm(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test")
	}

	dir := t.TempDir()
	cache := &ArtCache{
		dir:      dir,
		encoded:  make(map[string]string),
		protocol: "iterm",
	}

	encoded := cache.loadAndEncode(testIAIdentifier, testArtURL, ArtColsLarge, ArtRowsLarge)
	if encoded == "" {
		t.Fatal("full pipeline produced empty output for iterm protocol")
	}
	if encoded[0] != '\x1b' {
		t.Fatal("encoded output missing ESC sequence")
	}
	t.Logf("full pipeline iterm: %d bytes", len(encoded))
}

func TestArtCacheDiskPersistence(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test")
	}

	dir := t.TempDir()

	cache1 := &ArtCache{dir: dir, encoded: make(map[string]string), protocol: "kitty"}
	img1 := cache1.loadImage(testIAIdentifier, testArtURL)
	if img1 == nil {
		t.Fatal("first load (network) returned nil")
	}

	cache2 := &ArtCache{dir: dir, encoded: make(map[string]string), protocol: "kitty"}
	img2 := cache2.loadImage(testIAIdentifier, "")
	if img2 == nil {
		t.Fatal("second load (disk cache) returned nil — disk persistence broken")
	}

	b1 := img1.Bounds()
	b2 := img2.Bounds()
	if b1.Dx() != b2.Dx() || b1.Dy() != b2.Dy() {
		t.Errorf("cached image dimensions differ: %dx%d vs %dx%d",
			b1.Dx(), b1.Dy(), b2.Dx(), b2.Dy())
	}
}

func TestBrowseGetArtDisplayReturnsEncodedArt(t *testing.T) {
	cache := &ArtCache{protocol: "kitty", encoded: make(map[string]string)}
	m := BrowseModel{
		artCache:   cache,
		currentArt: "\x1b_Gfake_kitty_data\x1b\\",
	}

	display := m.getArtDisplay("Test Album", ArtColsSmall, ArtRowsSmall)
	if len(display) == 0 || display[0] != '\x1b' {
		t.Error("expected encoded art returned, got placeholder")
	}
}

func TestBrowseGetArtDisplayFallsBackToPlaceholder(t *testing.T) {
	cache := &ArtCache{protocol: "none", encoded: make(map[string]string)}
	m := BrowseModel{
		artCache:   cache,
		currentArt: "",
	}

	display := m.getArtDisplay("Test Album", ArtColsSmall, ArtRowsSmall)
	if len(display) == 0 {
		t.Error("expected placeholder art, got empty string")
	}
	if strings.Contains(display, "_G") || strings.Contains(display, "]1337;") {
		t.Error("should not contain kitty/iterm protocol sequences when protocol is none")
	}
}

func TestPlayerGetArtDisplayReturnsEncodedArt(t *testing.T) {
	cache := &ArtCache{protocol: "kitty", encoded: make(map[string]string)}
	m := PlayerModel{
		artCache:   cache,
		currentArt: "\x1b_Gfake_kitty_data\x1b\\",
	}

	display := m.getPlayerArtDisplay("Test Track")
	if len(display) == 0 || display[0] != '\x1b' {
		t.Error("expected encoded art returned in player, got placeholder")
	}
}

func TestPlayerGetArtDisplayFallsBackToPlaceholder(t *testing.T) {
	cache := &ArtCache{protocol: "none", encoded: make(map[string]string)}
	m := PlayerModel{
		artCache:   cache,
		currentArt: "",
	}

	display := m.getPlayerArtDisplay("Test Track")
	if len(display) == 0 {
		t.Error("expected placeholder art in player, got empty string")
	}
	if strings.Contains(display, "_G") || strings.Contains(display, "]1337;") {
		t.Error("should not contain kitty/iterm protocol sequences when protocol is none")
	}
}

func TestRenderArtPlaceholder(t *testing.T) {
	placeholder := RenderArtPlaceholder("Test Title", 8, 4)
	if placeholder == "" {
		t.Fatal("placeholder should not be empty for valid dimensions")
	}

	empty := RenderArtPlaceholder("Test", 0, 0)
	if empty != "" {
		t.Error("placeholder should be empty for zero dimensions")
	}
}

func TestDetectProtocolRespectsNoArtEnv(t *testing.T) {
	t.Setenv("TIMBRE_NO_ART", "1")
	protocol := detectProtocol()
	if protocol != "none" {
		t.Errorf("expected 'none' with TIMBRE_NO_ART set, got %q", protocol)
	}
}

func TestArtCacheInMemoryCaching(t *testing.T) {
	cache := &ArtCache{protocol: "kitty", encoded: make(map[string]string)}

	_, ok := cache.GetCached("test-album", 8, 4)
	if ok {
		t.Error("should not have cached entry before store")
	}

	cache.StoreEncoded("test-album", 8, 4, "\x1b_Gencoded\x1b\\")
	enc, ok := cache.GetCached("test-album", 8, 4)
	if !ok {
		t.Fatal("expected cached entry after store")
	}
	if enc != "\x1b_Gencoded\x1b\\" {
		t.Errorf("cached value mismatch: %q", enc)
	}

	_, ok = cache.GetCached("test-album", 12, 6)
	if ok {
		t.Error("different dimensions should not match cached entry")
	}
}
