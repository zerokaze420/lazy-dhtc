package dhtc_client

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIPv6BootstrapCacheRoundTrip(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "nodes6.txt")
	manager := &Manager{
		ipv6CachePath: cachePath,
		ipv6Cache:     make(map[string]struct{}),
	}
	manager.cacheIPv6Nodes(CompactNodeInfos{
		{ID: make([]byte, 20), Addr: net.UDPAddr{IP: net.ParseIP("2001:db8::1"), Port: 6881}},
		{ID: make([]byte, 20), Addr: net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 6881}},
		{ID: make([]byte, 20), Addr: net.UDPAddr{IP: net.ParseIP("fe80::1"), Port: 6881}},
	})

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "[2001:db8::1]:6881" {
		t.Fatalf("unexpected cache contents: %q", got)
	}

	reloaded := &Manager{ipv6CachePath: cachePath, ipv6Cache: make(map[string]struct{})}
	nodes := reloaded.loadIPv6BootstrapNodes()
	if len(nodes) != 1 || nodes[0] != "[2001:db8::1]:6881" {
		t.Fatalf("unexpected reloaded nodes: %#v", nodes)
	}
}

func TestDualStackIPv4ServiceRequestsNodes6(t *testing.T) {
	manager := NewManager(nil, []ListenEndpoint{
		{Network: "udp4", Address: "127.0.0.1:0"},
		{Network: "udp6", Address: "[::1]:0"},
	}, time.Hour, 10, 0, filepath.Join(t.TempDir(), "nodes6.txt"))
	defer manager.Terminate()

	service, ok := manager.indexingServices[0].(*IndexingService)
	if !ok {
		t.Fatal("IPv4 indexing service has unexpected type")
	}
	if len(service.want) != 2 || service.want[0] != "n4" || service.want[1] != "n6" {
		t.Fatalf("unexpected dual-stack want list: %#v", service.want)
	}
}

func TestManagerInjectsLearnedNodesIntoIPv6Service(t *testing.T) {
	service := NewIndexingService("udp6", "[::1]:0", time.Hour, 10, 0, IndexingServiceEventHandlers{})
	manager := &Manager{
		ipv6Services:  []*IndexingService{service},
		ipv6CachePath: filepath.Join(t.TempDir(), "nodes6.txt"),
		ipv6Cache:     make(map[string]struct{}),
	}
	node := CompactNodeInfo{
		ID:   []byte("12345678901234567890"),
		Addr: net.UDPAddr{IP: net.ParseIP("2001:db8::2"), Port: 6881},
	}
	manager.onNodes6(CompactNodeInfos{node})

	service.routingTableMutex.RLock()
	defer service.routingTableMutex.RUnlock()
	addr, ok := service.routingTable[string(node.ID)]
	if !ok || !addr.IP.Equal(node.Addr.IP) || addr.Port != node.Addr.Port {
		t.Fatalf("learned IPv6 node was not injected: %#v", addr)
	}
}
