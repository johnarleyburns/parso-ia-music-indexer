package tui

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/johnarleyburns/parso-ia-music-indexer/internal/db"

	"charm.land/lipgloss/v2"
)

type ResourceStats struct {
	MemAllocMB float64
	DBSizeMB   float64
}

func ComputeResourceStats(dbPath string) ResourceStats {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	var rs ResourceStats
	rs.MemAllocMB = float64(ms.Alloc) / (1024 * 1024)

	var dbTotal int64
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if info, err := os.Stat(dbPath + suffix); err == nil {
			dbTotal += info.Size()
		}
	}
	rs.DBSizeMB = float64(dbTotal) / (1024 * 1024)

	return rs
}

func formatETA(d time.Duration) string {
	if d < 0 {
		return "\u2014"
	}
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60

	if h > 99 {
		return fmt.Sprintf("%dd %dh", h/24, h%24)
	}
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func pctColor(pct float64) lipgloss.Style {
	switch {
	case pct >= 80:
		return lipgloss.NewStyle().Foreground(Danger).Bold(true)
	case pct >= 50:
		return lipgloss.NewStyle().Foreground(Warning).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(Success)
	}
}

func RenderStatusBar(metrics *Metrics, stats *db.CombinedStats, rs ResourceStats, width int) string {
	if stats == nil {
		stats = &db.CombinedStats{}
	}

	sep := lipgloss.NewStyle().Foreground(Muted).Render(" \u2502 ")
	labelStyle := lipgloss.NewStyle().Foreground(Secondary)
	valStyle := lipgloss.NewStyle().Foreground(TextColor)
	mutedVal := lipgloss.NewStyle().Foreground(Muted)

	bw := metrics.Bandwidth()
	bwKBs := bw / 1024
	bwPct := (bw / IABandwidthLimit) * 100
	if bwPct > 100 {
		bwPct = 100
	}

	apiRate := metrics.APIRate()
	apiPct := (apiRate / IAAPIRateLimit) * 100
	if apiPct > 100 {
		apiPct = 100
	}

	resolverRate := metrics.ResolverRate()
	analyzerRate := metrics.AnalyzerRate()
	cleanerRate := metrics.CleanerRate()
	enhancerRate := metrics.EnhancerRate()

	var resolverETA string
	if resolverRate > 0 && stats.Albums.Pending > 0 {
		secs := float64(stats.Albums.Pending+stats.Albums.Resolving) / resolverRate
		resolverETA = formatETA(time.Duration(secs) * time.Second)
	} else if stats.Albums.Pending == 0 && stats.Albums.Resolving == 0 {
		resolverETA = "done"
	} else {
		resolverETA = "\u2014"
	}

	var analyzerETA string
	if analyzerRate > 0 && stats.Tracks.Pending > 0 {
		secs := float64(stats.Tracks.Pending+stats.Tracks.Processing) / analyzerRate
		analyzerETA = formatETA(time.Duration(secs) * time.Second)
	} else if stats.Tracks.Pending == 0 && stats.Tracks.Processing == 0 {
		analyzerETA = "done"
	} else {
		analyzerETA = "\u2014"
	}

	var cleanerETA string
	if cleanerRate > 0 && stats.Albums.Unprechecked > 0 {
		secs := float64(stats.Albums.Unprechecked) / cleanerRate
		cleanerETA = formatETA(time.Duration(secs) * time.Second)
	} else if stats.Albums.Unprechecked == 0 {
		cleanerETA = "done"
	} else {
		cleanerETA = "\u2014"
	}

	var enhancerETA string
	if enhancerRate > 0 && stats.Tracks.UntaggedCount > 0 {
		secs := float64(stats.Tracks.UntaggedCount) / enhancerRate
		enhancerETA = formatETA(time.Duration(secs) * time.Second)
	} else if stats.Tracks.UntaggedCount == 0 {
		enhancerETA = "done"
	} else {
		enhancerETA = "\u2014"
	}

	bwVal := pctColor(bwPct).Render(fmt.Sprintf("%.1f KB/s (%.0f%%)", bwKBs, bwPct))
	apiVal := pctColor(apiPct).Render(fmt.Sprintf("%.2f req/s (%.0f%%)", apiRate, apiPct))

	line1Parts := []string{
		labelStyle.Render("IA:") + " " + bwVal,
		apiVal,
		labelStyle.Render("Resolvers:") + " " + valStyle.Render(resolverETA),
		labelStyle.Render("Analyzers:") + " " + valStyle.Render(analyzerETA),
		labelStyle.Render("Cleaners:") + " " + valStyle.Render(cleanerETA),
		labelStyle.Render("Enhancers:") + " " + valStyle.Render(enhancerETA),
	}

	line2Parts := []string{
		labelStyle.Render("Mem:") + " " + mutedVal.Render(fmt.Sprintf("%.0f MB", rs.MemAllocMB)),
		labelStyle.Render("Disk:") + " " + mutedVal.Render(fmt.Sprintf("DB %.1f MB", rs.DBSizeMB)),
	}

	albumsDone := stats.Albums.Resolved
	albumsTotal := stats.Albums.Total
	albumsPending := stats.Albums.Pending
	var albumPctTotal, albumPctVsPending float64
	if albumsTotal > 0 {
		albumPctTotal = float64(albumsDone) / float64(albumsTotal) * 100
	}
	if albumsDone+albumsPending > 0 {
		albumPctVsPending = float64(albumsDone) / float64(albumsDone+albumsPending) * 100
	}

	tracksDone := stats.Tracks.Completed
	tracksTotal := stats.Tracks.Total
	tracksPending := stats.Tracks.Pending
	var trackPctTotal, trackPctVsPending float64
	if tracksTotal > 0 {
		trackPctTotal = float64(tracksDone) / float64(tracksTotal) * 100
	}
	if tracksDone+tracksPending > 0 {
		trackPctVsPending = float64(tracksDone) / float64(tracksDone+tracksPending) * 100
	}

	line3Parts := []string{
		labelStyle.Render("Albums:") + " " + mutedVal.Render(fmt.Sprintf("%d/%d", albumsDone, albumsTotal)) + " " + valStyle.Render(fmt.Sprintf("%.1f%%", albumPctTotal)),
		labelStyle.Render("Tracks:") + " " + mutedVal.Render(fmt.Sprintf("%d/%d", tracksDone, tracksTotal)) + " " + valStyle.Render(fmt.Sprintf("%.1f%%", trackPctTotal)),
	}
	_ = albumPctVsPending
	_ = trackPctVsPending

	line1 := " " + strings.Join(line1Parts, sep)
	line2 := " " + strings.Join(line2Parts, sep)
	line3 := " " + strings.Join(line3Parts, sep)

	borderLine := lipgloss.NewStyle().
		Foreground(Muted).
		Render(strings.Repeat("\u2500", width))

	return borderLine + "\n" + line1 + "\n" + line2 + "\n" + line3
}
