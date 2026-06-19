package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/johnarleyburns/parso-ia-music-indexer/internal/audio"
	"github.com/johnarleyburns/parso-ia-music-indexer/internal/clap"
	"github.com/johnarleyburns/parso-ia-music-indexer/internal/config"
	"github.com/johnarleyburns/parso-ia-music-indexer/internal/db"
	"github.com/johnarleyburns/parso-ia-music-indexer/internal/ia"
	ratelimit "github.com/johnarleyburns/parso-ia-music-indexer/internal/rate"
	"github.com/johnarleyburns/parso-ia-music-indexer/internal/tui"
)

const (
	maxTrackRetries = 3
	maxAlbumRetries = 3
	stuckTrackAge   = 10 * time.Minute
)

func isTransientError(errMsg string) bool {
	transient := []string{
		"stream:", "rate limit:", "clap:",
		"timeout", "connection refused", "connection reset",
		"i/o timeout", "no such host", "EOF",
		"503", "429", "502", "504",
	}
	lower := strings.ToLower(errMsg)
	for _, pattern := range transient {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

func runCoordinator(cfg *config.Config, sqlDB *db.DB, events chan<- tui.ActivityEvent, controls <-chan tui.ControlCmd, metrics *tui.Metrics, clapClient clap.CLAPClient) {
	coordRunning := false
	coordStopCh := make(chan struct{})

	resolverCount := 0
	nextResolverID := 1
	resolverStopChs := make(map[int]chan struct{})

	workerCount := 0
	nextWorkerID := 1
	workerStopChs := make(map[int]chan struct{})

	dbMu := &sync.Mutex{}
	iaClient := ia.NewClient(60 * time.Second)
	iaClient.Transport = tui.NewInstrumentedTransport(metrics)
	metaLimiter := ratelimit.NewBurstLimiter(cfg.IAApiRate, 5)

	stuckTicker := time.NewTicker(5 * time.Minute)
	defer stuckTicker.Stop()

	collStats, _ := db.GetCollectionStats(sqlDB.Conn)
	collMsg := "0 collections"
	if collStats != nil {
		collMsg = fmt.Sprintf("%d collections loaded", collStats.Total)
	}
	events <- tui.ActivityEvent{
		Type:      tui.EventInfo,
		Timestamp: time.Now(),
		Message:   fmt.Sprintf("Application started. %s. [s] coordinator  [r] resolvers  [w] analyzers", collMsg),
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
						Message:   "Coordinator started (collection discovery)",
					}
					go coordinatorLoop(cfg, sqlDB, events, coordStopCh, metrics)
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
				go albumResolverLoop(sqlDB, events, stopCh, dbMu, iaClient, metaLimiter, rID, metrics)
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
				go workerLoop(cfg, sqlDB, events, stopCh, dbMu, clapClient, iaClient, wID, metrics)
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

			case tui.CmdResetFailed:
				dbMu.Lock()
				albumCount, trackCount, err := db.ResetAllFailed(sqlDB.Conn)
				dbMu.Unlock()
				if err != nil {
					events <- tui.ActivityEvent{
						Type:      tui.EventInfo,
						Timestamp: time.Now(),
						Message:   fmt.Sprintf("Reset failed error: %v", err),
						Error:     err.Error(),
					}
				} else {
					events <- tui.ActivityEvent{
						Type:      tui.EventInfo,
						Timestamp: time.Now(),
						Message:   fmt.Sprintf("Reset %d failed albums and %d failed tracks to pending", albumCount, trackCount),
					}
				}
			}

		case <-stuckTicker.C:
			dbMu.Lock()
			resetCount, err := db.ResetStuckTracks(sqlDB.Conn, stuckTrackAge)
			dbMu.Unlock()
			if err == nil && resetCount > 0 {
				events <- tui.ActivityEvent{
					Type:      tui.EventInfo,
					Timestamp: time.Now(),
					Message:   fmt.Sprintf("Reset %d stuck tracks back to pending", resetCount),
				}
			}
		}
	}
}

