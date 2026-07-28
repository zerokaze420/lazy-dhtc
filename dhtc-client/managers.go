package dhtc_client

import (
	"context"
	"net/netip"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type Service interface {
	Start(context.Context, []string)
	Terminate()
	routingTableSize() int
}

type Result interface {
	InfoHash() []byte
	PeerAddrs() []netip.AddrPort
}

type Manager struct {
	output           chan Result
	indexingServices []Service
	done             chan struct{}
	stopOnce         sync.Once
	ctx              context.Context
	cancel           context.CancelFunc
}

type ListenEndpoint struct {
	Network   string
	Address   string
	CachePath string
	Bootstrap []string
}

func NewManager(endpoints []ListenEndpoint, interval time.Duration, maxNeighbors uint, rateLimit int) *Manager {
	manager := new(Manager)
	manager.output = make(chan Result, 20)
	manager.done = make(chan struct{})
	manager.ctx, manager.cancel = context.WithCancel(context.Background())

	for _, endpoint := range endpoints {
		service := NewIndexingService(endpoint.Network, endpoint.Address, endpoint.CachePath, interval, maxNeighbors, rateLimit, IndexingServiceEventHandlers{
			OnResult: manager.onIndexingResult,
		})
		manager.indexingServices = append(manager.indexingServices, service)
	}
	for i, service := range manager.indexingServices {
		service.Start(manager.ctx, endpoints[i].Bootstrap)
		log.Info().Str("network", endpoints[i].Network).Bool("enabled", true).Int("bootstrap", len(endpoints[i].Bootstrap)).Int("routing_table", service.routingTableSize()).Msg("DHT network started")
	}

	return manager
}

func (m *Manager) onIndexingResult(res IndexingResult) {
	select {
	case <-m.done:
		return
	case m.output <- res:
	default:
		log.Debug().Msg("DHT manager output ch is full, idx result dropped!")
	}
}

func (m *Manager) Output() <-chan Result {
	return m.output
}

func (m *Manager) Terminate() {
	m.stopOnce.Do(func() {
		close(m.done)
		if m.cancel != nil {
			m.cancel()
		}
		for _, service := range m.indexingServices {
			service.Terminate()
		}
	})
}
