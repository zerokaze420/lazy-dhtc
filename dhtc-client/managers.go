package dhtc_client

import (
	"net"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type Service interface {
	Start(nodes []string)
	Terminate()
}

type Result interface {
	InfoHash() []byte
	PeerAddrs() []net.TCPAddr
}

type Manager struct {
	output           chan Result
	indexingServices []Service
	done             chan struct{}
	stopOnce         sync.Once
}

type ListenEndpoint struct {
	Network string
	Address string
}

func NewManager(nodes []string, endpoints []ListenEndpoint, interval time.Duration, maxNeighbors uint, rateLimit int) *Manager {
	manager := new(Manager)
	manager.output = make(chan Result, 20)
	manager.done = make(chan struct{})

	for _, endpoint := range endpoints {
		service := NewIndexingService(endpoint.Network, endpoint.Address, interval, maxNeighbors, rateLimit, IndexingServiceEventHandlers{
			OnResult: manager.onIndexingResult,
		})
		manager.indexingServices = append(manager.indexingServices, service)
		service.Start(nodes)
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
		for _, service := range m.indexingServices {
			service.Terminate()
		}
	})
}
