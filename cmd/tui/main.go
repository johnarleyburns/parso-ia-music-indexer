package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
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
	"github.com/johnarleyburns/parso-ia-music-indexer/internal/playlist"
	ratelimit "github.com/johnarleyburns/parso-ia-music-indexer/internal/rate"
	"github.com/johnarleyburns/parso-ia-music-indexer/internal/tui"
	"golang.org/x/time/rate"
)

const (
	maxAlbumRetries = 3
	stuckTrackAge   = 10 * time.Minute
)

func setupFileLogging(cfg *config.Config) *os.File {
	logDir := filepath.Dir(cfg.DBPath)
	os.MkdirAll(logDir, 0755)
	f, err := os.OpenFile(filepath.Join(logDir, "debug.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil
	}
	log.SetOutput(f)
	return f
}

func isTransientError(errMsg string) bool {
	transient := []string{
		"stream:", "rate limit:", "clap:",
		"timeout", "connection refused", "connection reset",
		"i/o timeout", "no such host", "EOF",
		"503", "429", "502", "500", "504",
	}
	lower := strings.ToLower(errMsg)
	for _, pattern := range transient {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

func safeGo(fn func(), events chan<- tui.ActivityEvent, label string) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("PANIC in goroutine %q: %v\n%s", label, r, debug.Stack())
				events <- tui.ActivityEvent{
					Type:      tui.EventInfo,
					Timestamp: time.Now(),
					Message:   fmt.Sprintf("Worker %q panicked: %v (check debug.log for details)", label, r),
					Error:     fmt.Sprintf("%v", r),
				}
			}
		}()
		fn()
	}()
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

	cleanerCount := 0
	nextCleanerID := 1
	cleanerStopChs := make(map[int]chan struct{})

	enhancerCount := 0
	nextEnhancerID := 1
	enhancerStopChs := make(map[int]chan struct{})

	dbMu := &sync.Mutex{}
	iaClient := ia.NewClient(60 * time.Second)
	iaClient.Transport = tui.NewInstrumentedTransport(metrics)
	iaLimiter := ratelimit.NewLimiter(cfg.IAApiRate)
	bwLimiter := rate.NewLimiter(rate.Limit(cfg.ThrottleBPS), cfg.ThrottleBPS)

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
		Message:   fmt.Sprintf("Application started. %s. [s] coordinator  [r] resolvers  [w] analyzers  [c] cleaners  [e] enhancers", collMsg),
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
				safeGo(func() { coordinatorLoop(cfg, sqlDB, events, coordStopCh, metrics, iaLimiter) }, events, "coordinator")
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
				safeGo(func() { albumResolverLoop(sqlDB, events, stopCh, dbMu, iaClient, iaLimiter, rID, metrics) }, events, rID)
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
				safeGo(func() { workerLoop(cfg, sqlDB, events, stopCh, dbMu, clapClient, iaClient, wID, metrics, iaLimiter, bwLimiter) }, events, wID)
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

	case tui.CmdAddCleaner:
		cleanerCount++
		cID := fmt.Sprintf("cleaner-%d", nextCleanerID)
		nextCleanerID++
		stopCh := make(chan struct{})
		cleanerStopChs[nextCleanerID-1] = stopCh
			events <- tui.ActivityEvent{
				Type:       tui.EventWorkerStarted,
				Timestamp:  time.Now(),
				Identifier: cID,
				WorkerID:   cID,
				Message:    fmt.Sprintf("Cleaner %s started (pool: %d)", cID, cleanerCount),
			}
			safeGo(func() { cleanupWorkerLoop(sqlDB, events, stopCh, dbMu, iaClient, iaLimiter, cID, metrics) }, events, cID)
		case tui.CmdRemoveCleaner:
		if cleanerCount > 0 {
			cleanerCount--
			for id, ch := range cleanerStopChs {
				close(ch)
				delete(cleanerStopChs, id)
				break
			}
			events <- tui.ActivityEvent{
				Type:      tui.EventWorkerStopped,
				Timestamp: time.Now(),
				Message:   fmt.Sprintf("Cleaner removed (pool: %d)", cleanerCount),
			}
		}

	case tui.CmdAddEnhancer:
		enhancerCount++
		eID := fmt.Sprintf("enhancer-%d", nextEnhancerID)
		nextEnhancerID++
		stopCh := make(chan struct{})
		enhancerStopChs[nextEnhancerID-1] = stopCh
			events <- tui.ActivityEvent{
				Type:       tui.EventWorkerStarted,
				Timestamp:  time.Now(),
				Identifier: eID,
				WorkerID:   eID,
				Message:    fmt.Sprintf("Enhancer %s started (pool: %d)", eID, enhancerCount),
			}
			safeGo(func() { tagEnhancerLoop(sqlDB, events, stopCh, dbMu, iaClient, iaLimiter, eID, metrics) }, events, eID)
		case tui.CmdRemoveEnhancer:
		if enhancerCount > 0 {
			enhancerCount--
			for id, ch := range enhancerStopChs {
				close(ch)
				delete(enhancerStopChs, id)
				break
			}
			events <- tui.ActivityEvent{
				Type:      tui.EventWorkerStopped,
				Timestamp: time.Now(),
				Message:   fmt.Sprintf("Enhancer removed (pool: %d)", enhancerCount),
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
		for _, ch := range cleanerStopChs {
			close(ch)
		}
		for _, ch := range enhancerStopChs {
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
			resetTracks, errT := db.ResetStuckTracks(sqlDB.Conn, stuckTrackAge)
			resetAlbums, errA := db.ResetStuckAlbums(sqlDB.Conn, stuckTrackAge)
			dbMu.Unlock()
			if errT == nil && resetTracks > 0 {
				events <- tui.ActivityEvent{
					Type:      tui.EventInfo,
					Timestamp: time.Now(),
					Message:   fmt.Sprintf("Reset %d stuck tracks back to pending", resetTracks),
				}
			}
			if errA == nil && resetAlbums > 0 {
				events <- tui.ActivityEvent{
					Type:      tui.EventInfo,
					Timestamp: time.Now(),
					Message:   fmt.Sprintf("Reset %d stuck albums back to pending", resetAlbums),
				}
			}
		}
	}
}

