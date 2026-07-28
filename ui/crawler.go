package ui

import (
	"dhtc/cache"
	"dhtc/config"
	"dhtc/db"
	dhtcclient "dhtc/dhtc-client"
	"dhtc/notifier"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type CrawlerStatus struct {
	Running             bool
	StartedAt           time.Time
	AutoStopAt          time.Time
	Threads             int
	AutoStopAfter       time.Duration
	ScheduleEnabled     bool
	ScheduleStart       string
	ScheduleEnd         string
	NextScheduleChange  time.Time
	ManualOverride      bool
	ManualOverrideUntil time.Time
}

type CrawlerManager struct {
	mu              sync.Mutex
	configuration   *config.Configuration
	database        db.Repository
	notifier        *notifier.Manager
	hub             *Hub
	bootstrapNodes4 []string
	stop            chan struct{}
	startedAt       time.Time
	autoStopAt      time.Time
	threads         int
	running         bool
	manualOverride  bool
	manualRun       bool
	manualUntil     time.Time
	onMetadata      func(dhtcclient.Metadata) bool
}

func NewCrawlerManager(configuration *config.Configuration, bootstrapNodes4 []string, database db.Repository, nManager *notifier.Manager, hub *Hub) *CrawlerManager {
	manager := &CrawlerManager{
		configuration:   configuration,
		database:        database,
		notifier:        nManager,
		hub:             hub,
		bootstrapNodes4: bootstrapNodes4,
	}
	manager.onMetadata = func(md dhtcclient.Metadata) bool {
		return IngestMetadata(manager.configuration, manager.database, manager.notifier, manager.hub, md)
	}
	go manager.scheduleLoop()
	return manager
}

func IngestMetadata(configuration *config.Configuration, database db.Repository, nManager *notifier.Manager, hub *Hub, md dhtcclient.Metadata) bool {
	if !database.InsertMetadata(md) {
		return false
	}
	fmt.Println("\t + Added:", md.Name)
	db.CheckWatches(configuration, database, md, nManager)
	hub.BroadcastMetadata(md)
	return true
}

func NewWorkerCrawlerManager(configuration *config.Configuration, bootstrapNodes4 []string, onMetadata func(dhtcclient.Metadata) bool) *CrawlerManager {
	manager := &CrawlerManager{
		configuration:   configuration,
		bootstrapNodes4: bootstrapNodes4,
		onMetadata:      onMetadata,
	}
	go manager.scheduleLoop()
	return manager
}

func (m *CrawlerManager) Start() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setManualOverrideLocked(true)
	return m.startLocked("manual")
}

func (m *CrawlerManager) startLocked(reason string) bool {

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
	log.Info().Str("reason", reason).Time("auto_stop_at", m.autoStopAt).Msg("Crawler started")

	return true
}

func (m *CrawlerManager) Stop() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setManualOverrideLocked(false)
	return m.stopLocked("manual")
}

func (m *CrawlerManager) Status() CrawlerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	_, next := scheduleState(now, m.configuration.CrawlerScheduleStart, m.configuration.CrawlerScheduleEnd)
	return CrawlerStatus{
		Running:             m.running,
		StartedAt:           m.startedAt,
		AutoStopAt:          m.autoStopAt,
		Threads:             m.threads,
		AutoStopAfter:       time.Duration(m.configuration.CrawlerAutoStopMinutes) * time.Minute,
		ScheduleEnabled:     m.configuration.CrawlerScheduleEnabled,
		ScheduleStart:       m.configuration.CrawlerScheduleStart,
		ScheduleEnd:         m.configuration.CrawlerScheduleEnd,
		NextScheduleChange:  next,
		ManualOverride:      m.manualOverride,
		ManualOverrideUntil: m.manualUntil,
	}
}

func (m *CrawlerManager) ReconcileSchedule() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.configuration.OnlyWebServer || !m.configuration.CrawlerScheduleEnabled {
		m.manualOverride = false
		m.manualUntil = time.Time{}
		return
	}
	now := time.Now()
	if m.manualOverride && now.Before(m.manualUntil) {
		if m.manualRun {
			m.startLocked("manual override")
		} else {
			m.stopLocked("manual override")
		}
		return
	}
	m.manualOverride = false
	m.manualUntil = time.Time{}
	shouldRun, _ := scheduleState(now, m.configuration.CrawlerScheduleStart, m.configuration.CrawlerScheduleEnd)
	if shouldRun {
		m.startLocked("schedule window opened")
	} else {
		m.stopLocked("schedule window closed")
	}
}

