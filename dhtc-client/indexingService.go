package dhtc_client

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/netip"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

type getPeersEntry struct {
	infoHash  []byte
	createdAt time.Time
}

// Keep the pending request table well below the two-byte transaction ID space.
// A busy DHT node can otherwise fill all 65536 IDs and make every new sample
// scan the entire map while holding requestsMu, effectively turning overload
// into a CPU spin loop.
const maxPendingGetPeersRequests = 8192

type IndexingService struct {
	// Private
	protocol      *Protocol
	network       string
	want          []string
	cachePath     string
	started       bool
	interval      time.Duration
	eventHandlers IndexingServiceEventHandlers
	done          chan struct{}
	stopOnce      sync.Once
	ctx           context.Context
	cancel        context.CancelFunc
	group         *errgroup.Group

	nodeID       []byte
	routingTable *RoutingTable

	counter          uint16
	getPeersRequests map[[2]byte]getPeersEntry
	requestsMu       sync.Mutex

	probeCache   map[netip.Addr]time.Time
	probeCacheMu sync.Mutex
}

type IndexingServiceEventHandlers struct {
	OnResult             func(IndexingResult)
	OnBootstrapCandidate func(netip.AddrPort)
}

type IndexingResult struct {
	infoHash  []byte
	peerAddrs []netip.AddrPort
	family    int
}

func (ir IndexingResult) InfoHash() []byte {
	return ir.infoHash
}

func (ir IndexingResult) PeerAddrs() []netip.AddrPort {
	return ir.peerAddrs
}

func (ir IndexingResult) Family() int {
	return ir.family
}

func NewIndexingService(network string, laddr string, cachePath string, interval time.Duration, maxNeighbors uint, rateLimit int, eventHandlers IndexingServiceEventHandlers) *IndexingService {
	service := new(IndexingService)
	service.network = network
	service.cachePath = cachePath
	if network == "udp6" {
		service.want = []string{"n6"}
	} else {
		service.want = []string{"n4"}
	}
	service.interval = interval
	service.protocol = NewProtocol(
		network,
		laddr,
		rateLimit,
		ProtocolEventHandlers{
			OnPingQuery:                service.onPingQuery,
			OnFindNodeQuery:            service.onFindNodeQuery,
			OnGetPeersQuery:            service.onGetPeersQuery,
			OnAnnouncePeerQuery:        service.onAnnouncePeerQuery,
			OnFindNodeResponse:         service.onFindNodeResponse,
			OnGetPeersResponse:         service.onGetPeersResponse,
			OnSampleInfohashesResponse: service.onSampleInfohashesResponse,
			OnSampleInfohashesQuery:    service.onSampleInfohashesQuery,
		},
	)
	service.nodeID = make([]byte, 20)
	if _, err := rand.Read(service.nodeID); err != nil {
		log.Fatal().Err(err).Msg("Could not generate DHT node ID")
	}
	service.routingTable = NewRoutingTable(service.nodeID, maxNeighbors)
	service.eventHandlers = eventHandlers
	service.done = make(chan struct{})

	service.getPeersRequests = make(map[[2]byte]getPeersEntry)
	service.probeCache = make(map[netip.Addr]time.Time)

	return service
}

func (is *IndexingService) responseNodes(target []byte, limit int) (CompactNodeInfos, CompactNodeInfos) {
	nodes4 := make(CompactNodeInfos, 0, limit)
	nodes6 := make(CompactNodeInfos, 0, limit)
	for _, entry := range is.routingTable.Closest(target, limit) {
		node := CompactNodeInfo{ID: append([]byte(nil), entry.ID[:]...), Addr: entry.Addr}
		if entry.Addr.Addr().Is4() {
			nodes4 = append(nodes4, node)
		} else {
			nodes6 = append(nodes6, node)
		}
		if len(nodes4)+len(nodes6) >= limit {
			break
		}
	}
	return nodes4, nodes6
}

func (is *IndexingService) onPingQuery(msg *Message, addr netip.AddrPort) {
	is.addNode(msg.A.ID, addr)
	is.protocol.SendMessage(NewBasicResponse(msg.T, is.nodeID), addr)
}

func (is *IndexingService) onFindNodeQuery(msg *Message, addr netip.AddrPort) {
	is.addNode(msg.A.ID, addr)
	nodes4, nodes6 := is.responseNodes(msg.A.Target, 8)
	is.protocol.SendMessage(NewNodeResponse(msg.T, is.nodeID, nodes4, nodes6), addr)
}

