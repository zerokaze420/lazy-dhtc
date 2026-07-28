package dhtc_client

import (
	"math/bits"
	"net/netip"
	"sort"
	"sync"
	"time"
)

const kBucketSize = 8

type routingNode struct {
	ID       [20]byte
	Addr     netip.AddrPort
	LastSeen time.Time
}

type RoutingTable struct {
	mu       sync.RWMutex
	localID  [20]byte
	buckets  [160][]routingNode
	maxNodes int
	size     int
}

func NewRoutingTable(localID []byte, maxNodes uint) *RoutingTable {
	table := &RoutingTable{maxNodes: int(maxNodes)}
	copy(table.localID[:], localID)
	if table.maxNodes < kBucketSize {
		table.maxNodes = kBucketSize
	}
	return table
}

func (t *RoutingTable) Add(id []byte, addr netip.AddrPort, seen time.Time) bool {
	if len(id) != 20 || !addr.IsValid() || addr.Port() == 0 {
		return false
	}
	var nodeID [20]byte
	copy(nodeID[:], id)
	bucketIndex := xorBucketIndex(t.localID, nodeID)
	if bucketIndex < 0 {
		return false
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	bucket := t.buckets[bucketIndex]
	for i := range bucket {
		if bucket[i].ID == nodeID {
			bucket[i].Addr = addr
			bucket[i].LastSeen = seen
			copy(bucket[1:i+1], bucket[0:i])
			bucket[0] = routingNode{ID: nodeID, Addr: addr, LastSeen: seen}
			t.buckets[bucketIndex] = bucket
			return false
		}
	}
	if len(bucket) >= kBucketSize {
		bucket = bucket[:kBucketSize-1]
	} else if t.size >= t.maxNodes {
		return false
	} else {
		t.size++
	}
	bucket = append([]routingNode{{ID: nodeID, Addr: addr, LastSeen: seen}}, bucket...)
	t.buckets[bucketIndex] = bucket
	return true
}

func (t *RoutingTable) Len() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.size
}

func (t *RoutingTable) Snapshot() []routingNode {
	t.mu.RLock()
	defer t.mu.RUnlock()
	nodes := make([]routingNode, 0, t.size)
	for _, bucket := range t.buckets {
		nodes = append(nodes, bucket...)
	}
	return nodes
}

func (t *RoutingTable) Closest(target []byte, limit int) []routingNode {
	nodes := t.Snapshot()
	var targetID [20]byte
	copy(targetID[:], target)
	sort.Slice(nodes, func(i, j int) bool {
		return xorLess(nodes[i].ID, nodes[j].ID, targetID)
	})
	if len(nodes) > limit {
		nodes = nodes[:limit]
	}
	return nodes
}

func (t *RoutingTable) Prune(before time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i, bucket := range t.buckets {
		kept := bucket[:0]
		for _, node := range bucket {
			if node.LastSeen.After(before) {
				kept = append(kept, node)
			} else {
				t.size--
			}
		}
		t.buckets[i] = kept
	}
}

func xorBucketIndex(local, remote [20]byte) int {
	for i := range local {
		x := local[i] ^ remote[i]
		if x != 0 {
			return (len(local)-1-i)*8 + bits.Len8(x) - 1
		}
	}
	return -1
}

func xorLess(a, b, target [20]byte) bool {
	for i := range a {
		da := a[i] ^ target[i]
		db := b[i] ^ target[i]
		if da != db {
			return da < db
		}
	}
	return false
}
