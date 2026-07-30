package dhtc_client

import (
	"net/netip"
	"time"

	"github.com/rs/zerolog/log"
)

func NewSink(deadline time.Duration, maxNLeeches int, maxConcurrentDownloads int, onDiscard func([]byte)) *Sink {
	ms := new(Sink)

	ms.PeerID = randomID()
	ms.deadline = deadline
	ms.maxNLeeches = maxNLeeches
	ms.maxConcurrentDownloads = maxConcurrentDownloads
	ms.downloadSem = make(chan struct{}, maxConcurrentDownloads)
	ms.drain = make(chan Metadata, 10)
	ms.incomingInfoHashes = make(map[string][]netip.AddrPort)
	ms.incomingFamilies = make(map[string]int)
	ms.incomingPeers = make(map[string]map[netip.AddrPort]struct{})
	ms.termination = make(chan any)
	ms.onDiscard = onDiscard

	return ms
}

func (ms *Sink) Sink(res Result) bool {
	if ms.terminated.Load() {
		return false
	}
	ms.incomingInfoHashesMx.Lock()
	defer ms.incomingInfoHashesMx.Unlock()

	// cap the max # of leeches
	if len(ms.incomingInfoHashes) >= ms.maxNLeeches {
		return false
	}

	infoHash := res.InfoHash()
	key := string(infoHash)
	peerAddrs := uniqueValidPeers(res.PeerAddrs(), nil)

	if _, exists := ms.incomingInfoHashes[key]; exists {
		return false
	} else if len(peerAddrs) > 0 {
		peer := peerAddrs[0]
		ms.incomingInfoHashes[key] = peerAddrs[1:]
		ms.incomingFamilies[key] = res.Family()
		ms.incomingPeers[key] = peerSet(peerAddrs)

		go ms.download(infoHash, peer)
		return true
	}
	return false
}

// AddPeers adds newly discovered peers to an infohash that is already being downloaded.
func (ms *Sink) AddPeers(res Result) bool {
	if ms.terminated.Load() {
		return false
	}

	ms.incomingInfoHashesMx.Lock()
	defer ms.incomingInfoHashesMx.Unlock()

	key := string(res.InfoHash())
	if _, exists := ms.incomingInfoHashes[key]; !exists {
		return false
	}
	peers := uniqueValidPeers(res.PeerAddrs(), ms.incomingPeers[key])
	if len(peers) == 0 {
		return false
	}
	for _, peer := range peers {
		ms.incomingPeers[key][peer] = struct{}{}
	}
	ms.incomingInfoHashes[key] = append(ms.incomingInfoHashes[key], peers...)
	return true
}

func uniqueValidPeers(peers []netip.AddrPort, existing map[netip.AddrPort]struct{}) []netip.AddrPort {
	seen := make(map[netip.AddrPort]struct{}, len(peers))
	result := make([]netip.AddrPort, 0, len(peers))
	for _, peer := range peers {
		if !peer.IsValid() || peer.Port() == 0 {
			continue
		}
		if _, ok := existing[peer]; ok {
			continue
		}
		if _, ok := seen[peer]; ok {
			continue
		}
		seen[peer] = struct{}{}
		result = append(result, peer)
	}
	return result
}

func peerSet(peers []netip.AddrPort) map[netip.AddrPort]struct{} {
	result := make(map[netip.AddrPort]struct{}, len(peers))
	for _, peer := range peers {
		result[peer] = struct{}{}
	}
	return result
}

func (ms *Sink) download(infoHash []byte, peer netip.AddrPort) {
	if ms.terminated.Load() {
		return
	}

	ms.downloadSem <- struct{}{}
	defer func() { <-ms.downloadSem }()

	NewClient(infoHash, peer, ms.PeerID, ClientEventHandlers{
		OnSuccess: ms.flush,
		OnError:   ms.onLeechError,
		OnPeers:   ms.onPeers,
	}).Do(time.Now().Add(ms.deadline))
}

func (ms *Sink) onPeers(infoHash []byte, peers []netip.AddrPort) {
	ms.incomingInfoHashesMx.Lock()
	defer ms.incomingInfoHashesMx.Unlock()

	if _, exists := ms.incomingInfoHashes[string(infoHash)]; !exists {
		return
	}

	ms.incomingInfoHashes[string(infoHash)] = append(ms.incomingInfoHashes[string(infoHash)], peers...)
}

func (ms *Sink) Drain() <-chan Metadata {
	if ms.terminated.Load() {
		return ms.drain
	}
	return ms.drain
}

func (ms *Sink) Terminate() {
	ms.drainMx.Lock()
	defer ms.drainMx.Unlock()

	if ms.terminated.CompareAndSwap(false, true) {
		close(ms.termination)
		close(ms.drain)
	}
}

func (ms *Sink) flush(result Metadata) {
	ms.drainMx.Lock()
	defer ms.drainMx.Unlock()

	if ms.terminated.Load() {
		return
	}

	ms.incomingInfoHashesMx.Lock()
	result.Family = ms.incomingFamilies[string(result.InfoHash)]
	delete(ms.incomingInfoHashes, string(result.InfoHash))
	delete(ms.incomingFamilies, string(result.InfoHash))
	delete(ms.incomingPeers, string(result.InfoHash))
	ms.incomingInfoHashesMx.Unlock()

	ms.drain <- result
}

func (ms *Sink) onLeechError(infoHash []byte, err error) {
	if ms.terminated.Load() {
		return
	}

	log.Debug().Err(err)

	ms.incomingInfoHashesMx.Lock()
	defer ms.incomingInfoHashesMx.Unlock()

	if len(ms.incomingInfoHashes[string(infoHash)]) > 0 {
		peer := ms.incomingInfoHashes[string(infoHash)][0]
		ms.incomingInfoHashes[string(infoHash)] = ms.incomingInfoHashes[string(infoHash)][1:]
		go ms.download(infoHash, peer)
	} else {
		delete(ms.incomingInfoHashes, string(infoHash))
		delete(ms.incomingFamilies, string(infoHash))
		delete(ms.incomingPeers, string(infoHash))
		if ms.onDiscard != nil {
			ms.onDiscard(infoHash)
		}
	}
}
