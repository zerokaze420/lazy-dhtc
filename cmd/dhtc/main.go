package main

import (
	"bufio"
	"context"
	"crypto/subtle"
	"dhtc/cache"
	"dhtc/cluster"
	"dhtc/config"
	"dhtc/db"
	dhtcclient "dhtc/dhtc-client"
	"dhtc/notifier"
	"dhtc/ui"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var defaultBootstrapNodes = []string{
	"router.bittorrent.com:6881", "router.utorrent.com:6881",
	"dht.transmissionbt.com:6881", "dht.libtorrent.org:25401",
}

func ReadFileLines(filePath string) []string {
	var rVal []string

	if filePath == "" {
		return rVal
	}

	file, err := os.Open(filePath)
	if err != nil {
		return rVal
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rVal = append(rVal, line)
	}

	return rVal
}

func collectStats(database db.Repository) {
	ticker := time.NewTicker(1 * time.Minute)
	for {
		count := database.GetInfoHashCount()
		err := database.InsertStats(db.Stats{
			Timestamp:    time.Now().Truncate(time.Minute),
			TorrentCount: int64(count),
		})
		if err != nil {
			log.Error().Err(err).Msg("could not insert stats")
		}
		<-ticker.C
	}
}

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	cfg := config.ParseArguments()
	bootstrapNodes := ReadFileLines(cfg.BootstrapNodeFile)
	bootstrapNodes = append(bootstrapNodes, cfg.BootstrapIPv4...)
	if len(bootstrapNodes) == 0 {
		bootstrapNodes = defaultBootstrapNodes
	}
	if cfg.NodeRole == "worker" {
		runWorker(cfg, bootstrapNodes)
		return
	}
	database, err := db.OpenRepository(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("could not open database")
	}

	cache.PopulateInfoHashCacheFromDatabase(database)

	database.AddToBlacklist(ReadFileLines(cfg.NameBlacklist), "0")
	database.AddToBlacklist(ReadFileLines(cfg.FileBlacklist), "1")

	hub := ui.NewHub()
	go hub.Run()

	var nManager *notifier.Manager
	if !cfg.OnlyWebServer {
		nManager = notifier.SetupNotifiers(cfg)
		if cfg.Statistics {
			go collectStats(database)
		}
	}
	crawler := ui.NewCrawlerManager(cfg, bootstrapNodes, database, nManager, hub)
	var workerMonitor *cluster.MasterPuller
	if cfg.NodeRole == "master" {
		puller, err := cluster.NewMasterPuller(splitList(cfg.WorkerURLs), cfg.ClusterToken, func(md dhtcclient.Metadata) bool {
			return ui.IngestMetadata(cfg, database, nManager, hub, md)
		})
		if err != nil {
			log.Fatal().Err(err).Msg("invalid master worker configuration")
		}
		workerMonitor = puller
		go puller.Run(context.Background())
	}
	if !cfg.OnlyWebServer && cfg.CrawlerStartOnLaunch && !cfg.CrawlerScheduleEnabled {
		crawler.Start()
	}

	ui.RunWebServer(cfg, database, hub, nManager, crawler, workerMonitor)
}

