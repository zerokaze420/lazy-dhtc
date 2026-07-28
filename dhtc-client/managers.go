package dhtc_client

import (
	"bufio"
	"net"
	"os"
	"sort"
	"strings"
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
	ipv6Services     []*IndexingService
	ipv6CachePath    string
	ipv6CacheMu      sync.Mutex
	ipv6Cache        map[string]struct{}
}

type ListenEndpoint struct {
	Network string
	Address string
}

func NewManager(nodes []string, endpoints []ListenEndpoint, interval time.Duration, maxNeighbors uint, rateLimit int, ipv6CachePath string) *Manager {
	manager := new(Manager)
	manager.output = make(chan Result, 20)
	manager.done = make(chan struct{})
	manager.ipv6CachePath = ipv6CachePath
	manager.ipv6Cache = make(map[string]struct{})

	hasIPv6 := false
	for _, endpoint := range endpoints {
		if endpoint.Network == "udp6" {
			hasIPv6 = true
			break
		}
	}
	if hasIPv6 {
		cachedNodes := manager.loadIPv6BootstrapNodes()
		nodes = append(nodes, cachedNodes...)
		log.Info().Int("count", len(cachedNodes)).Str("path", ipv6CachePath).Msg("Loaded learned IPv6 DHT bootstrap nodes")
	}

	for _, endpoint := range endpoints {
		service := NewIndexingService(endpoint.Network, endpoint.Address, interval, maxNeighbors, rateLimit, IndexingServiceEventHandlers{
			OnResult: manager.onIndexingResult,
			OnNodes6: manager.onNodes6,
		})
		if endpoint.Network == "udp4" && hasIPv6 {
			service.want = []string{"n4", "n6"}
		}
		if endpoint.Network == "udp6" {
			manager.ipv6Services = append(manager.ipv6Services, service)
		}
		manager.indexingServices = append(manager.indexingServices, service)
	}
	for _, service := range manager.indexingServices {
		service.Start(nodes)
	}

	return manager
}

func (m *Manager) onNodes6(nodes CompactNodeInfos) {
	if len(m.ipv6Services) == 0 {
		return
	}
	for _, service := range m.ipv6Services {
		service.AddNodes(nodes)
	}
	m.cacheIPv6Nodes(nodes)
}

func (m *Manager) loadIPv6BootstrapNodes() []string {
	file, err := os.Open(m.ipv6CachePath)
	if err != nil {
		return nil
	}
	defer file.Close()

	var nodes []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		node := strings.TrimSpace(scanner.Text())
		if node == "" {
			continue
		}
		m.ipv6Cache[node] = struct{}{}
		nodes = append(nodes, node)
	}
	return nodes
}

func (m *Manager) cacheIPv6Nodes(nodes CompactNodeInfos) {
	if m.ipv6CachePath == "" {
		return
	}
	m.ipv6CacheMu.Lock()
	defer m.ipv6CacheMu.Unlock()

	changed := false
	for _, node := range nodes {
		if node.Addr.IP.To4() != nil || node.Addr.Port == 0 || !node.Addr.IP.IsGlobalUnicast() {
			continue
		}
		address := node.Addr.String()
		if _, exists := m.ipv6Cache[address]; exists {
			continue
		}
		m.ipv6Cache[address] = struct{}{}
		changed = true
	}
	if !changed {
		return
	}

	addresses := make([]string, 0, len(m.ipv6Cache))
	for address := range m.ipv6Cache {
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	if len(addresses) > 256 {
		addresses = addresses[len(addresses)-256:]
	}
	data := []byte(strings.Join(addresses, "\n") + "\n")
	if err := os.WriteFile(m.ipv6CachePath, data, 0600); err != nil {
		log.Warn().Err(err).Str("path", m.ipv6CachePath).Msg("Could not persist learned IPv6 DHT nodes")
		return
	}
	log.Info().Int("count", len(addresses)).Str("path", m.ipv6CachePath).Msg("Persisted learned IPv6 DHT bootstrap nodes")
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