func coordinatorLoop(cfg *config.Config, sqlDB *db.DB, events chan<- tui.ActivityEvent, stopCh <-chan struct{}, metrics *tui.Metrics) {
	client := ia.NewClient(60 * time.Second)
	client.Transport = tui.NewInstrumentedTransport(metrics)
	limiter := ratelimit.NewLimiter(cfg.IAApiRate)

	denylist := loadDenylist(cfg.LibrivoxDenylistPath)
	if len(denylist) > 0 {
		events <- tui.ActivityEvent{
			Type:      tui.EventInfo,
			Timestamp: time.Now(),
			Message:   fmt.Sprintf("Loaded %d denylist entries from %s", len(denylist), cfg.LibrivoxDenylistPath),
		}
	}

	if err := db.ResetAllCollectionsForSync(sqlDB.Conn); err != nil {
		events <- tui.ActivityEvent{
			Type:      tui.EventInfo,
			Timestamp: time.Now(),
			Message:   fmt.Sprintf("Failed to reset collections for sync: %v", err),
			Error:     err.Error(),
		}
		return
	}

	collections, err := db.GetAllCollections(sqlDB.Conn)
	if err != nil {
		events <- tui.ActivityEvent{
			Type:      tui.EventInfo,
			Timestamp: time.Now(),
			Message:   fmt.Sprintf("Failed to load collections: %v", err),
			Error:     err.Error(),
		}
		return
	}

	if len(collections) == 0 {
		events <- tui.ActivityEvent{
			Type:      tui.EventInfo,
			Timestamp: time.Now(),
			Message:   "No collections configured. Add collections to begin ingestion.",
		}
		return
	}

	events <- tui.ActivityEvent{
		Type:      tui.EventCoordProgress,
		Timestamp: time.Now(),
		Message:   fmt.Sprintf("Starting discovery for %d collections", len(collections)),
		Total:     len(collections),
	}

	totalAlbums := 0
	for i, coll := range collections {
		select {
		case <-stopCh:
			events <- tui.ActivityEvent{
				Type:      tui.EventCoordStopped,
				Timestamp: time.Now(),
				Message:   fmt.Sprintf("Coordinator stopped. %d/%d collections processed, %d albums discovered", i, len(collections), totalAlbums),
				Count:     totalAlbums,
			}
			return
		default:
		}

		discovered, err := discoverCollection(cfg, sqlDB, client, limiter, events, stopCh, coll, i+1, len(collections), denylist)
		if err != nil {
			events <- tui.ActivityEvent{
				Type:         tui.EventCollectionFailed,
				Timestamp:    time.Now(),
				CollectionID: coll.CollectionID,
				Message:      fmt.Sprintf("Collection %q failed: %v", coll.Title, err),
				Error:        err.Error(),
			}
			db.MarkCollectionFailed(sqlDB.Conn, coll.CollectionID, err.Error())
			continue
		}
		totalAlbums += discovered
	}

	events <- tui.ActivityEvent{
		Type:      tui.EventCoordStopped,
		Timestamp: time.Now(),
		Message:   fmt.Sprintf("All collections processed. %d total albums discovered", totalAlbums),
		Count:     totalAlbums,
	}
}

