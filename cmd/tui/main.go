package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/johnarleyburns/parso-ia-music-indexer/internal/audio"
	"github.com/johnarleyburns/parso-ia-music-indexer/internal/clap"
	"github.com/johnarleyburns/parso-ia-music-indexer/internal/config"
	"github.com/johnarleyburns/parso-ia-music-indexer/internal/db"
	"github.com/johnarleyburns/parso-ia-music-indexer/internal/hybrid"
	"github.com/johnarleyburns/parso-ia-music-indexer/internal/ia"
	ratelimit "github.com/johnarleyburns/parso-ia-music-indexer/internal/rate"
	"github.com/johnarleyburns/parso-ia-music-indexer/internal/tui"
)

func runCoordinator(cfg *config.Config, sqlDB *db.DB, events chan<- tui.ActivityEvent, controls <-chan tui.ControlCmd) {
	coordRunning := false
	coordStopCh := make(chan struct{})

	resolverCount := 0
	nextResolverID := 1
	resolverStopChs := make(map[int]chan struct{})

	workerCount := 0
	nextWorkerID := 1
	workerStopChs := make(map[int]chan struct{})

	dbMu := &sync.Mutex{}
	clapClient := clap.NewMockClient()
	iaClient := ia.NewClient(60 * time.Second)
	metaLimiter := ratelimit.NewLimiter(cfg.IAApiRate)

	events <- tui.ActivityEvent{
		Type:      tui.EventInfo,
		Timestamp: time.Now(),
		Message:   "Application started. [s] coordinator  [r] resolvers  [w] analyzers",
	}

	for {
		select {
		case cmd, ok := <-controls:
			if !ok {
				return
			}
			switch cmd.Action {
			case tui.CmdStartCoordinator:
				if !coordRunning {
					coordRunning = true
					coordStopCh = make(chan struct{})
					events <- tui.ActivityEvent{
						Type:      tui.EventCoordStarted,
						Timestamp: time.Now(),
						Message:   "Coordinator started (album discovery)",
					}
					go coordinatorLoop(cfg, sqlDB, events, coordStopCh)
				}
			case tui.CmdStopCoordinator:
				if coordRunning {
					coordRunning = false
					close(coordStopCh)
					events <- tui.ActivityEvent{
						Type:      tui.EventCoordStopped,
						Timestamp: time.Now(),
						Message:   "Coordinator stopped",
					}
				}

			case tui.CmdAddResolver:
				resolverCount++
				rID := fmt.Sprintf("resolver-%d", nextResolverID)
				nextResolverID++
				stopCh := make(chan struct{})
				resolverStopChs[nextResolverID-1] = stopCh
				events <- tui.ActivityEvent{
					Type:       tui.EventWorkerStarted,
					Timestamp:  time.Now(),
					Identifier: rID,
					WorkerID:   rID,
					Message:    fmt.Sprintf("Resolver %s started (pool: %d)", rID, resolverCount),
				}
				go albumResolverLoop(sqlDB, events, stopCh, dbMu, iaClient, metaLimiter, rID)
			case tui.CmdRemoveResolver:
				if resolverCount > 0 {
					resolverCount--
					for id, ch := range resolverStopChs {
						close(ch)
						delete(resolverStopChs, id)
						break
					}
					events <- tui.ActivityEvent{
						Type:      tui.EventWorkerStopped,
						Timestamp: time.Now(),
						Message:   fmt.Sprintf("Resolver removed (pool: %d)", resolverCount),
					}
				}

			case tui.CmdAddWorker:
				workerCount++
				wID := fmt.Sprintf("analyzer-%d", nextWorkerID)
				nextWorkerID++
				stopCh := make(chan struct{})
				workerStopChs[nextWorkerID-1] = stopCh
				events <- tui.ActivityEvent{
					Type:       tui.EventWorkerStarted,
					Timestamp:  time.Now(),
					Identifier: wID,
					WorkerID:   wID,
					Message:    fmt.Sprintf("Analyzer %s started (pool: %d)", wID, workerCount),
				}
				go workerLoop(cfg, sqlDB, events, stopCh, dbMu, clapClient, iaClient, wID)
			case tui.CmdRemoveWorker:
				if workerCount > 0 {
					workerCount--
					for id, ch := range workerStopChs {
						close(ch)
						delete(workerStopChs, id)
						break
					}
					events <- tui.ActivityEvent{
						Type:      tui.EventWorkerStopped,
						Timestamp: time.Now(),
						Message:   fmt.Sprintf("Analyzer removed (pool: %d)", workerCount),
					}
				}

			case tui.CmdShutdown:
				if coordRunning {
					close(coordStopCh)
				}
				for _, ch := range resolverStopChs {
					close(ch)
				}
				for _, ch := range workerStopChs {
					close(ch)
				}
				return
			}
		}
	}
}

