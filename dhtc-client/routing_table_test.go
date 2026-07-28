package dhtc_client

import (
	"net/netip"
	"testing"
	"time"
)

func TestRoutingTableBucketCapacity(t *testing.T) {
	local := make([]byte, 20)
	table := NewRoutingTable(local, 64)
	for i := byte(1); i <= 9; i++ {
		id := make([]byte, 20)
		id[0] = 0x80 | i
		table.Add(id, netip.AddrPortFrom(netip.AddrFrom4([4]byte{192, 0, 2, i}), 6881), time.Now())
	}
	if got := table.Len(); got != kBucketSize {
		t.Fatalf("routing table size = %d, want %d", got, kBucketSize)
	}
}

func TestRoutingTableClosestUsesXORDistance(t *testing.T) {
	table := NewRoutingTable(make([]byte, 20), 16)
	for _, value := range []byte{8, 2, 4} {
		id := make([]byte, 20)
		id[19] = value
		table.Add(id, netip.MustParseAddrPort("192.0.2.1:6881"), time.Now())
	}
	target := make([]byte, 20)
	target[19] = 3
	closest := table.Closest(target, 3)
	want := []byte{2, 4, 8}
	for i := range want {
		if closest[i].ID[19] != want[i] {
			t.Fatalf("closest[%d] = %d, want %d", i, closest[i].ID[19], want[i])
		}
	}
}

func TestRoutingTablePrune(t *testing.T) {
	table := NewRoutingTable(make([]byte, 20), 16)
	oldID := make([]byte, 20)
	oldID[19] = 1
	newID := make([]byte, 20)
	newID[19] = 2
	now := time.Now()
	table.Add(oldID, netip.MustParseAddrPort("192.0.2.1:6881"), now.Add(-time.Hour))
	table.Add(newID, netip.MustParseAddrPort("192.0.2.2:6881"), now)

	table.Prune(now.Add(-time.Minute))
	if nodes := table.Snapshot(); len(nodes) != 1 || nodes[0].ID[19] != 2 {
		t.Fatalf("nodes after prune = %#v, want only current node", nodes)
	}
}