func discoverCollection(cfg *config.Config, sqlDB *db.DB, client *http.Client, limiter *ratelimit.Limiter,
	events chan<- tui.ActivityEvent, stopCh <-chan struct{}, coll db.Collection, idx, total int, denylist map[string]bool) (int, error) {

	events <- tui.ActivityEvent{
		Type:         tui.EventCollectionStarted,
		Timestamp:    time.Now(),
		CollectionID: coll.CollectionID,
		Message:      fmt.Sprintf("[%d/%d] Discovering collection %q (~%d items)", idx, total, coll.Title, coll.ExpectedCount),
		Total:        coll.ExpectedCount,
	}

	db.MarkCollectionDiscovering(sqlDB.Conn, coll.CollectionID)

	cursor := coll.LastCursor
	discovered := coll.DiscoveredCount

	for {
		select {
		case <-stopCh:
			db.SaveCollectionCursor(sqlDB.Conn, coll.CollectionID, cursor, discovered)
			return discovered, nil
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := limiter.Wait(ctx); err != nil {
			cancel()
			return discovered, fmt.Errorf("rate limiter: %w", err)
		}

		resp, err := ia.ScrapePage(ctx, client, cursor, coll.Query, ia.DefaultSort, ia.DefaultCount)
		cancel()

		if err != nil {
			events <- tui.ActivityEvent{
				Type:         tui.EventInfo,
				Timestamp:    time.Now(),
				CollectionID: coll.CollectionID,
				Message:      fmt.Sprintf("Scrape error for %q: %v (retrying)", coll.Title, err),
				Error:        err.Error(),
			}
			select {
			case <-time.After(5 * time.Second):
			case <-stopCh:
				db.SaveCollectionCursor(sqlDB.Conn, coll.CollectionID, cursor, discovered)
				return discovered, nil
			}
			continue
		}

		if len(resp.Items) == 0 {
			break
		}

		albums := make([]db.AlbumInsert, len(resp.Items))
		for i, item := range resp.Items {
			albums[i] = db.AlbumInsert{Identifier: item.Identifier, Downloads: item.Downloads}
		}

		albums = filterDenylisted(albums, denylist)

		inserted, err := db.BulkInsertCollectionAlbums(sqlDB.Conn, coll.CollectionID, albums)
		if err != nil {
			return discovered, fmt.Errorf("insert albums: %w", err)
		}

		discovered += int(inserted)
		cursor = resp.Cursor

		db.SaveCollectionCursor(sqlDB.Conn, coll.CollectionID, cursor, discovered)

		events <- tui.ActivityEvent{
			Type:         tui.EventCollectionProgress,
			Timestamp:    time.Now(),
			CollectionID: coll.CollectionID,
			Message:      fmt.Sprintf("[%d/%d] %q: +%d new, %d total (of ~%d)", idx, total, coll.Title, inserted, discovered, coll.ExpectedCount),
			Count:        discovered,
			Total:        coll.ExpectedCount,
		}

		if cursor == "" {
			break
		}

		select {
		case <-time.After(2 * time.Second):
		case <-stopCh:
			db.SaveCollectionCursor(sqlDB.Conn, coll.CollectionID, cursor, discovered)
			return discovered, nil
		}
	}

	db.MarkCollectionDiscovered(sqlDB.Conn, coll.CollectionID, discovered)

	events <- tui.ActivityEvent{
		Type:         tui.EventCollectionCompleted,
		Timestamp:    time.Now(),
		CollectionID: coll.CollectionID,
		Message:      fmt.Sprintf("[%d/%d] Collection %q complete: %d albums", idx, total, coll.Title, discovered),
		Count:        discovered,
	}

	return discovered, nil
}

func albumResolverLoop(sqlDB *db.DB, events chan<- tui.ActivityEvent, stopCh <-chan struct{},
	dbMu *sync.Mutex, iaClient *http.Client, metaLimiter *ratelimit.Limiter, resolverID string, metrics *tui.Metrics) {
	batchSize := 10

	for {
		select {
		case <-stopCh:
			return
		default:
		}

		dbMu.Lock()
		albumIDs, err := db.ClaimUnresolvedAlbumBatch(sqlDB.Conn, resolverID, batchSize)
		dbMu.Unlock()
		if err != nil {
			events <- tui.ActivityEvent{
				Type:      tui.EventInfo,
				Timestamp: time.Now(),
				WorkerID:  resolverID,
				Message:   fmt.Sprintf("[%s] Claim album batch error: %v", resolverID, err),
				Error:     err.Error(),
			}
			sleepOrStop(5*time.Second, stopCh)
			continue
		}

		if len(albumIDs) == 0 {
			sleepOrStop(3*time.Second, stopCh)
			continue
		}

		events <- tui.ActivityEvent{
			Type:      tui.EventInfo,
			Timestamp: time.Now(),
			WorkerID:  resolverID,
			Message:   fmt.Sprintf("[%s] Resolving batch of %d albums", resolverID, len(albumIDs)),
		}

		var wg sync.WaitGroup
		for _, albumID := range albumIDs {
			select {
			case <-stopCh:
				wg.Wait()
				return
			default:
			}
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				resolveAlbum(sqlDB, events, dbMu, iaClient, metaLimiter, resolverID, id, metrics)
			}(albumID)
		}
		wg.Wait()
	}
}

func main() {
	cfg := config.Parse()

	if cfg.SearchText != "" {
		runTextSearch(cfg)
		return
	}

	if cfg.Headless {
		runHeadless(cfg)
		return
	}

	runTUI(cfg)
}

func workerLoop(cfg *config.Config, sqlDB *db.DB, events chan<- tui.ActivityEvent, stopCh <-chan struct{},
	dbMu *sync.Mutex, clapClient clap.CLAPClient, iaClient *http.Client, workerID string, metrics *tui.Metrics) {
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
				analyzeTrack(cfg, sqlDB, events, dbMu, clapClient, iaClient, workerID, track, metrics)
			}
			continue
		}

		sleepOrStop(5*time.Second, stopCh)
	}
}

