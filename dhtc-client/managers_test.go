package dhtc_client

import (
	"net/netip"
	"path/filepath"
	"testing"
	"time"
)

func TestDualStackIPv4NetworkRequestsBEP32Candidates(t *testing.T) {
	manager := NewManager([]ListenEndpoint{
		{Network: "udp4", Address: "127.0.0.1:0"},
		{Network: "udp6", Address: "[::1]:0"},
	}, time.Hour, 10, 0)
	defer manager.Terminate()

	wants := [][]string{{"n4", "n6"}, {"n6"}}
	for i, expected := range wants {
		service := manager.indexingServices[i].(*IndexingService)
		if len(service.want) != len(expected) {
			t.Fatalf("service %d want list = %#v, want %#v", i, service.want, expected)
		}
		for j := range expected {
			if service.want[j] != expected[j] {
				t.Fatalf("service %d want list = %#v, want %#v", i, service.want, expected)
			}
		}
	}
}

func TestRoutingTableCacheRoundTrip(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "routing-v6.json")
	service := NewIndexingService("udp6", "[::1]:0", cachePath, time.Hour, 10, 0, IndexingServiceEventHandlers{})
	node := CompactNodeInfo{ID: []byte("12345678901234567890"), Addr: netip.MustParseAddrPort("[2001:db8::3]:6881")}
	service.addNode(node.ID, node.Addr)
	service.saveRoutingTable()

	restored := NewIndexingService("udp6", "[::1]:0", cachePath, time.Hour, 10, 0, IndexingServiceEventHandlers{})
	restored.loadRoutingTable()
	if restored.routingTableSize() != 1 {
		t.Fatalf("restored routing table size = %d, want 1", restored.routingTableSize())
	}
	got := restored.routingTable.Snapshot()[0].Addr
	if got != node.Addr {
		t.Fatalf("restored address = %v, want %v", got, node.Addr)
	}
}

func TestDualStackRoutingTablesAreIndependent(t *testing.T) {
	manager := NewManager([]ListenEndpoint{
		{Network: "udp4", Address: "127.0.0.1:0"},
		{Network: "udp6", Address: "[::1]:0"},
	}, time.Hour, 16, 0)
	defer manager.Terminate()

	ipv4 := manager.indexingServices[0].(*IndexingService)
	ipv6 := manager.indexingServices[1].(*IndexingService)
	ipv4.addNode([]byte("12345678901234567890"), netip.MustParseAddrPort("192.0.2.1:6881"))

	if ipv4.routingTableSize() != 1 || ipv6.routingTableSize() != 0 {
		t.Fatalf("routing table sizes = (%d, %d), want (1, 0)", ipv4.routingTableSize(), ipv6.routingTableSize())
	}
}
