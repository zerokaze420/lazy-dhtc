package dhtc_client

import (
	"crypto/rand"
	"net/netip"
	"testing"
)

func TestVerifyToken(t *testing.T) {
	p := NewProtocol("udp4", ":0", 100, ProtocolEventHandlers{})
	addr := netip.MustParseAddr("127.0.0.1")
	token := p.CalculateToken(addr)

	if !p.VerifyToken(addr, token) {
		t.Error("VerifyToken failed for current token")
	}

	// Test with wrong address
	wrongAddr := netip.MustParseAddr("127.0.0.2")
	if p.VerifyToken(wrongAddr, token) {
		t.Error("VerifyToken should fail for wrong address")
	}

	// Test with wrong token
	wrongToken := make([]byte, len(token))
	copy(wrongToken, token)
	wrongToken[0] ^= 0xFF
	if p.VerifyToken(addr, wrongToken) {
		t.Error("VerifyToken should fail for wrong token")
	}

	// Test with previous token
	p.tokenLock.Lock()
	copy(p.previousTokenSecret, p.currentTokenSecret)
	_, _ = rand.Read(p.currentTokenSecret)
	p.tokenLock.Unlock()

	if !p.VerifyToken(addr, token) {
		t.Error("VerifyToken failed for previous token")
	}

	newToken := p.CalculateToken(addr)
	if !p.VerifyToken(addr, newToken) {
		t.Error("VerifyToken failed for new current token")
	}
}

func TestQueriesIncludeWantedAddressFamily(t *testing.T) {
	want := []string{"n4", "n6"}
	queries := []*Message{
		NewFindNodeQuery(make([]byte, 20), make([]byte, 20), want),
		NewGetPeersQuery(make([]byte, 20), make([]byte, 20), want),
		NewSampleInfohashesQuery(make([]byte, 20), []byte("aa"), make([]byte, 20), want),
	}
	for _, query := range queries {
		if len(query.A.Want) != 2 || query.A.Want[0] != "n4" || query.A.Want[1] != "n6" {
			t.Fatalf("query does not preserve want: %#v", query.A.Want)
		}
	}
}

func TestFindNodeResponseAcceptsNodes6Only(t *testing.T) {
	msg := &Message{
		Y: "r",
		T: []byte("aa"),
		R: ResponseValues{
			ID: make([]byte, 20),
			Nodes6: CompactNodeInfos6{{
				ID:   make([]byte, 20),
				Addr: netip.MustParseAddrPort("[2001:db8::1]:6881"),
			}},
		},
	}
	if !validateFindNodeResponseMessage(msg) {
		t.Fatal("IPv6-only find_node response should be valid")
	}
}