func analyzeTrack(cfg *config.Config, sqlDB *db.DB, events chan<- tui.ActivityEvent,
	dbMu *sync.Mutex, clapClient clap.CLAPClient, iaClient *http.Client, workerID string, track db.ClaimedTrack, metrics *tui.Metrics) {

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
		errMsg := fmt.Sprintf("stream: %v", err)
		dbMu.Lock()
		requeued, _ := db.RequeueTrackForRetry(sqlDB.Conn, track.ID, maxTrackRetries, errMsg)
		dbMu.Unlock()
		if requeued {
			events <- tui.ActivityEvent{
				Type:       tui.EventInfo,
				Timestamp:  time.Now(),
				Identifier: trackLabel,
				WorkerID:   workerID,
				Message:    fmt.Sprintf("[%s] Retry queued %s: %v", workerID, trackLabel, err),
			}
		} else {
			events <- tui.ActivityEvent{
				Type:       tui.EventAnalysisFailed,
				Timestamp:  time.Now(),
				Identifier: trackLabel,
				WorkerID:   workerID,
				Message:    fmt.Sprintf("[%s] Failed %s: %v", workerID, trackLabel, err),
				Error:      err.Error(),
			}
		}
		return
	}

	pcmSamples, sampleRate, err := audio.DecodeMP3(mp3Data)
	if err != nil {
		reason := fmt.Sprintf("decode: %v", err)
		dbMu.Lock()
		skipped, _ := db.FlagAlbumPoorQuality(sqlDB.Conn, track.ID, track.AlbumID, reason)
		dbMu.Unlock()
		events <- tui.ActivityEvent{
			Type:       tui.EventAnalysisFailed,
			Timestamp:  time.Now(),
			Identifier: trackLabel,
			WorkerID:   workerID,
			Message:    fmt.Sprintf("[%s] Failed %s: %v", workerID, trackLabel, err),
			Error:      err.Error(),
		}
		if skipped > 0 {
			events <- tui.ActivityEvent{
				Type:       tui.EventAlbumFailed,
				Timestamp:  time.Now(),
				Identifier: track.AlbumID,
				WorkerID:   workerID,
				Message:    fmt.Sprintf("[%s] Flagged album %s poor quality, skipped %d pending tracks", workerID, track.AlbumID, skipped),
			}
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
	pcmBytes := clap.Float32ToBytes(pcmSamples)
	clapVec, err := clapClient.GetEmbedding(clapCtx, pcmBytes, int32(sampleRate))
	clapCancel()
	if err != nil {
		errMsg := fmt.Sprintf("clap: %v", err)
		dbMu.Lock()
		requeued, _ := db.RequeueTrackForRetry(sqlDB.Conn, track.ID, maxTrackRetries, errMsg)
		dbMu.Unlock()
		if requeued {
			events <- tui.ActivityEvent{
				Type:       tui.EventInfo,
				Timestamp:  time.Now(),
				Identifier: trackLabel,
				WorkerID:   workerID,
				Message:    fmt.Sprintf("[%s] Retry queued %s: %v", workerID, trackLabel, err),
			}
		} else {
			events <- tui.ActivityEvent{
				Type:       tui.EventAnalysisFailed,
				Timestamp:  time.Now(),
				Identifier: trackLabel,
				WorkerID:   workerID,
				Message:    fmt.Sprintf("[%s] CLAP error %s: %v", workerID, trackLabel, err),
				Error:      err.Error(),
			}
		}
		return
	}

	dbMu.Lock()
	if err := db.SaveEmbedding(sqlDB.Conn, track.ID, clapVec, mfccVec, chromaVec, compositeScore); err != nil {
		errMsg := fmt.Sprintf("save embedding: %v", err)
		requeued, _ := db.RequeueTrackForRetry(sqlDB.Conn, track.ID, maxTrackRetries, errMsg)
		dbMu.Unlock()
		if requeued {
			events <- tui.ActivityEvent{
				Type:       tui.EventInfo,
				Timestamp:  time.Now(),
				Identifier: trackLabel,
				WorkerID:   workerID,
				Message:    fmt.Sprintf("[%s] Retry queued %s: %v", workerID, trackLabel, err),
			}
		} else {
			events <- tui.ActivityEvent{
				Type:       tui.EventAnalysisFailed,
				Timestamp:  time.Now(),
				Identifier: trackLabel,
				WorkerID:   workerID,
				Message:    fmt.Sprintf("[%s] Save embedding error %s: %v", workerID, trackLabel, err),
				Error:      err.Error(),
			}
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
	metrics.RecordAnalyzerCompletion()
}

func resolveAlbum(sqlDB *db.DB, events chan<- tui.ActivityEvent, dbMu *sync.Mutex,
	iaClient *http.Client, metaLimiter *ratelimit.Limiter, workerID, albumID string, metrics *tui.Metrics) {

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
		errMsg := fmt.Sprintf("rate limit: %v", err)
		dbMu.Lock()
		requeued, _ := db.RequeueAlbumForRetry(sqlDB.Conn, albumID, maxAlbumRetries, errMsg)
		dbMu.Unlock()
		if requeued {
			events <- tui.ActivityEvent{
				Type:       tui.EventInfo,
				Timestamp:  time.Now(),
				Identifier: albumID,
				WorkerID:   workerID,
				Message:    fmt.Sprintf("[%s] Retry queued album %s: %v", workerID, albumID, err),
			}
		} else {
			events <- tui.ActivityEvent{
				Type:       tui.EventAlbumFailed,
				Timestamp:  time.Now(),
				Identifier: albumID,
				WorkerID:   workerID,
				Message:    fmt.Sprintf("[%s] Album failed %s: %v", workerID, albumID, err),
				Error:      err.Error(),
			}
		}
		return
	}

	album, err := ia.LookupAlbumMetadata(ctx, iaClient, albumID)
	cancel()
	if err != nil {
		errMsg := fmt.Sprintf("metadata: %v", err)
		dbMu.Lock()
		if isTransientError(errMsg) {
			requeued, _ := db.RequeueAlbumForRetry(sqlDB.Conn, albumID, maxAlbumRetries, errMsg)
			dbMu.Unlock()
			if requeued {
				events <- tui.ActivityEvent{
					Type:       tui.EventInfo,
					Timestamp:  time.Now(),
					Identifier: albumID,
					WorkerID:   workerID,
					Message:    fmt.Sprintf("[%s] Retry queued album %s: %v", workerID, albumID, err),
				}
				return
			}
		} else {
			db.MarkAlbumFailed(sqlDB.Conn, albumID, errMsg)
			dbMu.Unlock()
		}
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
			Duration:    t.Duration,
			DownloadURL: t.DownloadURL,
		}
	}

	dbMu.Lock()
	inserted, err := db.InsertTracks(sqlDB.Conn, albumID, trackInserts)
	if err != nil {
		errMsg := fmt.Sprintf("insert tracks: %v", err)
		requeued, _ := db.RequeueAlbumForRetry(sqlDB.Conn, albumID, maxAlbumRetries, errMsg)
		dbMu.Unlock()
		if requeued {
			events <- tui.ActivityEvent{
				Type:       tui.EventInfo,
				Timestamp:  time.Now(),
				Identifier: albumID,
				WorkerID:   workerID,
				Message:    fmt.Sprintf("[%s] Retry queued album %s: %v", workerID, albumID, err),
			}
		} else {
			events <- tui.ActivityEvent{
				Type:       tui.EventAlbumFailed,
				Timestamp:  time.Now(),
				Identifier: albumID,
				WorkerID:   workerID,
				Message:    fmt.Sprintf("[%s] Insert tracks error %s: %v", workerID, albumID, err),
				Error:      err.Error(),
			}
		}
		return
	}

	if err := db.MarkAlbumResolved(sqlDB.Conn, albumID, album.Title, album.Creator, album.Collection, album.ArtURL, inserted); err != nil {
		errMsg := fmt.Sprintf("mark resolved: %v", err)
		requeued, _ := db.RequeueAlbumForRetry(sqlDB.Conn, albumID, maxAlbumRetries, errMsg)
		dbMu.Unlock()
		if requeued {
			events <- tui.ActivityEvent{
				Type:       tui.EventInfo,
				Timestamp:  time.Now(),
				Identifier: albumID,
				WorkerID:   workerID,
				Message:    fmt.Sprintf("[%s] Retry queued album %s: %v", workerID, albumID, err),
			}
		} else {
			events <- tui.ActivityEvent{
				Type:       tui.EventAlbumFailed,
				Timestamp:  time.Now(),
				Identifier: albumID,
				WorkerID:   workerID,
				Message:    fmt.Sprintf("[%s] Mark resolved error %s: %v", workerID, albumID, err),
				Error:      err.Error(),
			}
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
	metrics.RecordResolverCompletion()
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

	seeded, err := db.SeedCollectionsIfEmpty(sqlDB.Conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error seeding collections: %v\n", err)
		os.Exit(1)
	}
	if seeded > 0 {
		fmt.Fprintf(os.Stderr, "Seeded %d collections\n", seeded)
	}

	logDir := filepath.Dir(cfg.DBPath)
	sidecarProc, clapClient, err := clap.EnsureSidecar(cfg.ClapHost, cfg.ClapPort, cfg.ClapSidecarDir, logDir, func(msg string) {
		fmt.Fprintf(os.Stderr, "%s\n", msg)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "CLAP sidecar error: %v\n", err)
		os.Exit(1)
	}
	if sidecarProc != nil {
		defer sidecarProc.Stop()
	}
	defer clapClient.Close()

	artDir := filepath.Join(filepath.Dir(cfg.DBPath), "art")
	artCache := tui.NewArtCache(artDir)
	metrics := tui.NewMetrics()

	events := tui.NewEventChannel()
	controls := tui.NewControlChannel()
	go runCoordinator(cfg, sqlDB, events, controls, metrics, clapClient)

	m := tui.NewMainModel(cfg, sqlDB.Conn, events, controls, artCache, metrics, cfg.DBPath, artDir)
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runTextSearch(cfg *config.Config) {
	sqlDB, err := db.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	logDir := filepath.Dir(cfg.DBPath)
	sidecarProc, clapClient, err := clap.EnsureSidecar(cfg.ClapHost, cfg.ClapPort, cfg.ClapSidecarDir, logDir, func(msg string) {
		fmt.Fprintf(os.Stderr, "%s\n", msg)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "CLAP sidecar error: %v\n", err)
		os.Exit(1)
	}
	if sidecarProc != nil {
		defer sidecarProc.Stop()
	}
	defer clapClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	textVec, err := clapClient.GetTextEmbedding(ctx, cfg.SearchText)
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "text embedding error: %v\n", err)
		os.Exit(1)
	}

	results, err := db.SearchByText(sqlDB.Conn, textVec, 20)
	if err != nil {
		fmt.Fprintf(os.Stderr, "search error: %v\n", err)
		os.Exit(1)
	}

	if len(results) == 0 {
		fmt.Println("No results found.")
		return
	}

	fmt.Printf("Results for %q (%d matches):\n\n", cfg.SearchText, len(results))
	for i, r := range results {
		fmt.Printf("  %2d. %.4f  %s — %s\n", i+1, r.Similarity, r.Title, r.AlbumTitle)
	}
}

func runHeadless(cfg *config.Config) {
	sqlDB, err := db.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	seeded, err := db.SeedCollectionsIfEmpty(sqlDB.Conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error seeding collections: %v\n", err)
		os.Exit(1)
	}
	if seeded > 0 {
		fmt.Fprintf(os.Stderr, "Seeded %d collections\n", seeded)
	}

	logDir := filepath.Dir(cfg.DBPath)
	sidecarProc, clapClient, err := clap.EnsureSidecar(cfg.ClapHost, cfg.ClapPort, cfg.ClapSidecarDir, logDir, func(msg string) {
		fmt.Fprintf(os.Stderr, "%s\n", msg)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "CLAP sidecar error: %v\n", err)
		os.Exit(1)
	}
	if sidecarProc != nil {
		defer sidecarProc.Stop()
	}
	defer clapClient.Close()

	stats, err := db.GetCombinedStats(sqlDB.Conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error getting stats: %v\n", err)
		os.Exit(1)
	}

	collStats, _ := db.GetCollectionStats(sqlDB.Conn)

	embedCount, err := db.GetEmbeddingCount(sqlDB.Conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error getting embedding count: %v\n", err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)

	collStatsMap := map[string]int{}
	if collStats != nil {
		collStatsMap = map[string]int{
			"total":       collStats.Total,
			"pending":     collStats.Pending,
			"discovering": collStats.Discovering,
			"discovered":  collStats.Discovered,
			"failed":      collStats.Failed,
		}
	}

	enc.Encode(map[string]interface{}{
		"event":   "db_stats",
		"mode":    "headless",
		"db_path": cfg.DBPath,
		"collections": collStatsMap,
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
	metrics := tui.NewMetrics()
	go runCoordinator(cfg, sqlDB, events, controls, metrics, clapClient)

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
