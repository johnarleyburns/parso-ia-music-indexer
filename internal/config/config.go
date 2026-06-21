package config

import (
	"flag"
	"fmt"
	"os"
)

type Config struct {
	DBPath              string
	Headless            bool
	Workers             int
	MaxBytes            int
	ThrottleBPS         int
	IAApiRate           int
	ClapHost            string
	ClapPort            int
	ClapSidecarDir      string
	SearchText          string
	LibrivoxDenylistPath string
}

func Parse() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.DBPath, "db-path", envOrDefault("DB_PATH", "data/parso_indexer.db"), "SQLite database path")
	flag.BoolVar(&cfg.Headless, "headless", false, "Run in headless mode (JSON logging, no TUI)")
	flag.IntVar(&cfg.Workers, "workers", envOrDefaultInt("WORKER_CONCURRENCY", 2), "Number of worker goroutines")
	flag.IntVar(&cfg.MaxBytes, "max-stream-bytes", envOrDefaultInt("MAX_STREAM_BYTES", 1600000), "Max bytes to stream per track (~30s MP3)")
	flag.IntVar(&cfg.ThrottleBPS, "throttle-bps", envOrDefaultInt("THROTTLE_BPS", 460800), "Download throttle in bytes/sec (450 KB/s)")
	flag.IntVar(&cfg.IAApiRate, "ia-api-rate", envOrDefaultInt("IA_API_RATE", 6), "IA API requests per minute")
	flag.StringVar(&cfg.ClapHost, "clap-host", envOrDefault("CLAP_HOST", "localhost"), "CLAP gRPC server host")
	flag.IntVar(&cfg.ClapPort, "clap-port", envOrDefaultInt("CLAP_PORT", 50051), "CLAP gRPC server port")
	flag.StringVar(&cfg.ClapSidecarDir, "clap-sidecar-dir", envOrDefault("CLAP_SIDECAR_DIR", "python_sidecar"), "Path to CLAP Python sidecar directory")
	flag.StringVar(&cfg.SearchText, "search-text", "", "Search indexed tracks by text query (CLAP text-to-audio)")
	flag.StringVar(&cfg.LibrivoxDenylistPath, "librivox-denylist", envOrDefault("LIBRIVOX_DENYLIST", "data/librivox_denylist.json"), "Path to LibriVox denylist JSON file")
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
