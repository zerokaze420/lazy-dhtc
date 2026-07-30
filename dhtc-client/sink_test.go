package dhtc_client

import (
	"net/netip"
	"testing"
	"time"
)

type sinkTestResult struct {
	infoHash []byte
	peers    []netip.AddrPort
}

func (r sinkTestResult) InfoHash() []byte            { return r.infoHash }
func (r sinkTestResult) PeerAddrs() []netip.AddrPort { return r.peers }
func (r sinkTestResult) Family() int                 { return 4 }

func TestSinkRejectsResultWithoutPeers(t *testing.T) {
	sink := NewSink(time.Second, 1, 1, nil)
	defer sink.Terminate()

	if sink.Sink(sinkTestResult{infoHash: make([]byte, 20)}) {
		t.Fatal("result without peers was accepted")
	}
}

func TestSinkRejectsResultAtLeechCapacity(t *testing.T) {
	sink := NewSink(time.Minute, 1, 1, nil)
	defer sink.Terminate()
	peer := netip.MustParseAddrPort("127.0.0.1:1")
	sink.incomingInfoHashes["existing"] = nil

	if sink.Sink(sinkTestResult{infoHash: []byte("abcdefghijabcdefghij"), peers: []netip.AddrPort{peer}}) {
		t.Fatal("result above leech capacity was accepted")
	}
}

func TestSinkDiscardsInfoHashAfterFinalPeerFailure(t *testing.T) {
	infoHash := []byte("12345678901234567890")
	discarded := make(chan string, 1)
	sink := NewSink(time.Second, 1, 1, func(hash []byte) {
		discarded <- string(hash)
	})
	defer sink.Terminate()
	sink.incomingInfoHashes[string(infoHash)] = nil
	sink.incomingFamilies[string(infoHash)] = 4

	sink.onLeechError(infoHash, nil)

	select {
	case got := <-discarded:
		if got != string(infoHash) {
			t.Fatalf("discarded %q, want %q", got, infoHash)
		}
	case <-time.After(time.Second):
		t.Fatal("final peer failure did not discard info hash")
	}
}

func TestSinkAddsNewPeersToActiveDownload(t *testing.T) {
	infoHash := []byte("12345678901234567890")
	first := netip.MustParseAddrPort("127.0.0.1:1")
	second := netip.MustParseAddrPort("127.0.0.1:2")
	sink := NewSink(time.Minute, 1, 1, nil)
	defer sink.Terminate()
	sink.incomingInfoHashes[string(infoHash)] = nil
	sink.incomingFamilies[string(infoHash)] = 4
	sink.incomingPeers[string(infoHash)] = peerSet([]netip.AddrPort{first})

	result := sinkTestResult{infoHash: infoHash, peers: []netip.AddrPort{first, second, second}}
	if !sink.AddPeers(result) {
		t.Fatal("new peer was not added to active download")
	}
	peers := sink.incomingInfoHashes[string(infoHash)]
	if len(peers) != 1 || peers[0] != second {
		t.Fatalf("queued peers = %v, want [%v]", peers, second)
	}
	if sink.AddPeers(result) {
		t.Fatal("duplicate peers were added twice")
	}
}