func (is *IndexingService) onGetPeersQuery(msg *Message, addr netip.AddrPort) {
	is.addNode(msg.A.ID, addr)
	nodes4, nodes6 := is.responseNodes(msg.A.InfoHash, 8)
	token := is.protocol.CalculateToken(addr.Addr())
	is.protocol.SendMessage(NewGetPeersResponse(msg.T, is.nodeID, token, nodes4, nodes6), addr)
}

func (is *IndexingService) onAnnouncePeerQuery(msg *Message, addr netip.AddrPort) {
	if !is.protocol.VerifyToken(addr.Addr(), msg.A.Token) {
		return
	}
	port := uint16(msg.A.Port)
	if msg.A.ImpliedPort != 0 {
		port = addr.Port()
	}
	peer := netip.AddrPortFrom(addr.Addr(), port)
	if is.eventHandlers.OnResult != nil {
		is.eventHandlers.OnResult(IndexingResult{infoHash: append([]byte(nil), msg.A.InfoHash...), peerAddrs: []netip.AddrPort{peer}, family: is.addressFamily()})
	}
	is.protocol.SendMessage(NewBasicResponse(msg.T, is.nodeID), addr)
}

func (is *IndexingService) Start(parent context.Context, nodes []string) {
	if is.started {
		log.Panic().Msg("Attempting to Start() a mainline/IndexingService that has been already started! (Programmer error.)")
	}
	is.started = true
	is.ctx, is.cancel = context.WithCancel(parent)
	is.group, is.ctx = errgroup.WithContext(is.ctx)

	is.loadRoutingTable()
	is.protocol.Start(is.ctx)
	is.group.Go(func() error { is.index(nodes); return nil })
}

func (is *IndexingService) Terminate() {
	is.stopOnce.Do(func() {
		close(is.done)
		if is.cancel != nil {
			is.cancel()
		}
		is.protocol.Terminate()
		if is.group != nil {
			_ = is.group.Wait()
		}
		is.saveRoutingTable()
	})
}

func (is *IndexingService) index(nodes []string) {
	ticker := time.NewTicker(is.interval)
	defer ticker.Stop()
	is.runMaintenance(nodes)

	for {
		select {
		case <-is.ctx.Done():
			return
		case <-is.done:
			return
		case <-ticker.C:
		}

		is.runMaintenance(nodes)
	}
}

func (is *IndexingService) runMaintenance(nodes []string) {
	routingTableLen := is.routingTable.Len()
	if routingTableLen < 8 {
		if len(nodes) == 0 {
			if is.network == "udp6" {
				log.Debug().Int("routing_table", routingTableLen).Msg("IPv6 DHT is waiting for automatic BEP 32 candidates")
			} else {
				log.Warn().Str("network", is.network).Int("routing_table", routingTableLen).Msg("DHT cannot bootstrap: no bootstrap nodes configured")
			}
		} else {
			is.bootstrap(nodes)
		}
	}
	if routingTableLen > 0 {
		is.findNeighbors()
		is.routingTable.Prune(time.Now().Add(-5 * time.Minute))
	}
	is.pruneProbeCache()
	is.expireOldGetPeersRequests()
}

type routingCacheEntry struct {
	ID       string `json:"id"`
	Address  string `json:"address"`
	LastSeen int64  `json:"last_seen"`
}

func (is *IndexingService) loadRoutingTable() {
	data, err := os.ReadFile(is.cachePath)
	if err != nil {
		return
	}
	var entries []routingCacheEntry
	if json.Unmarshal(data, &entries) != nil {
		return
	}
	for _, entry := range entries {
		id, err := hex.DecodeString(entry.ID)
		if err != nil || len(id) != 20 {
			continue
		}
		addr, err := netip.ParseAddrPort(entry.Address)
		if err != nil {
			continue
		}
		seen := time.Unix(entry.LastSeen, 0)
		if entry.LastSeen <= 0 {
			seen = time.Now()
		}
		if is.acceptsAddress(addr) {
			is.routingTable.Add(id, addr, seen)
		}
	}
	log.Info().Str("network", is.network).Int("nodes", is.routingTableSize()).Msg("Restored DHT routing table")
}