func runWorker(cfg *config.Configuration, bootstrapNodes []string) {
	if strings.TrimSpace(cfg.ClusterToken) == "" {
		log.Fatal().Msg("cluster token is required in worker mode")
	}
	onAck := func(md dhtcclient.Metadata) {
		cache.InfoHashCacheExpireAfter(string(md.InfoHash), 12*time.Hour)
	}
	var queue *cluster.WorkerQueue
	pushMode := cfg.MasterURL != ""
	if pushMode {
		cacheDir := cfg.WorkerCacheDir
		if cacheDir == "" {
			cacheDir = strings.ReplaceAll(cfg.Address, ":", "_")
			cacheDir = strings.ReplaceAll(cacheDir, "/", "_")
			cacheDir = "/tmp/dhtc-worker-" + cacheDir
		}
		queue = cluster.NewWorkerQueue(cfg.WorkerID, cfg.WorkerQueue, cacheDir, onAck)
	} else {
		queue = cluster.NewWorkerQueue(cfg.WorkerID, cfg.WorkerQueue, "", onAck)
	}
	enqueue := func(md dhtcclient.Metadata) bool {
		log.Debug().Str("name", md.Name).Hex("info_hash", md.InfoHash).Str("worker_id", cfg.WorkerID).Msg("Worker discovered metadata")
		return queue.Enqueue(md)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	crawler := ui.NewWorkerCrawlerManager(cfg, bootstrapNodes, enqueue)
	if pushMode {
		queue.SetMaster(cfg.MasterURL, cfg.ClusterToken)
		queue.StartFlushLoop(ctx, 30*time.Second, cfg.WorkerBatch, cfg.WorkerBatch)
		log.Info().Str("master", cfg.MasterURL).Msg("Worker push mode enabled")
	}
	if !cfg.CrawlerScheduleEnabled {
		crawler.Start()
	}
	go logWorkerMetrics(ctx, cfg.WorkerID, crawler, queue)

	mux := http.NewServeMux()
	mux.Handle(cluster.QueuePath, queue.Handler(cfg.ClusterToken, cfg.WorkerBatch))
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		crawlerStatus := crawler.Status()
		queueStats := queue.Stats()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":                 "ok",
			"role":                   "worker",
			"paused":                 !crawlerStatus.Running,
			"discovered":             crawlerStatus.Discovered,
			"download_succeeded":     crawlerStatus.DownloadSucceeded,
			"dht_output_dropped":     crawlerStatus.DHTOutputDropped,
			"pending_get_peers":      crawlerStatus.PendingGetPeers,
			"get_peers_rejected":     crawlerStatus.GetPeersRejected,
			"get_peers_deduped":      crawlerStatus.GetPeersDeduped,
			"get_peers_send_dropped": crawlerStatus.GetPeersSendDropped,
			"queued":                 queueStats.Queued,
			"queue_dropped":          queueStats.Dropped,
			"dropped":                queueStats.Dropped,
			"flushed":                queueStats.Flushed,
			"flush_batches":          queueStats.FlushBatches,
			"flush_rate_per_second":  queueStats.FlushRatePerSecond,
		})
	})
	mux.HandleFunc("/api/worker/v1/control", func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if cfg.ClusterToken == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(cfg.ClusterToken)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		switch req.Action {
		case "pause":
			if crawler.Stop() {
				log.Info().Str("worker_id", cfg.WorkerID).Msg("Worker paused")
			}
		case "resume":
			if crawler.Start() {
				log.Info().Str("worker_id", cfg.WorkerID).Msg("Worker resumed")
			}
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "running": crawler.Status().Running})
	})
	server := &http.Server{Addr: cfg.Address, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("worker health server stopped")
			cancel()
		}
	}()
	log.Info().Str("worker_id", cfg.WorkerID).Str("listen", cfg.Address).Int("queue", cfg.WorkerQueue).Int("batch", cfg.WorkerBatch).Bool("push_mode", pushMode).Msg("Worker started")
	<-ctx.Done()
	crawler.Stop()
	queue.StopFlushLoop()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
}

func logWorkerMetrics(ctx context.Context, workerID string, crawler *ui.CrawlerManager, queue *cluster.WorkerQueue) {
	const interval = time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var previousDiscovered uint64
	var previousDownloads uint64
	var previousDHTDropped uint64
	var previousGetPeersRejected uint64
	var previousGetPeersDeduped uint64
	var previousGetPeersSendDropped uint64
	var previousFlushed uint64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		crawlerStatus := crawler.Status()
		queueStats := queue.Stats()
		log.Info().
			Str("worker_id", workerID).
			Uint64("discovered", crawlerStatus.Discovered).
			Float64("discovered_per_second", float64(crawlerStatus.Discovered-previousDiscovered)/interval.Seconds()).
			Uint64("download_succeeded", crawlerStatus.DownloadSucceeded).
			Float64("downloads_per_second", float64(crawlerStatus.DownloadSucceeded-previousDownloads)/interval.Seconds()).
			Int("queued", queueStats.Queued).
			Uint64("queue_dropped", queueStats.Dropped).
			Uint64("flushed", queueStats.Flushed).
			Float64("flush_per_second", float64(queueStats.Flushed-previousFlushed)/interval.Seconds()).
			Uint64("flush_batches", queueStats.FlushBatches).
			Uint64("dht_output_dropped", crawlerStatus.DHTOutputDropped).
			Uint64("dht_output_dropped_interval", crawlerStatus.DHTOutputDropped-previousDHTDropped).
			Int("pending_get_peers", crawlerStatus.PendingGetPeers).
			Uint64("get_peers_rejected", crawlerStatus.GetPeersRejected).
			Uint64("get_peers_rejected_interval", crawlerStatus.GetPeersRejected-previousGetPeersRejected).
			Uint64("get_peers_deduped", crawlerStatus.GetPeersDeduped).
			Uint64("get_peers_deduped_interval", crawlerStatus.GetPeersDeduped-previousGetPeersDeduped).
			Uint64("get_peers_send_dropped", crawlerStatus.GetPeersSendDropped).
			Uint64("get_peers_send_dropped_interval", crawlerStatus.GetPeersSendDropped-previousGetPeersSendDropped).
			Msg("Worker metrics")
		previousDiscovered = crawlerStatus.Discovered
		previousDownloads = crawlerStatus.DownloadSucceeded
		previousDHTDropped = crawlerStatus.DHTOutputDropped
		previousGetPeersRejected = crawlerStatus.GetPeersRejected
		previousGetPeersDeduped = crawlerStatus.GetPeersDeduped
		previousGetPeersSendDropped = crawlerStatus.GetPeersSendDropped
		previousFlushed = queueStats.Flushed
	}
}

func splitList(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
}