func coordinatorLoop(cfg *config.Config, sqlDB *db.DB, events chan<- tui.ActivityEvent, stopCh <-chan struct{}) {
	client := ia.NewClient(60 * time.Second)
	limiter := ratelimit.NewLimiter(cfg.IAApiRate)

	query := os.Getenv("IA_QUERY")
	if query == "" {
		query = ia.DefaultQuery
	}

	cursor := ""
	itemsIndexed := 0

	cursorState, err := db.GetCursor(sqlDB.Conn)
	if err == nil && cursorState != nil && cursorState.Cursor != "" {
		cursor = cursorState.Cursor
		itemsIndexed = cursorState.ItemsIndexed
		events <- tui.ActivityEvent{
			Type:      tui.EventInfo,
			Timestamp: time.Now(),
			Message:   fmt.Sprintf("Resuming from cursor: %d items indexed", itemsIndexed),
		}
	}

	for {
		select {
		case <-stopCh:
			if err := db.SaveCursor(sqlDB.Conn, cursor, itemsIndexed); err != nil {
				events <- tui.ActivityEvent{
					Type:      tui.EventInfo,
					Timestamp: time.Now(),
					Message:   fmt.Sprintf("Failed to save cursor: %v", err),
					Error:     err.Error(),
				}
			}
			events <- tui.ActivityEvent{
				Type:      tui.EventCoordStopped,
				Timestamp: time.Now(),
				Message:   fmt.Sprintf("Coordinator stopped. %d total items indexed. Cursor saved.", itemsIndexed),
				Count:     itemsIndexed,
			}
			return
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

		if err := limiter.Wait(ctx); err != nil {
			cancel()
			events <- tui.ActivityEvent{
				Type:      tui.EventInfo,
				Timestamp: time.Now(),
				Message:   fmt.Sprintf("Rate limiter error: %v", err),
				Error:     err.Error(),
			}
			return
		}

		resp, err := ia.ScrapePage(ctx, client, cursor, query, ia.DefaultSort, ia.DefaultCount)
		cancel()

		if err != nil {
			events <- tui.ActivityEvent{
				Type:      tui.EventInfo,
				Timestamp: time.Now(),
				Message:   fmt.Sprintf("Scrape error: %v", err),
				Error:     err.Error(),
			}
			select {
			case <-time.After(5 * time.Second):
			case <-stopCh:
				db.SaveCursor(sqlDB.Conn, cursor, itemsIndexed)
				return
			}
			continue
		}

		if len(resp.Items) == 0 {
			events <- tui.ActivityEvent{
				Type:      tui.EventInfo,
				Timestamp: time.Now(),
				Message:   "Scrape returned 0 items — collection exhausted or empty result",
			}
			db.SaveCursor(sqlDB.Conn, cursor, itemsIndexed)
			return
		}

		identifiers := make([]db.AlbumInsert, len(resp.Items))
		for i, item := range resp.Items {
			identifiers[i] = db.AlbumInsert{Identifier: item.Identifier, Downloads: item.Downloads}
		}

		inserted, err := db.BulkInsertAlbums(sqlDB.Conn, identifiers)
		if err != nil {
			events <- tui.ActivityEvent{
				Type:      tui.EventInfo,
				Timestamp: time.Now(),
				Message:   fmt.Sprintf("DB insert error: %v", err),
				Error:     err.Error(),
			}
			select {
			case <-time.After(5 * time.Second):
			case <-stopCh:
				db.SaveCursor(sqlDB.Conn, cursor, itemsIndexed)
				return
			}
			continue
		}

		itemsIndexed += int(inserted)
		cursor = resp.Cursor
		total := resp.Total

		if err := db.SaveCursor(sqlDB.Conn, cursor, itemsIndexed); err != nil {
			events <- tui.ActivityEvent{
				Type:      tui.EventInfo,
				Timestamp: time.Now(),
				Message:   fmt.Sprintf("Failed to save cursor: %v", err),
				Error:     err.Error(),
			}
		}

		events <- tui.ActivityEvent{
			Type:      tui.EventCoordProgress,
			Timestamp: time.Now(),
			Message:   fmt.Sprintf("Scraped page: %d new albums, %d total indexed (of ~%d)", inserted, itemsIndexed, total),
			Count:     itemsIndexed,
			Cursor:    cursor,
			Total:     total,
		}

		for _, a := range identifiers {
			events <- tui.ActivityEvent{
				Type:       tui.EventQueueAdded,
				Timestamp:  time.Now(),
				Identifier: a.Identifier,
				Message:    fmt.Sprintf("+ %s  (album added)", a.Identifier),
			}
		}

		if cursor == "" {
			events <- tui.ActivityEvent{
				Type:      tui.EventCoordStopped,
				Timestamp: time.Now(),
				Message:   fmt.Sprintf("No more pages. %d total items indexed", itemsIndexed),
				Count:     itemsIndexed,
			}
			return
		}

		select {
		case <-time.After(2 * time.Second):
		case <-stopCh:
			db.SaveCursor(sqlDB.Conn, cursor, itemsIndexed)
			events <- tui.ActivityEvent{
				Type:      tui.EventCoordStopped,
				Timestamp: time.Now(),
				Message:   fmt.Sprintf("Coordinator stopped. %d total items indexed", itemsIndexed),
				Count:     itemsIndexed,
			}
			return
		}
	}
}

func albumResolverLoop(sqlDB *db.DB, events chan<- tui.ActivityEvent, stopCh <-chan struct{},
	dbMu *sync.Mutex, iaClient *http.Client, metaLimiter *ratelimit.Limiter, resolverID string) {
	for {
		select {
		case <-stopCh:
			return
		default:
		}

		dbMu.Lock()
		albumID, err := db.ClaimUnresolvedAlbum(sqlDB.Conn, resolverID)
		dbMu.Unlock()
		if err != nil {
			events <- tui.ActivityEvent{
				Type:      tui.EventInfo,
				Timestamp: time.Now(),
				WorkerID:  resolverID,
				Message:   fmt.Sprintf("[%s] Claim album error: %v", resolverID, err),
				Error:     err.Error(),
			}
			sleepOrStop(5*time.Second, stopCh)
			continue
		}

		if albumID == "" {
			sleepOrStop(3*time.Second, stopCh)
			continue
		}

		resolveAlbum(sqlDB, events, dbMu, iaClient, metaLimiter, resolverID, albumID)
	}
}

func main() {
	cfg := config.Parse()

	if cfg.Headless {
		runHeadless(cfg)
		return
	}

	runTUI(cfg)
}

func workerLoop(cfg *config.Config, sqlDB *db.DB, events chan<- tui.ActivityEvent, stopCh <-chan struct{},
	dbMu *sync.Mutex, clapClient clap.CLAPClient, iaClient *http.Client, workerID string) {
	batchSize := 2

	for {
		select {
		case <-stopCh:
			return
		default:
		}

		dbMu.Lock()
		tracks, err := db.ClaimNextTrackBatch(sqlDB.Conn, workerID, batchSize)
		dbMu.Unlock()
		if err != nil {
			events <- tui.ActivityEvent{
				Type:      tui.EventInfo,
				Timestamp: time.Now(),
				WorkerID:  workerID,
				Message:   fmt.Sprintf("Claim track batch error: %v", err),
				Error:     err.Error(),
			}
			sleepOrStop(5*time.Second, stopCh)
			continue
		}

		if len(tracks) > 0 {
			for _, track := range tracks {
				select {
				case <-stopCh:
					return
				default:
				}
				analyzeTrack(cfg, sqlDB, events, dbMu, clapClient, iaClient, workerID, track)
			}
			continue
		}

		sleepOrStop(5*time.Second, stopCh)
	}
}

func analyzeTrack(cfg *config.Config, sqlDB *db.DB, events chan<- tui.ActivityEvent,
	dbMu *sync.Mutex, clapClient clap.CLAPClient, iaClient *http.Client, workerID string, track db.ClaimedTrack) {

	trackLabel := track.Title
	if trackLabel == "" {
		trackLabel = track.Filename
	}

	events <- tui.ActivityEvent{
		Type:       tui.EventAnalysisStarted,
		Timestamp:  time.Now(),
		Identifier: fmt.Sprintf("%s/%s", track.AlbumID, track.Filename),
		WorkerID:   workerID,
		Message:    fmt.Sprintf("[%s] Analyzing: %s", workerID, trackLabel),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	mp3Data, err := audio.StreamAudioFromURL(ctx, iaClient, track.DownloadURL, cfg.MaxBytes)
	cancel()
	if err != nil {
		dbMu.Lock()
		db.MarkTrackFailed(sqlDB.Conn, track.ID, fmt.Sprintf("stream: %v", err))
		dbMu.Unlock()
		events <- tui.ActivityEvent{
			Type:       tui.EventAnalysisFailed,
			Timestamp:  time.Now(),
			Identifier: trackLabel,
			WorkerID:   workerID,
			Message:    fmt.Sprintf("[%s] Failed %s: %v", workerID, trackLabel, err),
			Error:      err.Error(),
		}
		return
	}

	pcmSamples, sampleRate, err := audio.DecodeMP3(mp3Data)
	if err != nil {
		dbMu.Lock()
		db.MarkTrackFailed(sqlDB.Conn, track.ID, fmt.Sprintf("decode: %v", err))
		dbMu.Unlock()
		events <- tui.ActivityEvent{
			Type:       tui.EventAnalysisFailed,
			Timestamp:  time.Now(),
			Identifier: trackLabel,
			WorkerID:   workerID,
			Message:    fmt.Sprintf("[%s] Failed %s: %v", workerID, trackLabel, err),
			Error:      err.Error(),
		}
		return
	}

	snrDB := audio.CalculateSNR(pcmSamples)
	centroidHz := audio.CalculateSpectralCentroid(pcmSamples, sampleRate)
	crestFactor := audio.CalculateCrestFactor(pcmSamples)
	compositeScore := audio.CalculateCompositeScore(snrDB, centroidHz, crestFactor)

	if compositeScore < audio.QualityUnusable {
		dbMu.Lock()
		db.MarkTrackFailed(sqlDB.Conn, track.ID, fmt.Sprintf("low quality: score=%.3f snr=%.1f centroid=%.0f crest=%.1f", compositeScore, snrDB, centroidHz, crestFactor))
		dbMu.Unlock()
		events <- tui.ActivityEvent{
			Type:         tui.EventAnalysisFailed,
			Timestamp:    time.Now(),
			Identifier:   trackLabel,
			WorkerID:     workerID,
			Message:      fmt.Sprintf("[%s] Low quality %s: score=%.3f", workerID, trackLabel, compositeScore),
			QualityScore: compositeScore,
		}
		return
	}

	mfccVec := audio.ComputeMFCCPool(pcmSamples, sampleRate)
	chromaVec := audio.ComputeChromaPool(pcmSamples)

	clapCtx, clapCancel := context.WithTimeout(context.Background(), 30*time.Second)
	clapVec, err := clapClient.GetEmbedding(clapCtx, nil, int32(sampleRate))
	clapCancel()
	if err != nil {
		dbMu.Lock()
		db.MarkTrackFailed(sqlDB.Conn, track.ID, fmt.Sprintf("clap: %v", err))
		dbMu.Unlock()
		events <- tui.ActivityEvent{
			Type:       tui.EventAnalysisFailed,
			Timestamp:  time.Now(),
			Identifier: trackLabel,
			WorkerID:   workerID,
			Message:    fmt.Sprintf("[%s] CLAP error %s: %v", workerID, trackLabel, err),
			Error:      err.Error(),
		}
		return
	}

	hybridVec := hybrid.FuseFeatures(clapVec, mfccVec, chromaVec)

	dbMu.Lock()
	if err := db.SaveEmbedding(sqlDB.Conn, track.ID, hybridVec, compositeScore); err != nil {
		dbMu.Unlock()
		events <- tui.ActivityEvent{
			Type:       tui.EventAnalysisFailed,
			Timestamp:  time.Now(),
			Identifier: trackLabel,
			WorkerID:   workerID,
			Message:    fmt.Sprintf("[%s] Save embedding error %s: %v", workerID, trackLabel, err),
			Error:      err.Error(),
		}
		return
	}
	db.MarkTrackCompleted(sqlDB.Conn, track.ID)
	dbMu.Unlock()

	events <- tui.ActivityEvent{
		Type:         tui.EventAnalysisComplete,
		Timestamp:    time.Now(),
		Identifier:   trackLabel,
		WorkerID:     workerID,
		Message:      fmt.Sprintf("[%s] Complete %s (quality: %.2f)", workerID, trackLabel, compositeScore),
		QualityScore: compositeScore,
	}
}

func resolveAlbum(sqlDB *db.DB, events chan<- tui.ActivityEvent, dbMu *sync.Mutex,
	iaClient *http.Client, metaLimiter *ratelimit.Limiter, workerID, albumID string) {

	events <- tui.ActivityEvent{
		Type:       tui.EventAlbumResolving,
		Timestamp:  time.Now(),
		Identifier: albumID,
		WorkerID:   workerID,
		Message:    fmt.Sprintf("[%s] Resolving album: %s", workerID, albumID),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := metaLimiter.Wait(ctx); err != nil {
		cancel()
		dbMu.Lock()
		db.MarkAlbumFailed(sqlDB.Conn, albumID, fmt.Sprintf("rate limit: %v", err))
		dbMu.Unlock()
		return
	}

	album, err := ia.LookupAlbumMetadata(ctx, iaClient, albumID)
	cancel()
	if err != nil {
		dbMu.Lock()
		db.MarkAlbumFailed(sqlDB.Conn, albumID, fmt.Sprintf("metadata: %v", err))
		dbMu.Unlock()
		events <- tui.ActivityEvent{
			Type:       tui.EventAlbumFailed,
			Timestamp:  time.Now(),
			Identifier: albumID,
			WorkerID:   workerID,
			Message:    fmt.Sprintf("[%s] Album failed %s: %v", workerID, albumID, err),
			Error:      err.Error(),
		}
		return
	}

	if len(album.Tracks) == 0 {
		dbMu.Lock()
		db.MarkAlbumFailed(sqlDB.Conn, albumID, "no acceptable MP3 tracks found")
		dbMu.Unlock()
		events <- tui.ActivityEvent{
			Type:       tui.EventAlbumFailed,
			Timestamp:  time.Now(),
			Identifier: albumID,
			WorkerID:   workerID,
			Message:    fmt.Sprintf("[%s] Album %s: no acceptable MP3 tracks", workerID, albumID),
		}
		return
	}

	trackInserts := make([]db.TrackInsert, len(album.Tracks))
	for i, t := range album.Tracks {
		trackInserts[i] = db.TrackInsert{
			Filename:    t.Filename,
			Title:       t.Title,
			TrackNumber: t.TrackNumber,
			Format:      t.Format,
			Bitrate:     t.Bitrate,
			DownloadURL: t.DownloadURL,
		}
	}

	dbMu.Lock()
	inserted, err := db.InsertTracks(sqlDB.Conn, albumID, trackInserts)
	if err != nil {
		dbMu.Unlock()
		events <- tui.ActivityEvent{
			Type:       tui.EventAlbumFailed,
			Timestamp:  time.Now(),
			Identifier: albumID,
			WorkerID:   workerID,
			Message:    fmt.Sprintf("[%s] Insert tracks error %s: %v", workerID, albumID, err),
			Error:      err.Error(),
		}
		return
	}

	if err := db.MarkAlbumResolved(sqlDB.Conn, albumID, album.Title, album.Creator, album.Collection, album.ArtURL, inserted); err != nil {
		dbMu.Unlock()
		events <- tui.ActivityEvent{
			Type:       tui.EventAlbumFailed,
			Timestamp:  time.Now(),
			Identifier: albumID,
			WorkerID:   workerID,
			Message:    fmt.Sprintf("[%s] Mark resolved error %s: %v", workerID, albumID, err),
			Error:      err.Error(),
		}
		return
	}
	dbMu.Unlock()

	events <- tui.ActivityEvent{
		Type:       tui.EventAlbumResolved,
		Timestamp:  time.Now(),
		Identifier: albumID,
		WorkerID:   workerID,
		Message:    fmt.Sprintf("[%s] Resolved %s: %q — %d tracks", workerID, albumID, album.Title, inserted),
		TrackCount: inserted,
	}
}

func sleepOrStop(d time.Duration, stopCh <-chan struct{}) {
	select {
	case <-time.After(d):
	case <-stopCh:
	}
}

func runTUI(cfg *config.Config) {
	sqlDB, err := db.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	artDir := filepath.Join(filepath.Dir(cfg.DBPath), "art")
	artCache := tui.NewArtCache(artDir)

	events := tui.NewEventChannel()
	controls := tui.NewControlChannel()
	go runCoordinator(cfg, sqlDB, events, controls)

	m := tui.NewMainModel(cfg, sqlDB.Conn, events, controls, artCache)
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

	stats, err := db.GetCombinedStats(sqlDB.Conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error getting stats: %v\n", err)
		os.Exit(1)
	}

	embedCount, err := db.GetEmbeddingCount(sqlDB.Conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error getting embedding count: %v\n", err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.Encode(map[string]interface{}{
		"event":   "db_stats",
		"mode":    "headless",
		"db_path": cfg.DBPath,
		"albums": map[string]int{
			"total":     stats.Albums.Total,
			"pending":   stats.Albums.Pending,
			"resolving": stats.Albums.Resolving,
			"resolved":  stats.Albums.Resolved,
			"failed":    stats.Albums.Failed,
		},
		"tracks": map[string]int{
			"total":      stats.Tracks.Total,
			"pending":    stats.Tracks.Pending,
			"processing": stats.Tracks.Processing,
			"completed":  stats.Tracks.Completed,
			"failed":     stats.Tracks.Failed,
		},
		"embeddings": map[string]int{
			"count": embedCount,
		},
	})

	events := tui.NewEventChannel()
	controls := tui.NewControlChannel()
	go runCoordinator(cfg, sqlDB, events, controls)

	controls <- tui.ControlCmd{Action: tui.CmdStartCoordinator}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

loop:
	for {
		select {
		case event, ok := <-events:
			if !ok {
				break loop
			}
			enc.Encode(event)
		case <-sigCh:
			controls <- tui.ControlCmd{Action: tui.CmdShutdown}
			time.Sleep(300 * time.Millisecond)
			break loop
		}
	}
}