func (is *IndexingService) saveRoutingTable() {
	if is.cachePath == "" {
		return
	}
	nodes := is.routingTable.Snapshot()
	entries := make([]routingCacheEntry, 0, len(nodes))
	for _, node := range nodes {
		entries = append(entries, routingCacheEntry{ID: hex.EncodeToString(node.ID[:]), Address: node.Addr.String(), LastSeen: node.LastSeen.Unix()})
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(is.cachePath, data, 0600); err != nil {
		log.Warn().Err(err).Str("path", is.cachePath).Msg("Could not persist DHT routing table")
	}
}

func (is *IndexingService) routingTableSize() int {
	return is.routingTable.Len()
}

func (is *IndexingService) bootstrap(nodes []string) {
	resolved := 0
	sent := 0
	for _, node := range nodes {
		if is.stopped() {
			return
		}

		target := make([]byte, 20)
		_, err := rand.Read(target)
		if err != nil {
			log.Panic().Msg("Could NOT generate random bytes during bootstrapping!")
		}

		addrs, err := resolveBootstrapAddresses(is.network, node)
		if err != nil {
			log.Error().Err(err).Str("node", node).Msg("Could NOT resolve (UDP) address of the bootstrapping node!")
			continue
		}

		for _, addr := range addrs {
			resolved++
			is.protocol.SendMessage(NewFindNodeQuery(is.nodeID, target, is.want), addr)
			sent++
		}
	}
	log.Info().Str("network", is.network).Int("configured", len(nodes)).Int("resolved", resolved).Int("sent", sent).Int("routing_table", is.routingTable.Len()).Msg("DHT bootstrap round")
}

func (is *IndexingService) findNeighbors() {
	if is.stopped() {
		return
	}

	target := make([]byte, 20)

	nodes := is.routingTable.Snapshot()
	addressesToSend := make([]netip.AddrPort, 0, len(nodes))
	for _, node := range nodes {
		addressesToSend = append(addressesToSend, node.Addr)
	}

	for _, addr := range addressesToSend {
		if is.stopped() {
			return
		}

		_, err := rand.Read(target)
		if err != nil {
			log.Panic().Msg("Could NOT generate random bytes during bootstrapping!")
		}

		is.protocol.SendMessage(
			NewSampleInfohashesQuery(is.nodeID, []byte("aa"), target, is.want),
			addr,
		)
	}
}

func (is *IndexingService) addNode(id []byte, addr netip.AddrPort) {
	if is.stopped() {
		return
	}

	if !is.acceptsAddress(addr) {
		return
	}
	if !is.routingTable.Add(id, addr, time.Now()) {
		return
	}

	target := make([]byte, 20)
	_, err := rand.Read(target)
	if err != nil {
		log.Panic().Msg("Could NOT generate random bytes!")
	}
	is.protocol.SendMessage(
		NewSampleInfohashesQuery(is.nodeID, []byte("aa"), target, is.want),
		addr,
	)
}

func (is *IndexingService) onFindNodeResponse(response *Message, addr netip.AddrPort) {
	if is.stopped() {
		return
	}

	is.addNode(response.R.ID, addr)

	for _, node := range response.R.Nodes {
		is.addNode(node.ID, node.Addr)
	}
	for _, node := range response.R.Nodes6 {
		is.handleDiscoveredNode(node)
	}
}

func (is *IndexingService) onGetPeersResponse(msg *Message, addr netip.AddrPort) {
	if is.stopped() {
		return
	}

	is.addNode(msg.R.ID, addr)

	var t [2]byte
	copy(t[:], msg.T)

	is.requestsMu.Lock()
	entry, ok := is.getPeersRequests[t]
	delete(is.getPeersRequests, t)
	is.requestsMu.Unlock()
	if !ok {
		return
	}
	infoHash := entry.infoHash

	// BEP 51 specifies that
	//     The new sample_infohashes remote procedure call requests that a remote node return a string of multiple
	//     concatenated infohashes (20 bytes each) FOR WHICH IT HOLDS GET_PEERS VALUES.
	//                                                                          ^^^^^^
	// So theoretically we should never hit the case where `values` is empty, but c'est la vie.
	if len(msg.R.Values) == 0 {
		return
	}

	peerAddrs := make([]netip.AddrPort, 0)
	for _, peer := range msg.R.Values {
		if !peer.Addr.IsValid() || peer.Addr.Port() == 0 {
			continue
		}
		peerAddrs = append(peerAddrs, peer.Addr)
	}

	is.eventHandlers.OnResult(IndexingResult{
		infoHash:  infoHash,
		peerAddrs: peerAddrs,
		family:    is.addressFamily(),
	})
}

func (is *IndexingService) onSampleInfohashesResponse(msg *Message, addr netip.AddrPort) {
	if is.stopped() {
		return
	}

	is.addNode(msg.R.ID, addr)

	// request samples
	for i := range len(msg.R.Samples) / 20 {
		infoHash := make([]byte, 20)
		copy(infoHash, msg.R.Samples[i*20:(i+1)*20])
		is.requestPeers(infoHash, addr)
	}

	for _, infoHash := range msg.R.Samples2 {
		ih := make([]byte, 32)
		copy(ih, infoHash)
		is.requestPeers(ih, addr)
	}

	for _, node := range msg.R.Nodes {
		is.addNode(node.ID, node.Addr)
	}

	for _, node := range msg.R.Nodes6 {
		is.handleDiscoveredNode(node)
	}
}

func (is *IndexingService) handleDiscoveredNode(node CompactNodeInfo) {
	if is.acceptsAddress(node.Addr) {
		is.addNode(node.ID, node.Addr)
		return
	}
	if is.network == "udp4" && node.Addr.Addr().Is6() && is.eventHandlers.OnBootstrapCandidate != nil {
		is.eventHandlers.OnBootstrapCandidate(node.Addr)
	}
}

func (is *IndexingService) probeBootstrapCandidate(addr netip.AddrPort) {
	if !is.acceptsAddress(addr) || is.stopped() {
		return
	}

	is.probeCacheMu.Lock()
	if last, ok := is.probeCache[addr.Addr()]; ok && time.Since(last) < 10*time.Minute {
		is.probeCacheMu.Unlock()
		return
	}
	is.probeCache[addr.Addr()] = time.Now()
	is.probeCacheMu.Unlock()

	target := make([]byte, 20)
	if _, err := rand.Read(target); err != nil {
		return
	}
	is.protocol.SendMessage(NewFindNodeQuery(is.nodeID, target, is.want), addr)
}

func (is *IndexingService) requestPeers(infoHash []byte, addr netip.AddrPort) bool {
	if is.stopped() {
		return false
	}

	is.requestsMu.Lock()
	if len(is.getPeersRequests) >= maxPendingGetPeersRequests {
		is.requestsMu.Unlock()
		return false
	}

	var t [2]byte
	for {
		t = uint16BE(is.counter)
		is.counter++
		if entry, exists := is.getPeersRequests[t]; !exists || time.Since(entry.createdAt) > 5*time.Minute {
			break
		}
	}
	is.getPeersRequests[t] = getPeersEntry{infoHash: append([]byte(nil), infoHash...), createdAt: time.Now()}
	is.requestsMu.Unlock()

	msg := NewGetPeersQuery(is.nodeID, infoHash, is.want)
	msg.T = t[:]
	is.protocol.SendMessage(msg, addr)
	return true
}

func (is *IndexingService) onSampleInfohashesQuery(msg *Message, addr netip.AddrPort) {
	if is.stopped() {
		return
	}

	nodes4, nodes6 := is.responseNodes(msg.A.Target, 8)
	response := NewSampleInfohashesResponse(msg.T, is.nodeID, int(is.interval.Seconds()), nodes4, nodes6, 0, nil)
	is.protocol.SendMessage(response, addr)
}

func (is *IndexingService) acceptsAddress(addr netip.AddrPort) bool {
	if !addr.IsValid() || addr.Port() == 0 {
		return false
	}
	is4 := addr.Addr().Is4()
	return (is.network == "udp4" && is4) || (is.network == "udp6" && !is4)
}

func (is *IndexingService) addressFamily() int {
	if is.network == "udp6" {
		return 6
	}
	return 4
}

func (is *IndexingService) expireOldGetPeersRequests() {
	cutoff := time.Now().Add(-5 * time.Minute)
	is.requestsMu.Lock()
	defer is.requestsMu.Unlock()
	for t, entry := range is.getPeersRequests {
		if entry.createdAt.Before(cutoff) {
			delete(is.getPeersRequests, t)
		}
	}
}

func (is *IndexingService) pruneProbeCache() {
	is.probeCacheMu.Lock()
	defer is.probeCacheMu.Unlock()
	cutoff := time.Now().Add(-10 * time.Minute)
	for addr, last := range is.probeCache {
		if last.Before(cutoff) {
			delete(is.probeCache, addr)
		}
	}
}

func resolveBootstrapAddresses(network, node string) ([]netip.AddrPort, error) {
	host, portText, err := net.SplitHostPort(node)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return nil, err
	}

	ips, err := net.DefaultResolver.LookupIP(context.Background(), "ip", host)
	if err != nil {
		return nil, err
	}
	addrs := make([]netip.AddrPort, 0, len(ips))
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			continue
		}
		addr = addr.Unmap()
		is4 := addr.Is4()
		if (network == "udp4" && !is4) || (network == "udp6" && is4) {
			continue
		}
		addrs = append(addrs, netip.AddrPortFrom(addr, uint16(port)))
	}
	return addrs, nil
}

func uint16BE(v uint16) (b [2]byte) {
	b[0] = byte(v >> 8)
	b[1] = byte(v)
	return
}

func (is *IndexingService) stopped() bool {
	select {
	case <-is.done:
		return true
	default:
		return false
	}
}