func (m *CrawlerManager) setManualOverrideLocked(run bool) {
	m.setManualOverrideAtLocked(time.Now(), run)
}

func (m *CrawlerManager) setManualOverrideAtLocked(now time.Time, run bool) {
	if !m.configuration.CrawlerScheduleEnabled {
		return
	}
	_, next := scheduleState(now, m.configuration.CrawlerScheduleStart, m.configuration.CrawlerScheduleEnd)
	if next.IsZero() {
		return
	}
	m.manualOverride = true
	m.manualRun = run
	m.manualUntil = next
}

func (m *CrawlerManager) scheduleLoop() {
	m.ReconcileSchedule()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		m.ReconcileSchedule()
	}
}

func scheduleState(now time.Time, startText, endText string) (bool, time.Time) {
	startMinute, startOK := parseClockMinutes(startText)
	endMinute, endOK := parseClockMinutes(endText)
	if !startOK || !endOK || startMinute == endMinute {
		return false, time.Time{}
	}
	minute := now.Hour()*60 + now.Minute()
	inside := false
	if startMinute < endMinute {
		inside = minute >= startMinute && minute < endMinute
	} else {
		inside = minute >= startMinute || minute < endMinute
	}
	nextMinute := startMinute
	if inside {
		nextMinute = endMinute
	}
	next := time.Date(now.Year(), now.Month(), now.Day(), nextMinute/60, nextMinute%60, 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return inside, next
}

func parseClockMinutes(value string) (int, bool) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, false
	}
	return parsed.Hour()*60 + parsed.Minute(), true
}

func (m *CrawlerManager) stopAfter(stop <-chan struct{}, delay time.Duration) {
	if delay <= 0 {
		m.mu.Lock()
		if m.stop == stop {
			m.manualRun = false
			m.stopLocked("auto-stop timer")
		}
		m.mu.Unlock()
		return
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		m.mu.Lock()
		if m.stop == stop {
			m.manualRun = false
			m.stopLocked("auto-stop timer")
		}
		m.mu.Unlock()
	case <-stop:
		return
	}
}

func (m *CrawlerManager) stopLocked(reason string) bool {
	if !m.running {
		return false
	}

	close(m.stop)
	m.running = false
	m.stop = nil
	m.autoStopAt = time.Time{}
	log.Info().Str("reason", reason).Msg("Crawler stopped")
	return true
}

func (m *CrawlerManager) crawl(stop <-chan struct{}, threads int) {
	endpoints := crawlerEndpoints(m.configuration, m.bootstrapNodes4)
	_ = threads
	mode := config.NormalizeNetworkMode(m.configuration.NetworkMode)
	log.Info().Str("mode", mode).Msg("Network Mode")
	log.Info().Bool("enabled", true).Msg("IPv4 DHT")
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
			if m.onMetadata != nil {
				m.onMetadata(md)
			}
		}
	}
}

func crawlerEndpoints(configuration *config.Configuration, bootstrap4 []string) []dhtcclient.ListenEndpoint {
	endpoints := make([]dhtcclient.ListenEndpoint, 0, 2)
	switch config.NormalizeNetworkMode(configuration.NetworkMode) {
	case config.NetworkModeDual:
		endpoints = append(endpoints,
			dhtcclient.ListenEndpoint{Network: "udp4", Address: configuration.ListenIPv4, CachePath: configuration.RoutingTableCacheIPv4, Bootstrap: bootstrap4},
			dhtcclient.ListenEndpoint{Network: "udp6", Address: configuration.ListenIPv6, CachePath: configuration.RoutingTableCacheIPv6},
		)
	default:
		endpoints = append(endpoints, dhtcclient.ListenEndpoint{Network: "udp4", Address: configuration.ListenIPv4, CachePath: configuration.RoutingTableCacheIPv4, Bootstrap: bootstrap4})
	}
	return endpoints
}
