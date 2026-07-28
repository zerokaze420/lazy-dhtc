package dhtc_client

import (
	"container/list"
	"context"
	"crypto/rand"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type IndexingService struct {
	// Private
	protocol      *Protocol
	network       string
	want          []string
	started       bool
	interval      time.Duration
	eventHandlers IndexingServiceEventHandlers
	done          chan struct{}
	stopOnce      sync.Once

	nodeID            []byte
	routingTable      map[string]*net.UDPAddr
	lastSeen          map[string]time.Time
	lruList           *list.List
	lruElements       map[string]*list.Element
	routingTableMutex sync.RWMutex
	maxNeighbors      uint

	counter          uint16
	getPeersRequests map[[2]byte][]byte // GetPeersQuery.`t` -> infohash
}

type IndexingServiceEventHandlers struct {
	OnResult func(IndexingResult)
	OnNodes6 func(CompactNodeInfos)
}

type IndexingResult struct {
	infoHash  []byte
	peerAddrs []net.TCPAddr
}

func (ir IndexingResult) InfoHash() []byte {
	return ir.infoHash
}

func (ir IndexingResult) PeerAddrs() []net.TCPAddr {
	return ir.peerAddrs
}

func NewIndexingService(network string, laddr string, interval time.Duration, maxNeighbors uint, rateLimit int, eventHandlers IndexingServiceEventHandlers) *IndexingService {
	service := new(IndexingService)
	service.network = network
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
			OnFindNodeResponse:         service.onFindNodeResponse,
			OnGetPeersResponse:         service.onGetPeersResponse,
			OnSampleInfohashesResponse: service.onSampleInfohashesResponse,
			OnSampleInfohashesQuery:    service.onSampleInfohashesQuery,
		},
	)
	service.nodeID = make([]byte, 20)
	service.routingTable = make(map[string]*net.UDPAddr)
	service.lastSeen = make(map[string]time.Time)
	service.lruList = list.New()
	service.lruElements = make(map[string]*list.Element)
	service.maxNeighbors = maxNeighbors
	service.eventHandlers = eventHandlers
	service.done = make(chan struct{})

	service.getPeersRequests = make(map[[2]byte][]byte)

	return service
}

func (is *IndexingService) Start(nodes []string) {
	if is.started {
		log.Panic().Msg("Attempting to Start() a mainline/IndexingService that has been already started! (Programmer error.)")
	}
	is.started = true

	is.protocol.Start()
	go is.index(nodes)
}

func (is *IndexingService) Terminate() {
	is.stopOnce.Do(func() {
		close(is.done)
		is.protocol.Terminate()
	})
}

func (is *IndexingService) index(nodes []string) {
	ticker := time.NewTicker(is.interval)
	defer ticker.Stop()

	for {
		select {
		case <-is.done:
			return
		case <-ticker.C:
		}

		is.routingTableMutex.RLock()
		routingTableLen := len(is.routingTable)
		is.routingTableMutex.RUnlock()

		if routingTableLen == 0 {
			is.bootstrap(nodes)
		} else {
			is.findNeighbors()

			// Prune dead nodes
			is.routingTableMutex.Lock()
			for {
				back := is.lruList.Back()
				if back == nil {
					break
				}
				id, _ := back.Value.(string)
				seen := is.lastSeen[id]
				if time.Since(seen) > 5*time.Minute {
					is.lruList.Remove(back)
					delete(is.lruElements, id)
					delete(is.routingTable, id)
					delete(is.lastSeen, id)
				} else {
					break
				}
			}

			is.routingTableMutex.Unlock()
		}
	}
}

func (is *IndexingService) bootstrap(nodes []string) {
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
			is.protocol.SendMessage(NewFindNodeQuery(is.nodeID, target, is.want), addr)
		}
	}
}

