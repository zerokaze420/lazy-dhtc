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
)

type CrawlerStatus struct {
	Running       bool
	StartedAt     time.Time
	AutoStopAt    time.Time
	Threads       int
	AutoStopAfter time.Duration
}

type CrawlerManager struct {
	mu             sync.Mutex
	configuration  *config.Configuration
	database       db.Repository
	notifier       *notifier.Manager
	hub            *Hub
	bootstrapNodes []string
	stop           chan struct{}
	startedAt      time.Time
	autoStopAt     time.Time
	threads        int
	running        bool
}

func NewCrawlerManager(configuration *config.Configuration, bootstrapNodes []string, database db.Repository, nManager *notifier.Manager, hub *Hub) *CrawlerManager {
	return &CrawlerManager{
		configuration:  configuration,
		database:       database,
		notifier:       nManager,
		hub:            hub,
		bootstrapNodes: bootstrapNodes,
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
	endpoints := crawlerEndpoints(m.configuration.NetworkMode, threads)

	trawlingManager := dhtcclient.NewManager(m.bootstrapNodes, endpoints, 10*time.Second, m.configuration.MaxNeighbors, m.configuration.RateLimit, m.configuration.IPv6BootstrapNodeFile)
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

func crawlerEndpoints(mode string, threads int) []dhtcclient.ListenEndpoint {
	if threads < 1 {
		threads = 1
	}
	endpoints := make([]dhtcclient.ListenEndpoint, 0, threads*2)
	add := func(network, address string) {
		for range threads {
			endpoints = append(endpoints, dhtcclient.ListenEndpoint{Network: network, Address: address})
		}
	}
	switch config.NormalizeNetworkMode(mode) {
	case config.NetworkModeIPv6:
		add("udp6", "[::]:0")
	case config.NetworkModeDual:
		add("udp4", "0.0.0.0:0")
		add("udp6", "[::]:0")
	default:
		add("udp4", "0.0.0.0:0")
	}
	return endpoints
}
