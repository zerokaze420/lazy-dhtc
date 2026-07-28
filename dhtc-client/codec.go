// Package dhtc_client provides a DHT client implementation.
package dhtc_client

import (
	"encoding/binary"
	"fmt"
	"net/netip"

	"github.com/anacrolix/missinggo/v2/iter"
	"github.com/anacrolix/torrent/bencode"
	"github.com/willf/bloom"
)

// Message represents a KRPC message.
type Message struct {
	// Q is the Query method. One of 5:
	//   - "ping"
	//   - "find_node"
	//   - "get_peers"
	//   - "announce_peer"
	//   - "sample_infohashes" (added by BEP 51)
	Q string `bencode:"q,omitempty"`
	// A contains the named QueryArguments sent with a query.
	A QueryArguments `bencode:"a,omitempty"`
	// T is the required transaction ID.
	T []byte `bencode:"t"`
	// Y is the required type of the message: q for QUERY, r for RESPONSE, e for ERROR.
	Y string `bencode:"y"`
	// R is the RESPONSE type only.
	R ResponseValues `bencode:"r,omitempty"`
	// E is the ERROR type only.
	E Error `bencode:"e,omitempty"`
}

// QueryArguments represents the "a" dictionary in a DHT query.
type QueryArguments struct {
	// ID is the ID of the querying Node.
	ID []byte `bencode:"id"`
	// InfoHash is the InfoHash of the torrent.
	InfoHash []byte `bencode:"info_hash,omitempty"`
	// Target is the ID of the node sought.
	Target []byte `bencode:"target,omitempty"`
	// Token is the token received from an earlier get_peers query.
	Token []byte `bencode:"token,omitempty"`
	// Port is the senders torrent port.
	Port int `bencode:"port,omitempty"`
	// ImpliedPort indicates if the senders apparent DHT port should be used.
	ImpliedPort int `bencode:"implied_port,omitempty"`

	// Seed indicates whether the querying node is seeding the torrent it announces.
	// Defined in BEP 33 "DHT Scrapes" for `announce_peer` queries.
	Seed int `bencode:"seed,omitempty"`

	// NoSeed indicates if the responding node should try to fill the `values` list with non-seed items.
	// Defined in BEP 33 "DHT Scrapes" for `get_peers` queries.
	NoSeed int `bencode:"noseed,omitempty"`
	// Scrape indicates if the responding node should add Bloom Filters to the response.
	// Defined in BEP 33 "DHT Scrapes" for `get_peers` queries.
	Scrape int `bencode:"scrape,omitempty"`
	// Want requests IPv4 (n4), IPv6 (n6), or both node address families (BEP 32).
	Want []string `bencode:"want,omitempty"`
}

// ResponseValues represents the "r" dictionary in a DHT response.
type ResponseValues struct {
	// ID of the responding node.
	ID []byte `bencode:"id"`
	// Nodes is a list of K closest nodes to the requested target (IPv4).
	Nodes CompactNodeInfos4 `bencode:"nodes,omitempty"`
	// Nodes6 is a list of K closest nodes to the requested target (IPv6).
	Nodes6 CompactNodeInfos6 `bencode:"nodes6,omitempty"`
	// Token for future announce_peer.
	Token []byte `bencode:"token,omitempty"`
	// Values is a list of torrent peers.
	Values CompactPeers `bencode:"values,omitempty"`

	// Interval is the subset refresh interval in seconds (BEP 51).
	Interval int `bencode:"interval,omitempty"`
	// Num is the number of infohashes in storage (BEP 51).
	Num int `bencode:"num,omitempty"`
	// Samples is a subset of stored infohashes, N × 20 bytes (BEP 51).
	Samples []byte `bencode:"samples,omitempty"`
	// Samples2 is a subset of stored 32-byte infohashes (BEP 52).
	Samples2 [][]byte `bencode:"samples2,omitempty"`

	// BFsd is a Bloom Filter (256 bytes) representing all stored seeds for that infohash (BEP 33).
	BFsd *bloom.BloomFilter `bencode:"BFsd,omitempty"`
	// BFpe is a Bloom Filter (256 bytes) representing all stored peers for that infohash (BEP 33).
	BFpe *bloom.BloomFilter `bencode:"BFpe,omitempty"`
}

// Error represents a KRPC error.
type Error struct {
	Code    int
	Message []byte
}

