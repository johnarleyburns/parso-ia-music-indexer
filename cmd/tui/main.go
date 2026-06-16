package main

import (
	"encoding/json"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/johnarleyburns/parso-ia-music-indexer/internal/config"
	"github.com/johnarleyburns/parso-ia-music-indexer/internal/db"
	"github.com/johnarleyburns/parso-ia-music-indexer/internal/tui"
)

func main() {
	cfg := config.Parse()

	if cfg.Headless {
		runHeadless(cfg)
		return
	}

	runTUI(cfg)
}

func runTUI(cfg *config.Config) {
	sqlDB, err := db.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	m := tui.NewMainModel(cfg, sqlDB.Conn)
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runHeadless(cfg *config.Config) {
	sqlDB, err := db.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	stats, err := db.GetStats(sqlDB.Conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error getting stats: %v\n", err)
		os.Exit(1)
	}

	embedCount, err := db.GetEmbeddingCount(sqlDB.Conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error getting embedding count: %v\n", err)
		os.Exit(1)
	}

	result := map[string]interface{}{
		"event":    "db_stats",
		"mode":     "headless",
		"db_path":  cfg.DBPath,
		"catalog_queue": map[string]int{
			"total":      stats.Total,
			"pending":    stats.Pending,
			"processing": stats.Processing,
			"completed":  stats.Completed,
			"failed":     stats.Failed,
		},
		"embeddings": map[string]int{
			"count": embedCount,
		},
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "error encoding stats: %v\n", err)
		os.Exit(1)
	}
}
