package ui

import (
	"dhtc/cache"
	"dhtc/config"
	"dhtc/db"
	dhtcclient "dhtc/dhtc-client"
	"dhtc/notifier"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type CrawlerStatus struct {
	Running       bool
	StartedAt     time.Time
	AutoStopAt    time.Time
	Threads       int
	AutoStopAfter time.Duration
}

type CrawlerManager struct {
	mu              sync.Mutex
	configuration   *config.Configuration
	database        db.Repository
	notifier        *notifier.Manager
	hub             *Hub
	bootstrapNodes4 []string
	bootstrapNodes6 []string
	stop            chan struct{}
	startedAt       time.Time
	autoStopAt      time.Time
	threads         int
	running         bool
}

func NewCrawlerManager(configuration *config.Configuration, bootstrapNodes4, bootstrapNodes6 []string, database db.Repository, nManager *notifier.Manager, hub *Hub) *CrawlerManager {
	return &CrawlerManager{
		configuration:   configuration,
		database:        database,
		notifier:        nManager,
		hub:             hub,
		bootstrapNodes4: bootstrapNodes4,
		bootstrapNodes6: bootstrapNodes6,
	}
}

func (m *CrawlerManager) Start() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running || m.configuration.OnlyWebServer {
		return false
	}

	threads := m.configuration.CrawlerThreads
	if threads < 1 {
		threads = 1
	}

	stop := make(chan struct{})
	m.stop = stop
	m.running = true
	m.startedAt = time.Now()
	m.threads = threads
	m.autoStopAt = time.Time{}

	go m.crawl(stop, threads)

	if m.configuration.CrawlerAutoStopMinutes > 0 {
		m.autoStopAt = m.startedAt.Add(time.Duration(m.configuration.CrawlerAutoStopMinutes) * time.Minute)
		go m.stopAfter(stop, time.Until(m.autoStopAt))
	}

	return true
}

func (m *CrawlerManager) Stop() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopLocked()
}

func (m *CrawlerManager) Status() CrawlerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	return CrawlerStatus{
		Running:       m.running,
		StartedAt:     m.startedAt,
		AutoStopAt:    m.autoStopAt,
		Threads:       m.threads,
		AutoStopAfter: time.Duration(m.configuration.CrawlerAutoStopMinutes) * time.Minute,
	}
}

func (m *CrawlerManager) stopAfter(stop <-chan struct{}, delay time.Duration) {
	if delay <= 0 {
		m.Stop()
		return
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		m.mu.Lock()
		if m.stop == stop {
			m.stopLocked()
		}
		m.mu.Unlock()
	case <-stop:
		return
	}
}

func (m *CrawlerManager) stopLocked() bool {
	if !m.running {
		return false
	}

	close(m.stop)
	m.running = false
	m.stop = nil
	m.autoStopAt = time.Time{}
	return true
}

func (m *CrawlerManager) crawl(stop <-chan struct{}, threads int) {
	endpoints := crawlerEndpoints(m.configuration, m.bootstrapNodes4, m.bootstrapNodes6)
	_ = threads
	mode := config.NormalizeNetworkMode(m.configuration.NetworkMode)
	log.Info().Str("mode", mode).Msg("Network Mode")
	log.Info().Bool("enabled", mode != config.NetworkModeIPv6).Msg("IPv4 DHT")
	log.Info().Bool("enabled", mode != config.NetworkModeIPv4).Msg("IPv6 DHT")

	trawlingManager := dhtcclient.NewManager(endpoints, 10*time.Second, m.configuration.MaxNeighbors, m.configuration.RateLimit)
	metadataSink := dhtcclient.NewSink(m.configuration.DrainTimeout, m.configuration.MaxLeeches, m.configuration.MaxConcurrentDownloads)
	defer trawlingManager.Terminate()
	defer metadataSink.Terminate()

	for {
		select {
		case <-stop:
			return
		case result := <-trawlingManager.Output():
			hash := result.InfoHash()
			if cache.InfoHashCacheAdd(string(hash)) {
				metadataSink.Sink(result)
			}
		case md, ok := <-metadataSink.Drain():
			if !ok {
				return
			}
			if m.database.InsertMetadata(md) {
				fmt.Println("\t + Added:", md.Name)
				db.CheckWatches(m.configuration, m.database, md, m.notifier)
				m.hub.BroadcastMetadata(md)
			}
		}
	}
}

func crawlerEndpoints(configuration *config.Configuration, bootstrap4, bootstrap6 []string) []dhtcclient.ListenEndpoint {
	endpoints := make([]dhtcclient.ListenEndpoint, 0, 2)
	switch config.NormalizeNetworkMode(configuration.NetworkMode) {
	case config.NetworkModeIPv6:
		endpoints = append(endpoints, dhtcclient.ListenEndpoint{Network: "udp6", Address: configuration.ListenIPv6, CachePath: configuration.RoutingTableCacheIPv6, Bootstrap: bootstrap6})
	case config.NetworkModeDual:
		endpoints = append(endpoints,
			dhtcclient.ListenEndpoint{Network: "udp4", Address: configuration.ListenIPv4, CachePath: configuration.RoutingTableCacheIPv4, Bootstrap: bootstrap4},
			dhtcclient.ListenEndpoint{Network: "udp6", Address: configuration.ListenIPv6, CachePath: configuration.RoutingTableCacheIPv6, Bootstrap: bootstrap6},
		)
	default:
		endpoints = append(endpoints, dhtcclient.ListenEndpoint{Network: "udp4", Address: configuration.ListenIPv4, CachePath: configuration.RoutingTableCacheIPv4, Bootstrap: bootstrap4})
	}
	return endpoints
}
