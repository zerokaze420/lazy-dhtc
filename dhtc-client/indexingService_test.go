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
}

func TestRequestPeersReusesExpiredTransaction(t *testing.T) {
	service := NewIndexingService("udp4", "127.0.0.1:0", "", time.Second, 8, 0, IndexingServiceEventHandlers{})
	service.counter = 7
	txID := uint16BE(7)
	service.getPeersRequests[txID] = getPeersEntry{
		infoHash:  []byte("old"),
		createdAt: time.Now().Add(-6 * time.Minute),
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
