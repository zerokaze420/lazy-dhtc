package dhtc_client

import (
	"net/netip"
	"testing"
	"time"
)

func TestRequestPeersAppliesPendingRequestBackpressure(t *testing.T) {
	service := NewIndexingService("udp4", "127.0.0.1:0", "", time.Second, 8, 0, IndexingServiceEventHandlers{})
	now := time.Now()
	for i := 0; i < maxPendingGetPeersRequests; i++ {
		service.getPeersRequests[uint16BE(uint16(i))] = getPeersEntry{
			infoHash:  []byte{byte(i)},
			createdAt: now,
		}
	}
	service.counter = 1234

	if service.requestPeers(make([]byte, 20), netip.MustParseAddrPort("192.0.2.1:6881")) {
		t.Fatal("request was accepted even though pending request table was full")
	}
	if service.counter != 1234 {
		t.Fatalf("counter advanced under backpressure: got %d", service.counter)
	}
	if len(service.getPeersRequests) != maxPendingGetPeersRequests {
		t.Fatalf("pending request count changed: got %d", len(service.getPeersRequests))
	}
	if service.rejectedGetPeersRequests() != 1 {
		t.Fatalf("rejected request count = %d, want 1", service.rejectedGetPeersRequests())
	}
}

func TestRequestPeersReusesExpiredTransaction(t *testing.T) {
	service := NewIndexingService("udp4", "127.0.0.1:0", "", time.Second, 8, 0, IndexingServiceEventHandlers{})
	service.counter = 7
	txID := uint16BE(7)
	service.getPeersRequests[txID] = getPeersEntry{
		infoHash:  []byte("old"),
		createdAt: time.Now().Add(-getPeersRequestTimeout),
	}
	infoHash := make([]byte, 20)
	infoHash[0] = 42

	if !service.requestPeers(infoHash, netip.MustParseAddrPort("192.0.2.1:6881")) {
		t.Fatal("expired transaction ID was not reused")
	}
	entry := service.getPeersRequests[txID]
	if len(entry.infoHash) != 20 || entry.infoHash[0] != 42 {
		t.Fatalf("expired entry was not replaced: %#v", entry)
	}
}

func TestRequestPeersReclaimsExpiredEntriesBeforeBackpressure(t *testing.T) {
	service := NewIndexingService("udp4", "127.0.0.1:0", "", time.Second, 8, 0, IndexingServiceEventHandlers{})
	now := time.Now()
	for i := 0; i < maxPendingGetPeersRequests; i++ {
		service.getPeersRequests[uint16BE(uint16(i))] = getPeersEntry{
			infoHash:  []byte{byte(i)},
			createdAt: now.Add(-getPeersRequestTimeout),
		}
	}
	service.nextRequestExpiry = now.Add(-time.Second)

	if !service.requestPeers(make([]byte, 20), netip.MustParseAddrPort("192.0.2.1:6881")) {
		t.Fatal("request was rejected even though all pending entries had expired")
	}
	if got := service.pendingGetPeersRequests(); got != 1 {
		t.Fatalf("pending request count = %d, want 1", got)
	}
	if service.rejectedGetPeersRequests() != 0 {
		t.Fatalf("rejected request count = %d, want 0", service.rejectedGetPeersRequests())
	}
}
