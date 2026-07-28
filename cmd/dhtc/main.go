package main

import (
	"bufio"
	"dhtc/cache"
	"dhtc/config"
	"dhtc/db"
	"dhtc/notifier"
	"dhtc/ui"
	"os"
	"strings"
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
	database, err := db.OpenRepository(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("could not open database")
	}

	cache.PopulateInfoHashCacheFromDatabase(database)

	database.AddToBlacklist(ReadFileLines(cfg.NameBlacklist), "0")
	database.AddToBlacklist(ReadFileLines(cfg.FileBlacklist), "1")

	bootstrapNodes := ReadFileLines(cfg.BootstrapNodeFile)
	bootstrapNodes6 := ReadFileLines(cfg.BootstrapNodeFileIPv6)
	bootstrapNodes6 = append(bootstrapNodes6, ReadFileLines(cfg.IPv6BootstrapNodeFile)...)
	bootstrapNodes = append(bootstrapNodes, cfg.BootstrapIPv4...)
	bootstrapNodes6 = append(bootstrapNodes6, cfg.BootstrapIPv6...)

	if len(bootstrapNodes) == 0 {
		log.Warn().Msg("No bootstrap nodes found in '" + cfg.BootstrapNodeFile + "'.")
		log.Info().Msg("Using default bootstrap nodes.")
		bootstrapNodes = defaultBootstrapNodes
	}

	hub := ui.NewHub()
	go hub.Run()

	var nManager *notifier.Manager
	crawler := ui.NewCrawlerManager(cfg, bootstrapNodes, bootstrapNodes6, database, nManager, hub)
	if !cfg.OnlyWebServer {
		nManager = notifier.SetupNotifiers(cfg)
		crawler = ui.NewCrawlerManager(cfg, bootstrapNodes, bootstrapNodes6, database, nManager, hub)

		if cfg.Statistics {
			go collectStats(database)
		}

		if cfg.CrawlerStartOnLaunch {
			crawler.Start()
		}
	}

	ui.RunWebServer(cfg, database, hub, nManager, crawler)
}
