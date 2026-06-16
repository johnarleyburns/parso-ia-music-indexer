package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/johnarleyburns/parso-ia-music-indexer/internal/config"
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
	m := tui.NewMainModel(cfg)
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runHeadless(cfg *config.Config) {
	fmt.Fprintf(os.Stderr, "headless mode — not yet implemented\n")
	fmt.Fprintf(os.Stderr, "db-path: %s\n", cfg.DBPath)
	os.Exit(0)
}
