package main

import (
	"bufio"
	"context"
	"dhtc/cache"
	"dhtc/cluster"
	"dhtc/config"
	"dhtc/db"
	dhtcclient "dhtc/dhtc-client"
	"dhtc/notifier"
	"dhtc/ui"
	"fmt"
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
	if cfg.MaxConcurrentDownloads == 10 {
		cfg.MaxConcurrentDownloads = 2
	}
	if cfg.MaxLeeches == 128 {
		cfg.MaxLeeches = 32
	}
	if strings.TrimSpace(cfg.ClusterToken) == "" {
		log.Fatal().Msg("cluster token is required in worker mode")
	}
	cacheDir := cfg.WorkerCacheDir
	if cacheDir == "" {
		cacheDir = strings.ReplaceAll(cfg.Address, ":", "_")
		cacheDir = strings.ReplaceAll(cacheDir, "/", "_")
		cacheDir = "/tmp/dhtc-worker-" + cacheDir
	}
	queue := cluster.NewWorkerQueue(cfg.WorkerID, cfg.WorkerQueue, cacheDir, func(md dhtcclient.Metadata) {
		cache.InfoHashCacheExpireAfter(string(md.InfoHash), 12*time.Hour)
	})
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	crawler := ui.NewWorkerCrawlerManager(cfg, bootstrapNodes, queue.Enqueue)
	if !cfg.CrawlerScheduleEnabled {
		crawler.Start()
	}
	if cfg.MasterURL != "" {
		queue.SetMaster(cfg.MasterURL, cfg.ClusterToken)
		queue.StartFlushLoop(ctx, 5*time.Minute, cfg.WorkerQueue)
		log.Info().Str("master", cfg.MasterURL).Msg("Worker push mode enabled")
	}

	mux := http.NewServeMux()
	mux.Handle(cluster.QueuePath, queue.Handler(cfg.ClusterToken, cfg.WorkerBatch))
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","role":"worker","queued":%d,"dropped":%d}`, queue.Length(), queue.Dropped())
	})
	server := &http.Server{Addr: cfg.Address, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("worker health server stopped")
			cancel()
		}
	}()
	log.Info().Str("worker_id", cfg.WorkerID).Str("listen", cfg.Address).Int("queue", cfg.WorkerQueue).Int("batch", cfg.WorkerBatch).Msg("Worker started")
	<-ctx.Done()
	crawler.Stop()
	queue.StopFlushLoop()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
}

func splitList(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
}
