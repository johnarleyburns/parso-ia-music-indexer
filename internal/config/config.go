package config

import (
	"flag"
	"fmt"
	"os"
)

type Config struct {
	DBPath                  string
	Headless                bool
	Workers                 int
	MaxBytes                int
	ThrottleBPS             int
	IAApiRate               int
	ClapHost                string
	ClapPort                int
	ClapSidecarDir          string
	SearchText              string
	LibrivoxDenylistPath    string
	SeedCollections         bool
	IAParent                string
	SampleStrategy          string
	SampleSkipSeconds       int
	ListenabilityMinTrackSecs int
	ListenabilityCleanerAction string
	FreeOnly                bool
	Pills                   bool
	PillID                  string
}

func Parse() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.DBPath, "db-path", envOrDefault("DB_PATH", "data/parso_indexer.db"), "SQLite database path")
	flag.BoolVar(&cfg.Headless, "headless", false, "Run in headless mode (JSON logging, no TUI)")
	flag.IntVar(&cfg.Workers, "workers", envOrDefaultInt("WORKER_CONCURRENCY", 2), "Number of worker goroutines")
	flag.IntVar(&cfg.MaxBytes, "max-stream-bytes", envOrDefaultInt("MAX_STREAM_BYTES", 1600000), "Max bytes to stream per track (~30s MP3)")
	flag.IntVar(&cfg.ThrottleBPS, "throttle-bps", envOrDefaultInt("THROTTLE_BPS", 921600), "Download throttle in bytes/sec (900 KB/s)")
	flag.IntVar(&cfg.IAApiRate, "ia-api-rate", envOrDefaultInt("IA_API_RATE", 30), "IA API requests per minute")
	flag.StringVar(&cfg.ClapHost, "clap-host", envOrDefault("CLAP_HOST", "localhost"), "CLAP gRPC server host")
	flag.IntVar(&cfg.ClapPort, "clap-port", envOrDefaultInt("CLAP_PORT", 50051), "CLAP gRPC server port")
	flag.StringVar(&cfg.ClapSidecarDir, "clap-sidecar-dir", envOrDefault("CLAP_SIDECAR_DIR", "python_sidecar"), "Path to CLAP Python sidecar directory")
	flag.StringVar(&cfg.SearchText, "search-text", "", "Search indexed tracks by text query (CLAP text-to-audio)")
	flag.StringVar(&cfg.LibrivoxDenylistPath, "librivox-denylist", envOrDefault("LIBRIVOX_DENYLIST", "data/librivox_denylist.json"), "Path to LibriVox denylist JSON file")
	flag.BoolVar(&cfg.SeedCollections, "seed-collections", false, "Seed all collections from embedded JSON (INSERT OR IGNORE)")
	flag.StringVar(&cfg.IAParent, "parent", envOrDefault("IA_PARENT", ""), "IA parent collection for playlists (default: fav-{username} from ia.ini)")
	flag.StringVar(&cfg.SampleStrategy, "sample-strategy", envOrDefault("SAMPLE_STRATEGY", "midpoint"), "Audio sampling strategy: head, midpoint, or multiwindow")
	flag.IntVar(&cfg.SampleSkipSeconds, "sample-skip-seconds", envOrDefaultInt("SAMPLE_SKIP_SECONDS", 20), "Seconds to skip from start before sampling (midpoint/multiwindow)")
	flag.IntVar(&cfg.ListenabilityMinTrackSecs, "listenability-min-track-seconds", envOrDefaultInt("LISTENABILITY_MIN_TRACK_SECS", 60), "Minimum track duration in seconds for listenability scoring")
	flag.StringVar(&cfg.ListenabilityCleanerAction, "listenability-cleaner-action", envOrDefault("LISTENABILITY_CLEANER_ACTION", "score-only"), "Cleaner action: score-only or mark-unavailable")
	flag.BoolVar(&cfg.FreeOnly, "free-only", envOrDefaultBool("FREE_ONLY", true), "Index only commercially-usable licenses (pd, cc0, cc-by, cc-by-sa); mark others unavailable")
	flag.BoolVar(&cfg.Pills, "pills", false, "List active genre pills (those with enough listenable music) and exit")
	flag.StringVar(&cfg.PillID, "pill", "", "Render a similar-music feed for the given pill id (e.g. ambient-drone)")
	flag.Parse()

	return cfg
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrDefaultInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return def
}

func envOrDefaultBool(key string, def bool) bool {
	switch os.Getenv(key) {
	case "1", "true", "TRUE", "True", "yes", "on":
		return true
	case "0", "false", "FALSE", "False", "no", "off":
		return false
	default:
		return def
	}
}