func coordinatorLoop(cfg *config.Config, sqlDB *db.DB, events chan<- tui.ActivityEvent, stopCh <-chan struct{}, metrics *tui.Metrics, iaLimiter *ratelimit.Limiter) {
	client := ia.NewClient(60 * time.Second)
	client.Transport = tui.NewInstrumentedTransport(metrics)

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

		if coll.SourceType == "playlist" || coll.SourceType == "simplelist" {
			events <- tui.ActivityEvent{
				Type:         tui.EventCollectionStarted,
				Timestamp:    time.Now(),
				CollectionID: coll.CollectionID,
				Message:      fmt.Sprintf("[%d/%d] Syncing playlist %q", i+1, len(collections), coll.Title),
			}

			db.MarkCollectionDiscovering(sqlDB.Conn, coll.CollectionID)

			count, syncErr := playlist.SyncPlaylist(sqlDB, client, coll, func(state string, current, total int) {
				log.Printf("[coordinator] playlist sync %s: %s (%d/%d)", coll.CollectionID, state, current, total)
			})
			if syncErr != nil {
				events <- tui.ActivityEvent{
					Type:         tui.EventCollectionFailed,
					Timestamp:    time.Now(),
					CollectionID: coll.CollectionID,
					Message:      fmt.Sprintf("Playlist %q sync failed: %v", coll.Title, syncErr),
					Error:        syncErr.Error(),
				}
				db.MarkCollectionFailed(sqlDB.Conn, coll.CollectionID, syncErr.Error())
				continue
			}

			totalAlbums += count

			events <- tui.ActivityEvent{
				Type:         tui.EventCollectionCompleted,
				Timestamp:    time.Now(),
				CollectionID: coll.CollectionID,
				Message:      fmt.Sprintf("[%d/%d] Playlist %q synced: %d items", i+1, len(collections), coll.Title, count),
				Count:        count,
			}
			continue
		}

		discovered, err := discoverCollection(cfg, sqlDB, client, iaLimiter, events, stopCh, coll, i+1, len(collections), denylist)
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

func discoverCollection(cfg *config.Config, sqlDB *db.DB, client *http.Client, iaLimiter *ratelimit.Limiter,
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
		if err := iaLimiter.Wait(ctx); err != nil {
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
	dbMu *sync.Mutex, iaClient *http.Client, iaLimiter *ratelimit.Limiter, resolverID string, metrics *tui.Metrics) {
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

		resolveAlbum(sqlDB, events, stopCh, dbMu, iaClient, iaLimiter, resolverID, albumID, metrics)
		sleepOrStop(1*time.Second, stopCh)
	}
}

func cleanupWorkerLoop(sqlDB *db.DB, events chan<- tui.ActivityEvent, stopCh <-chan struct{},
	dbMu *sync.Mutex, iaClient *http.Client, iaLimiter *ratelimit.Limiter, workerID string, metrics *tui.Metrics) {
	for {
		select {
		case <-stopCh:
			return
		default:
		}

		dbMu.Lock()
		album, err := db.ClaimUnprecheckedAlbum(sqlDB.Conn)
		dbMu.Unlock()
		if err != nil {
			events <- tui.ActivityEvent{
				Type:      tui.EventInfo,
				Timestamp: time.Now(),
				WorkerID:  workerID,
				Message:   fmt.Sprintf("[%s] Claim unprechecked album error: %v", workerID, err),
				Error:     err.Error(),
			}
			sleepOrStop(30*time.Second, stopCh)
			continue
		}

		if album == nil {
			sleepOrStop(30*time.Second, stopCh)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := iaLimiter.Wait(ctx); err != nil {
			cancel()
			events <- tui.ActivityEvent{
				Type:      tui.EventInfo,
				Timestamp: time.Now(),
				WorkerID:  workerID,
				Message:   fmt.Sprintf("[%s] Rate limit wait error: %v", workerID, err),
				Error:     err.Error(),
			}
			sleepOrStop(30*time.Second, stopCh)
			continue
		}

		meta, err := ia.LookupAlbumMetadata(ctx, iaClient, album.IAIdentifier)
		cancel()
		if err != nil {
			events <- tui.ActivityEvent{
				Type:       tui.EventInfo,
				Timestamp:  time.Now(),
				Identifier: album.IAIdentifier,
				WorkerID:   workerID,
				Message:    fmt.Sprintf("[%s] Cleanup metadata error %s: %v", workerID, album.IAIdentifier, err),
				Error:      err.Error(),
			}
			sleepOrStop(5*time.Second, stopCh)
			continue
		}

		dbMu.Lock()
		if meta.AccessRestrictedItem {
			skipped, ferr := db.FailAlbumAndPendingTracksUnavailable(sqlDB.Conn, album.IAIdentifier, "access-restricted item (detected by cleanup)")
			dbMu.Unlock()
			if ferr != nil {
				events <- tui.ActivityEvent{
					Type:       tui.EventInfo,
					Timestamp:  time.Now(),
					Identifier: album.IAIdentifier,
					WorkerID:   workerID,
					Message:    fmt.Sprintf("[%s] Fail album error %s: %v", workerID, album.IAIdentifier, ferr),
					Error:      ferr.Error(),
				}
			} else {
				events <- tui.ActivityEvent{
					Type:       tui.EventAlbumUnavailable,
					Timestamp:  time.Now(),
					Identifier: album.IAIdentifier,
					WorkerID:   workerID,
					Message:    fmt.Sprintf("[%s] Restricted album %s (%s), %d pending tracks unavailable", workerID, album.Title, album.IAIdentifier, skipped),
				}
			}
		} else {
			err = db.MarkAlbumPrechecked(sqlDB.Conn, album.IAIdentifier)
			dbMu.Unlock()
			if err != nil {
				events <- tui.ActivityEvent{
					Type:       tui.EventInfo,
					Timestamp:  time.Now(),
					Identifier: album.IAIdentifier,
					WorkerID:   workerID,
					Message:    fmt.Sprintf("[%s] Mark prechecked error %s: %v", workerID, album.IAIdentifier, err),
					Error:      err.Error(),
				}
			} else {
				events <- tui.ActivityEvent{
					Type:       tui.EventAlbumPrechecked,
					Timestamp:  time.Now(),
					Identifier: album.IAIdentifier,
					WorkerID:   workerID,
					Message:    fmt.Sprintf("[%s] Clean %s (%d tracks available)", workerID, album.Title, album.TrackCount),
				}
			}
		}

		metrics.RecordCleanerCompletion()
		sleepOrStop(1*time.Second, stopCh)
	}
}

func runSeedCollections(cfg *config.Config) {
	sqlDB, err := db.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	n, err := db.SeedCollections(sqlDB.Conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed error: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Seeded %d new collections (INSERT OR IGNORE)\n", n)
}

func main() {
	cfg := config.Parse()

	logFile := setupFileLogging(cfg)
	if logFile != nil {
		defer logFile.Close()
	}

	if cfg.SeedCollections {
		runSeedCollections(cfg)
		return
	}

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
	dbMu *sync.Mutex, clapClient clap.CLAPClient, iaClient *http.Client, workerID string, metrics *tui.Metrics,
	iaLimiter *ratelimit.Limiter, bwLimiter *rate.Limiter) {
	batchSize := 2
	consecutiveFailures := 0

	for {
		select {
		case <-stopCh:
			return
		default:
		}

		if consecutiveFailures > 0 {
			backoff := time.Duration(1<<uint(consecutiveFailures-1)) * 5 * time.Second
			if backoff > 1*time.Hour {
				backoff = 1 * time.Hour
			}
			if backoff >= 1*time.Hour {
				events <- tui.ActivityEvent{
					Type:      tui.EventWorkerStopped,
					Timestamp: time.Now(),
					WorkerID:  workerID,
					Message:   fmt.Sprintf("[%s] Stopped: %d consecutive transient failures (backoff reached 1 hour limit)", workerID, consecutiveFailures),
					Error:     fmt.Sprintf("%d consecutive failures", consecutiveFailures),
				}
				return
			}
			events <- tui.ActivityEvent{
				Type:      tui.EventInfo,
				Timestamp: time.Now(),
				WorkerID:  workerID,
				Message:   fmt.Sprintf("[%s] Backing off %s after %d consecutive failures", workerID, backoff, consecutiveFailures),
			}
			sleepOrStop(backoff, stopCh)
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
			consecutiveFailures++
			continue
		}

		if len(tracks) > 0 {
			for _, track := range tracks {
				select {
				case <-stopCh:
					return
				default:
				}
				ok := analyzeTrack(cfg, sqlDB, events, dbMu, clapClient, iaClient, workerID, track, metrics, iaLimiter, bwLimiter, stopCh)
				if ok {
					consecutiveFailures = 0
				} else {
					consecutiveFailures++
					break
				}
			}
			continue
		}

		sleepOrStop(5*time.Second, stopCh)
	}
}

func tagEnhancerLoop(sqlDB *db.DB, events chan<- tui.ActivityEvent, stopCh <-chan struct{},
	dbMu *sync.Mutex, iaClient *http.Client, iaLimiter *ratelimit.Limiter, workerID string, metrics *tui.Metrics) {
	for {
		select {
		case <-stopCh:
			return
		default:
		}

		dbMu.Lock()
		album, err := db.ClaimUntaggedAlbum(sqlDB.Conn)
		dbMu.Unlock()
		if err != nil {
			events <- tui.ActivityEvent{
				Type:      tui.EventInfo,
				Timestamp: time.Now(),
				WorkerID:  workerID,
				Message:   fmt.Sprintf("[%s] Claim untagged album error: %v", workerID, err),
				Error:     err.Error(),
			}
			sleepOrStop(10*time.Second, stopCh)
			continue
		}

		if album == nil {
			sleepOrStop(15*time.Second, stopCh)
			continue
		}

		events <- tui.ActivityEvent{
			Type:       tui.EventAnalysisStarted,
			Timestamp:  time.Now(),
			Identifier: album.IAIdentifier,
			WorkerID:   workerID,
			Message:    fmt.Sprintf("[%s] Enhancing tags for %s (%d tracks)", workerID, album.IAIdentifier, album.TrackCount),
		}

		subjects := album.Subjects
		genres := album.Genres

		if subjects == "" || genres == "" {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := iaLimiter.Wait(ctx); err != nil {
				cancel()
				events <- tui.ActivityEvent{
					Type:       tui.EventInfo,
					Timestamp:  time.Now(),
					Identifier: album.IAIdentifier,
					WorkerID:   workerID,
					Message:    fmt.Sprintf("[%s] Rate limit wait for %s, will retry", workerID, album.IAIdentifier),
				}
				sleepOrStop(30*time.Second, stopCh)
				continue
			}
			meta, metaErr := ia.LookupAlbumMetadata(ctx, iaClient, album.IAIdentifier)
			cancel()
			if metaErr != nil {
				errMsg := fmt.Sprintf("metadata: %v", metaErr)
				if isTransientError(errMsg) {
					events <- tui.ActivityEvent{
						Type:       tui.EventInfo,
						Timestamp:  time.Now(),
						Identifier: album.IAIdentifier,
						WorkerID:   workerID,
						Message:    fmt.Sprintf("[%s] Transient metadata error %s: %v, will retry", workerID, album.IAIdentifier, metaErr),
					}
					sleepOrStop(5*time.Second, stopCh)
				} else {
					events <- tui.ActivityEvent{
						Type:       tui.EventAnalysisFailed,
						Timestamp:  time.Now(),
						Identifier: album.IAIdentifier,
						WorkerID:   workerID,
						Message:    fmt.Sprintf("[%s] Metadata fetch error %s: %v", workerID, album.IAIdentifier, metaErr),
						Error:      metaErr.Error(),
					}
				}
				continue
			}
			subjects = strings.Join(meta.Subjects, ", ")
			genres = strings.Join(meta.Genres, ", ")

			dbMu.Lock()
			if err := db.UpdateAlbumMetadata(sqlDB.Conn, album.IAIdentifier, subjects, genres); err != nil {
				dbMu.Unlock()
				events <- tui.ActivityEvent{
					Type:       tui.EventInfo,
					Timestamp:  time.Now(),
					Identifier: album.IAIdentifier,
					WorkerID:   workerID,
					Message:    fmt.Sprintf("[%s] Storing album metadata error %s: %v", workerID, album.IAIdentifier, err),
					Error:      err.Error(),
				}
				continue
			}
			dbMu.Unlock()
		}

		var subjectsSlice, genresSlice []string
		if subjects != "" {
			subjectsSlice = strings.Split(subjects, ", ")
		}
		if genres != "" {
			genresSlice = strings.Split(genres, ", ")
		}

		tags := db.GenerateTags(album.IAIdentifier, album.Title, album.Creator, subjectsSlice, genresSlice)
		if tags == "" {
			continue
		}

		dbMu.Lock()
		if err := db.UpdateTracksTags(sqlDB.Conn, album.IAIdentifier, tags); err != nil {
			dbMu.Unlock()
			events <- tui.ActivityEvent{
				Type:       tui.EventInfo,
				Timestamp:  time.Now(),
				Identifier: album.IAIdentifier,
				WorkerID:   workerID,
				Message:    fmt.Sprintf("[%s] Storing track tags error %s: %v", workerID, album.IAIdentifier, err),
				Error:      err.Error(),
			}
			continue
		}
		dbMu.Unlock()

		events <- tui.ActivityEvent{
			Type:       tui.EventAnalysisComplete,
			Timestamp:  time.Now(),
			Identifier: album.IAIdentifier,
			WorkerID:   workerID,
			Message:    fmt.Sprintf("[%s] Enhanced %s with %d tags (%d tracks)", workerID, album.IAIdentifier, len(strings.Split(tags, ", ")), album.TrackCount),
		}
		metrics.RecordEnhancerCompletion()
	}
}

func analyzeTrack(cfg *config.Config, sqlDB *db.DB, events chan<- tui.ActivityEvent,
	dbMu *sync.Mutex, clapClient clap.CLAPClient, iaClient *http.Client, workerID string, track db.ClaimedTrack, metrics *tui.Metrics,
	iaLimiter *ratelimit.Limiter, bwLimiter *rate.Limiter, stopCh <-chan struct{}) bool {

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
	if err := iaLimiter.Wait(ctx); err != nil {
		cancel()
		now := time.Now().UTC().Format(time.RFC3339)
		dbMu.Lock()
		sqlDB.Conn.Exec(`UPDATE tracks SET status='pending', updated_at=? WHERE id=?`, now, track.ID)
		dbMu.Unlock()
		events <- tui.ActivityEvent{
			Type:       tui.EventInfo,
			Timestamp:  time.Now(),
			Identifier: trackLabel,
			WorkerID:   workerID,
			Message:    fmt.Sprintf("[%s] Rate limit wait for %s, will retry", workerID, trackLabel),
		}
		sleepOrStop(30*time.Second, stopCh)
		return true
	}
	mp3Data, err := audio.StreamAudioFromURL(ctx, iaClient, track.DownloadURL, cfg.MaxBytes, bwLimiter)
	cancel()
	if err != nil {
		errMsg := fmt.Sprintf("stream: %v", err)
		dbMu.Lock()
		if strings.Contains(err.Error(), "unexpected status 401") || strings.Contains(err.Error(), "unexpected status 403") {
			reason := fmt.Sprintf("access-restricted: %v", err)
			skipped, _ := db.FailAlbumAndPendingTracksUnavailable(sqlDB.Conn, track.AlbumID, reason)
			db.MarkTrackUnavailable(sqlDB.Conn, track.ID, reason)
			dbMu.Unlock()
			events <- tui.ActivityEvent{
				Type:       tui.EventAlbumUnavailable,
				Timestamp:  time.Now(),
				Identifier: track.AlbumID,
				WorkerID:   workerID,
				Message:    fmt.Sprintf("[%s] Album access-restricted %s: %v (%d pending tracks unavailable)", workerID, track.AlbumID, err, skipped),
				Error:      err.Error(),
			}
		} else if isTransientError(errMsg) {
			requeued, _ := db.RequeueTrackForRetry(sqlDB.Conn, track.ID, 3, errMsg)
			dbMu.Unlock()
			if requeued {
				events <- tui.ActivityEvent{
					Type:       tui.EventInfo,
					Timestamp:  time.Now(),
					Identifier: trackLabel,
					WorkerID:   workerID,
					Message:    fmt.Sprintf("[%s] Requeued %s: %v", workerID, trackLabel, err),
				}
			} else {
				events <- tui.ActivityEvent{
					Type:       tui.EventAnalysisFailed,
					Timestamp:  time.Now(),
					Identifier: trackLabel,
					WorkerID:   workerID,
					Message:    fmt.Sprintf("[%s] Failed %s (max retries): %v", workerID, trackLabel, err),
					Error:      err.Error(),
				}
			}
		} else {
			db.MarkTrackFailed(sqlDB.Conn, track.ID, errMsg)
			dbMu.Unlock()
			events <- tui.ActivityEvent{
				Type:       tui.EventAnalysisFailed,
				Timestamp:  time.Now(),
				Identifier: trackLabel,
				WorkerID:   workerID,
				Message:    fmt.Sprintf("[%s] Failed %s: %v", workerID, trackLabel, err),
				Error:      err.Error(),
			}
		}
		return false
	}

	pcmSamples, sampleRate, err := audio.DecodeMP3(mp3Data)
	if err != nil {
		reason := fmt.Sprintf("decode: %v", err)
		dbMu.Lock()
		skipped, _ := db.FlagAlbumPoorQuality(sqlDB.Conn, track.ID, track.AlbumID, reason)
		dbMu.Unlock()
		events <- tui.ActivityEvent{
			Type:       tui.EventAnalysisUnavailable,
			Timestamp:  time.Now(),
			Identifier: trackLabel,
			WorkerID:   workerID,
			Message:    fmt.Sprintf("[%s] Failed %s: %v", workerID, trackLabel, err),
			Error:      err.Error(),
		}
		if skipped > 0 {
			events <- tui.ActivityEvent{
				Type:       tui.EventAlbumUnavailable,
				Timestamp:  time.Now(),
				Identifier: track.AlbumID,
				WorkerID:   workerID,
				Message:    fmt.Sprintf("[%s] Flagged album %s poor quality, skipped %d pending tracks", workerID, track.AlbumID, skipped),
			}
		}
		return true
	}

	snrDB := audio.CalculateSNR(pcmSamples)
	centroidHz := audio.CalculateSpectralCentroid(pcmSamples, sampleRate)
	crestFactor := audio.CalculateCrestFactor(pcmSamples)
	compositeScore := audio.CalculateCompositeScore(snrDB, centroidHz, crestFactor)

	if compositeScore < audio.QualityUnusable {
		dbMu.Lock()
		db.MarkTrackUnavailable(sqlDB.Conn, track.ID, fmt.Sprintf("low quality: score=%.3f snr=%.1f centroid=%.0f crest=%.1f", compositeScore, snrDB, centroidHz, crestFactor))
		dbMu.Unlock()
		events <- tui.ActivityEvent{
			Type:         tui.EventAnalysisUnavailable,
			Timestamp:    time.Now(),
			Identifier:   trackLabel,
			WorkerID:     workerID,
			Message:      fmt.Sprintf("[%s] Low quality %s: score=%.3f", workerID, trackLabel, compositeScore),
			QualityScore: compositeScore,
		}
		return true
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
		db.MarkTrackFailed(sqlDB.Conn, track.ID, errMsg)
		dbMu.Unlock()
		events <- tui.ActivityEvent{
			Type:       tui.EventAnalysisFailed,
			Timestamp:  time.Now(),
			Identifier: trackLabel,
			WorkerID:   workerID,
			Message:    fmt.Sprintf("[%s] CLAP error %s: %v", workerID, trackLabel, err),
			Error:      err.Error(),
		}
		return false
	}

	dbMu.Lock()
	if err := db.SaveEmbedding(sqlDB.Conn, track.ID, clapVec, mfccVec, chromaVec, compositeScore); err != nil {
		errMsg := fmt.Sprintf("save embedding: %v", err)
		db.MarkTrackFailed(sqlDB.Conn, track.ID, errMsg)
		dbMu.Unlock()
		events <- tui.ActivityEvent{
			Type:       tui.EventAnalysisFailed,
			Timestamp:  time.Now(),
			Identifier: trackLabel,
			WorkerID:   workerID,
			Message:    fmt.Sprintf("[%s] Save embedding error %s: %v", workerID, trackLabel, err),
			Error:      err.Error(),
		}
		return false
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
	return true
}

func resolveAlbum(sqlDB *db.DB, events chan<- tui.ActivityEvent, stopCh <-chan struct{},
	dbMu *sync.Mutex, iaClient *http.Client, iaLimiter *ratelimit.Limiter, workerID, albumID string, metrics *tui.Metrics) {

	events <- tui.ActivityEvent{
		Type:       tui.EventAlbumResolving,
		Timestamp:  time.Now(),
		Identifier: albumID,
		WorkerID:   workerID,
		Message:    fmt.Sprintf("[%s] Resolving album: %s", workerID, albumID),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := iaLimiter.Wait(ctx); err != nil {
		cancel()
		now := time.Now().UTC().Format(time.RFC3339)
		dbMu.Lock()
		sqlDB.Conn.Exec(`UPDATE albums SET status='pending', updated_at=? WHERE ia_identifier=?`, now, albumID)
		dbMu.Unlock()
		events <- tui.ActivityEvent{
			Type:       tui.EventInfo,
			Timestamp:  time.Now(),
			Identifier: albumID,
			WorkerID:   workerID,
			Message:    fmt.Sprintf("[%s] Rate limit wait for %s, will retry", workerID, albumID),
		}
		sleepOrStop(30*time.Second, stopCh)
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
			db.MarkAlbumUnavailable(sqlDB.Conn, albumID, errMsg)
			dbMu.Unlock()
		}
		events <- tui.ActivityEvent{
			Type:       tui.EventAlbumUnavailable,
			Timestamp:  time.Now(),
			Identifier: albumID,
			WorkerID:   workerID,
			Message:    fmt.Sprintf("[%s] Album unavailable %s: %v", workerID, albumID, err),
			Error:      err.Error(),
		}
		return
	}

	if isMusic, reason := ia.IsMusicContent(album); !isMusic {
		dbMu.Lock()
		db.MarkAlbumUnavailable(sqlDB.Conn, albumID, reason)
		dbMu.Unlock()
		events <- tui.ActivityEvent{
			Type:       tui.EventAlbumUnavailable,
			Timestamp:  time.Now(),
			Identifier: albumID,
			WorkerID:   workerID,
			Message:    fmt.Sprintf("[%s] Album %s: %s", workerID, albumID, reason),
		}
		return
	}

	if len(album.Tracks) == 0 {
		dbMu.Lock()
		db.MarkAlbumUnavailable(sqlDB.Conn, albumID, "no acceptable MP3 tracks found")
		dbMu.Unlock()
		events <- tui.ActivityEvent{
			Type:       tui.EventAlbumUnavailable,
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

	subjectsJoined := strings.Join(album.Subjects, ", ")
	genresJoined := strings.Join(album.Genres, ", ")
	if err := db.UpdateAlbumMetadata(sqlDB.Conn, albumID, subjectsJoined, genresJoined); err != nil {
		events <- tui.ActivityEvent{
			Type:       tui.EventInfo,
			Timestamp:  time.Now(),
			Identifier: albumID,
			WorkerID:   workerID,
			Message:    fmt.Sprintf("[%s] Storing album metadata %s: %v", workerID, albumID, err),
			Error:      err.Error(),
		}
	}

	tags := db.GenerateTags(albumID, album.Title, album.Creator, album.Subjects, album.Genres)
	if tags != "" {
		if err := db.UpdateTracksTags(sqlDB.Conn, albumID, tags); err != nil {
			events <- tui.ActivityEvent{
				Type:       tui.EventInfo,
				Timestamp:  time.Now(),
				Identifier: albumID,
				WorkerID:   workerID,
				Message:    fmt.Sprintf("[%s] Storing track tags %s: %v", workerID, albumID, err),
				Error:      err.Error(),
			}
		}
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
		log.Printf("[sidecar] %s", msg)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "CLAP sidecar error: %v\n", err)
		os.Exit(1)
	}
	if sidecarProc != nil {
		defer sidecarProc.Stop()
	}
	defer clapClient.Close()

	metrics := tui.NewMetrics()

	events := tui.NewEventChannel()
	controls := tui.NewControlChannel()
	safeGo(func() { runCoordinator(cfg, sqlDB, events, controls, metrics, clapClient) }, events, "runCoordinator")

	m := tui.NewMainModel(cfg, sqlDB.Conn, events, controls, metrics, cfg.DBPath)
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
		log.Printf("[sidecar] %s", msg)
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

	results, err := db.SearchByText(sqlDB.Conn, textVec, cfg.SearchText, 20)
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
		fmt.Printf("  %2d. %.4f (clap=%.4f pill=%.4f)  %s — %s\n", i+1, r.Similarity, r.CLAPSimilarity, r.PillScore, r.Title, r.AlbumTitle)
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
		log.Printf("[sidecar] %s", msg)
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
			"total":       stats.Albums.Total,
			"pending":     stats.Albums.Pending,
			"resolving":   stats.Albums.Resolving,
			"resolved":    stats.Albums.Resolved,
			"failed":      stats.Albums.Failed,
			"unavailable": stats.Albums.Unavailable,
		},
		"tracks": map[string]int{
			"total":       stats.Tracks.Total,
			"pending":     stats.Tracks.Pending,
			"processing":  stats.Tracks.Processing,
			"completed":   stats.Tracks.Completed,
			"failed":      stats.Tracks.Failed,
			"unavailable": stats.Tracks.Unavailable,
			"untagged":    stats.Tracks.UntaggedCount,
		},
		"embeddings": map[string]int{
			"count": embedCount,
		},
	})

	events := tui.NewEventChannel()
	controls := tui.NewControlChannel()
	metrics := tui.NewMetrics()
	safeGo(func() { runCoordinator(cfg, sqlDB, events, controls, metrics, clapClient) }, events, "runCoordinator-headless")

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