// CompactPeer represents a peer's IP and port.
type CompactPeer struct {
	Addr netip.AddrPort
}

// CompactPeers is a slice of CompactPeer.
type CompactPeers []CompactPeer

// CompactNodeInfo represents a node's ID and address.
type CompactNodeInfo struct {
	ID   []byte
	Addr netip.AddrPort
}

// CompactNodeInfos is a slice of CompactNodeInfo.
type CompactNodeInfos []CompactNodeInfo

type CompactNodeInfos4 CompactNodeInfos
type CompactNodeInfos6 CompactNodeInfos

// UnmarshalBencode unmarshals the compact peers from bencode.
// It supports both a list of strings and a single string.
func (cps *CompactPeers) UnmarshalBencode(b []byte) error {
	var list [][]byte
	if err := bencode.Unmarshal(b, &list); err == nil {
		*cps = make(CompactPeers, 0, len(list))
		for _, s := range list {
			var cp CompactPeer
			if err := cp.UnmarshalBinary(s); err != nil {
				return err
			}
			*cps = append(*cps, cp)
		}
		return nil
	}
	var bb []byte
	if err := bencode.Unmarshal(b, &bb); err != nil {
		return err
	}
	var err error
	*cps, err = UnmarshalCompactPeers(bb)
	return err
}

// MarshalBencode marshals the compact peers to bencode.
func (cps *CompactPeers) MarshalBencode() ([]byte, error) {
	list := make([][]byte, 0, len(*cps))
	for _, cp := range *cps {
		list = append(list, cp.MarshalBinary())
	}
	return bencode.Marshal(list)
}

// MarshalBinary marshals the compact peer to binary.
func (cp *CompactPeer) MarshalBinary() []byte {
	if !cp.Addr.IsValid() {
		return nil
	}
	ip := cp.Addr.Addr().AsSlice()
	ret := make([]byte, len(ip)+2)
	copy(ret, ip)
	binary.BigEndian.PutUint16(ret[len(ip):], cp.Addr.Port())
	return ret
}

// UnmarshalBinary unmarshals the compact peer from binary.
func (cp *CompactPeer) UnmarshalBinary(b []byte) error {
	var ip netip.Addr
	var ok bool
	switch len(b) {
	case 18:
		ip, ok = netip.AddrFromSlice(b[:16])
	case 6:
		ip, ok = netip.AddrFromSlice(b[:4])
	default:
		return fmt.Errorf("bad compact peer string: %q", b)
	}
	if !ok {
		return fmt.Errorf("invalid compact peer address")
	}
	cp.Addr = netip.AddrPortFrom(ip.Unmap(), binary.BigEndian.Uint16(b[len(b)-2:]))
	return nil
}

// UnmarshalCompactPeers unmarshals the compact peers from a byte slice.
func UnmarshalCompactPeers(b []byte) (ret CompactPeers, err error) {
	if len(b) == 0 {
		return nil, nil
	}

	var peerSize int
	if len(b)%6 == 0 {
		peerSize = 6
	} else if len(b)%18 == 0 {
		peerSize = 18
	} else {
		return nil, fmt.Errorf("compact peer info length %d is neither a multiple of 6 nor 18", len(b))
	}

	num := len(b) / peerSize
	ret = make(CompactPeers, num)
	for i := range iter.N(num) {
		off := i * peerSize
		err = ret[i].UnmarshalBinary(b[off : off+peerSize])
		if err != nil {
			return
		}
	}
	return
}

// UnmarshalBencode unmarshals the compact node infos from bencode.
func (cnis *CompactNodeInfos) UnmarshalBencode(b []byte) (err error) {
	var bb []byte
	err = bencode.Unmarshal(b, &bb)
	if err != nil {
		return
	}
	*cnis, err = UnmarshalCompactNodeInfos(bb)
	return
}

func (cnis *CompactNodeInfos4) UnmarshalBencode(b []byte) error {
	nodes, err := unmarshalCompactNodeInfosBencode(b, 26)
	*cnis = CompactNodeInfos4(nodes)
	return err
}

func (cnis *CompactNodeInfos6) UnmarshalBencode(b []byte) error {
	nodes, err := unmarshalCompactNodeInfosBencode(b, 38)
	*cnis = CompactNodeInfos6(nodes)
	return err
}