func (is *IndexingService) findNeighbors() {
	if is.stopped() {
		return
	}

	target := make([]byte, 20)

	/*
		We could just RLock and defer RUnlock here, but that would mean that each response that we get could not Lock
		the table because we are sending. So we would basically make read and write NOT concurrent.
		A better approach would be to get all addresses to send in a slice and then work on that, releasing the main map.
	*/
	is.routingTableMutex.RLock()
	addressesToSend := make([]*net.UDPAddr, 0, len(is.routingTable))
	for _, addr := range is.routingTable {
		addressesToSend = append(addressesToSend, addr)
	}
	is.routingTableMutex.RUnlock()

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

func (is *IndexingService) addNode(id []byte, addr *net.UDPAddr) {
	if is.stopped() {
		return
	}

	if addr.Port == 0 {
		return
	}
	is4 := addr.IP.To4() != nil
	if (is.network == "udp4" && !is4) || (is.network == "udp6" && is4) {
		return
	}

	is.routingTableMutex.Lock()
	defer is.routingTableMutex.Unlock()

	sid := string(id)
	if _, exists := is.routingTable[sid]; exists {
		is.lastSeen[sid] = time.Now()
		is.lruList.MoveToFront(is.lruElements[sid])
		return
	}

	if uint(len(is.routingTable)) >= is.maxNeighbors {
		back := is.lruList.Back()
		if back != nil {
			oldestID, _ := back.Value.(string)
			is.lruList.Remove(back)
			delete(is.lruElements, oldestID)
			delete(is.routingTable, oldestID)
			delete(is.lastSeen, oldestID)
		}
	}

	is.routingTable[sid] = addr
	is.lastSeen[sid] = time.Now()
	is.lruElements[sid] = is.lruList.PushFront(sid)

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

func (is *IndexingService) onFindNodeResponse(response *Message, addr *net.UDPAddr) {
	if is.stopped() {
		return
	}

	is.addNode(response.R.ID, addr)

	for _, node := range response.R.Nodes {
		is.addNode(node.ID, &node.Addr)
	}
	for _, node := range response.R.Nodes6 {
		is.addNode(node.ID, &node.Addr)
	}
	is.reportNodes6(response.R.Nodes6)
}

func (is *IndexingService) onGetPeersResponse(msg *Message, addr *net.UDPAddr) {
	if is.stopped() {
		return
	}

	is.addNode(msg.R.ID, addr)

	var t [2]byte
	copy(t[:], msg.T)

	infoHash := is.getPeersRequests[t]
	// We got a response, so free the key!
	delete(is.getPeersRequests, t)

	// BEP 51 specifies that
	//     The new sample_infohashes remote procedure call requests that a remote node return a string of multiple
	//     concatenated infohashes (20 bytes each) FOR WHICH IT HOLDS GET_PEERS VALUES.
	//                                                                          ^^^^^^
	// So theoretically we should never hit the case where `values` is empty, but c'est la vie.
	if len(msg.R.Values) == 0 {
		return
	}

	peerAddrs := make([]net.TCPAddr, 0)
	for _, peer := range msg.R.Values {
		if peer.Port == 0 {
			continue
		}

		peerAddrs = append(peerAddrs, net.TCPAddr{
			IP:   peer.IP,
			Port: peer.Port,
		})
	}

	is.eventHandlers.OnResult(IndexingResult{
		infoHash:  infoHash,
		peerAddrs: peerAddrs,
	})
}

func (is *IndexingService) onSampleInfohashesResponse(msg *Message, addr *net.UDPAddr) {
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
		is.addNode(node.ID, &node.Addr)
	}

	for _, node := range msg.R.Nodes6 {
		is.addNode(node.ID, &node.Addr)
	}
	is.reportNodes6(msg.R.Nodes6)
}

func (is *IndexingService) AddNodes(nodes CompactNodeInfos) {
	for _, node := range nodes {
		is.addNode(node.ID, &node.Addr)
	}
}

func (is *IndexingService) reportNodes6(nodes CompactNodeInfos) {
	if len(nodes) > 0 && is.eventHandlers.OnNodes6 != nil {
		is.eventHandlers.OnNodes6(nodes)
	}
}

func (is *IndexingService) requestPeers(infoHash []byte, addr *net.UDPAddr) {
	if is.stopped() {
		return
	}

	msg := NewGetPeersQuery(is.nodeID, infoHash, is.want)
	t := uint16BE(is.counter)
	msg.T = t[:]

	is.protocol.SendMessage(msg, addr)

	is.getPeersRequests[t] = infoHash
	is.counter++
}

func (is *IndexingService) onSampleInfohashesQuery(msg *Message, addr *net.UDPAddr) {
	if is.stopped() {
		return
	}

	is.routingTableMutex.RLock()
	// Get some nodes from routing table
	nodes := make(CompactNodeInfos, 0)
	for id, addr := range is.routingTable {
		nodes = append(nodes, CompactNodeInfo{
			ID:   []byte(id),
			Addr: *addr,
		})
		if len(nodes) >= 8 {
			break
		}
	}
	is.routingTableMutex.RUnlock()

	nodes4 := make(CompactNodeInfos, 0, len(nodes))
	nodes6 := make(CompactNodeInfos, 0, len(nodes))
	for _, node := range nodes {
		if node.Addr.IP.To4() == nil {
			nodes6 = append(nodes6, node)
		} else {
			nodes4 = append(nodes4, node)
		}
	}
	response := NewSampleInfohashesResponse(msg.T, is.nodeID, int(is.interval.Seconds()), nodes4, nodes6, 0, nil)
	is.protocol.SendMessage(response, addr)
}

func resolveBootstrapAddresses(network, node string) ([]*net.UDPAddr, error) {
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
	addrs := make([]*net.UDPAddr, 0, len(ips))
	for _, ip := range ips {
		is4 := ip.To4() != nil
		if (network == "udp4" && !is4) || (network == "udp6" && is4) {
			continue
		}
		addrs = append(addrs, &net.UDPAddr{IP: ip, Port: port})
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
