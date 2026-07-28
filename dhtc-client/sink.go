package dhtc_client

import (
	"net/netip"
	"time"

	"github.com/rs/zerolog/log"
)

func NewSink(deadline time.Duration, maxNLeeches int, maxConcurrentDownloads int) *Sink {
	ms := new(Sink)

	ms.PeerID = randomID()
	ms.deadline = deadline
	ms.maxNLeeches = maxNLeeches
	ms.maxConcurrentDownloads = maxConcurrentDownloads
	ms.downloadSem = make(chan struct{}, maxConcurrentDownloads)
	ms.drain = make(chan Metadata, 10)
	ms.incomingInfoHashes = make(map[string][]netip.AddrPort)
	ms.termination = make(chan any)

	return ms
}

func (ms *Sink) Sink(res Result) {
	if ms.terminated.Load() {
		return
	}
	ms.incomingInfoHashesMx.Lock()
	defer ms.incomingInfoHashesMx.Unlock()

	// cap the max # of leeches
	if len(ms.incomingInfoHashes) >= ms.maxNLeeches {
		return
	}

	infoHash := res.InfoHash()
	peerAddrs := res.PeerAddrs()

	if _, exists := ms.incomingInfoHashes[string(infoHash)]; exists {
		return
	} else if len(peerAddrs) > 0 {
		peer := peerAddrs[0]
		ms.incomingInfoHashes[string(infoHash)] = peerAddrs[1:]

		go ms.download(infoHash, peer)
	}
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

	ms.drain <- result
	// Delete the infoHash from ms.incomingInfoHashes ONLY AFTER once we've flushed the
	// metadata!
	ms.incomingInfoHashesMx.Lock()
	defer ms.incomingInfoHashesMx.Unlock()

	delete(ms.incomingInfoHashes, string(result.InfoHash))
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
	}
}