func unmarshalCompactNodeInfosBencode(b []byte, nodeSize int) (CompactNodeInfos, error) {
	var raw []byte
	if err := bencode.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	if len(raw)%nodeSize != 0 {
		return nil, fmt.Errorf("compact node length %d is not a multiple of %d", len(raw), nodeSize)
	}
	nodes := make(CompactNodeInfos, len(raw)/nodeSize)
	for i := range nodes {
		if err := nodes[i].UnmarshalBinary(raw[i*nodeSize : (i+1)*nodeSize]); err != nil {
			return nil, err
		}
	}
	return nodes, nil
}

// UnmarshalCompactNodeInfos unmarshals the compact node infos from a byte slice.
func UnmarshalCompactNodeInfos(b []byte) (ret []CompactNodeInfo, err error) {
	if len(b) == 0 {
		return nil, nil
	}

	var nodeSize int
	if len(b)%26 == 0 {
		nodeSize = 26
	} else if len(b)%38 == 0 {
		nodeSize = 38
	} else {
		err = fmt.Errorf("compact node is neither a multiple of 26 nor 38")
		return
	}

	num := len(b) / nodeSize
	ret = make([]CompactNodeInfo, num)
	for i := range iter.N(num) {
		off := i * nodeSize
		err = ret[i].UnmarshalBinary(b[off : off+nodeSize])
		if err != nil {
			return
		}
	}
	return
}

// UnmarshalBinary unmarshals the compact node info from binary.
func (cni *CompactNodeInfo) UnmarshalBinary(b []byte) error {
	if len(b) != 26 && len(b) != 38 {
		return fmt.Errorf("invalid compact node info length: %d", len(b))
	}
	if len(cni.ID) != 20 {
		cni.ID = make([]byte, 20)
	}
	copy(cni.ID, b[:20])
	b = b[20:]

	var ipLen int
	if len(b) == 6 {
		ipLen = 4
	} else {
		ipLen = 16
	}

	ip, ok := netip.AddrFromSlice(b[:ipLen])
	if !ok {
		return fmt.Errorf("invalid compact node address")
	}
	b = b[ipLen:]
	cni.Addr = netip.AddrPortFrom(ip.Unmap(), binary.BigEndian.Uint16(b))
	return nil
}

// MarshalBencode marshals the compact node infos to bencode.
func (cnis *CompactNodeInfos) MarshalBencode() ([]byte, error) {
	var ret []byte

	if len(*cnis) == 0 {
		return []byte("0:"), nil
	}

	for _, cni := range *cnis {
		ret = append(ret, cni.MarshalBinary()...)
	}

	return bencode.Marshal(ret)
}

func (cnis CompactNodeInfos4) MarshalBencode() ([]byte, error) {
	nodes := CompactNodeInfos(cnis)
	return (&nodes).MarshalBencode()
}

func (cnis CompactNodeInfos6) MarshalBencode() ([]byte, error) {
	nodes := CompactNodeInfos(cnis)
	return (&nodes).MarshalBencode()
}

// MarshalBinary marshals the compact node info to binary.
func (cni *CompactNodeInfo) MarshalBinary() []byte {
	ret := make([]byte, 20)
	copy(ret, cni.ID)

	if !cni.Addr.IsValid() {
		return nil
	}
	ip := cni.Addr.Addr().AsSlice()
	ret = append(ret, ip...)

	portEncoding := make([]byte, 2)
	binary.BigEndian.PutUint16(portEncoding, cni.Addr.Port())
	ret = append(ret, portEncoding...)

	return ret
}

// MarshalBencode marshals the error to bencode.
func (e *Error) MarshalBencode() ([]byte, error) {
	return bencode.Marshal([]any{e.Code, e.Message})
}

// UnmarshalBencode unmarshals the error from bencode.
func (e *Error) UnmarshalBencode(b []byte) (err error) {
	var i any
	err = bencode.Unmarshal(b, &i)
	if err != nil {
		return err
	}

	l, ok := i.([]any)
	if !ok {
		return fmt.Errorf("invalid error type: %T", i)
	}

	if len(l) < 2 {
		return fmt.Errorf("invalid error list length: %d", len(l))
	}

	code, ok := l[0].(int64)
	if !ok {
		return fmt.Errorf("invalid error code type: %T", l[0])
	}
	e.Code = int(code)

	switch v := l[1].(type) {
	case []byte:
		e.Message = v
	case string:
		e.Message = []byte(v)
	default:
		return fmt.Errorf("invalid error message type: %T", l[1])
	}

	return nil
}
