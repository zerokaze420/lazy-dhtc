package dhtc_client

import (
	"context"
	"net"
	"net/netip"
	"sync"

	"github.com/anacrolix/torrent/bencode"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
)

type sendRequest struct {
	msg  *Message
	addr netip.AddrPort
}

// Transport represents a DHT transport layer.
type Transport struct {
	network string
	fd      net.PacketConn
	laddr   string
	started bool
	buffer  []byte

	limiter  *rate.Limiter
	sendChan chan sendRequest
	done     chan struct{}
	stopOnce sync.Once
	ctx      context.Context
	cancel   context.CancelFunc
	group    *errgroup.Group

	// OnMessage is the function that will be called when Transport receives a packet that is
	// successfully unmarshalled as a syntactically correct Message (but, of course, checking
	// the semantic correctness of the Message is left to Protocol).
	onMessage func(*Message, netip.AddrPort)
	// OnCongestion
	onCongestion func()
}

// NewTransport creates a new DHT transport layer.
func NewTransport(network string, laddr string, rateLimit int, onMessage func(*Message, netip.AddrPort), onCongestion func()) *Transport {
	t := new(Transport)
	t.network = network
	/*   The field size sets a theoretical limit of 65,535 bytes (8 byte header + 65,527 bytes of
	 * data) for a UDP datagram. However, the actual limit for the data length, which is imposed by
	 * the underlying IPv4 protocol, is 65,507 bytes (65,535 − 8 byte UDP header − 20 byte IP
	 * header).
	 *
	 *   In IPv6 jumbograms it is possible to have UDP packets of size greater than 65,535 bytes.
	 * RFC 2675 specifies that the length field is set to zero if the length of the UDP header plus
	 * UDP data is greater than 65,535.
	 *
	 * https://en.wikipedia.org/wiki/User_Datagram_Protocol
	 */
	t.buffer = make([]byte, 65507)
	t.onMessage = onMessage
	t.onCongestion = onCongestion

	if rateLimit > 0 {
		t.limiter = rate.NewLimiter(rate.Limit(rateLimit), rateLimit)
	}
	t.sendChan = make(chan sendRequest, 2048)
	t.done = make(chan struct{})

	t.laddr = laddr

	return t
}

// Start starts the DHT transport layer.
func (t *Transport) Start(parent context.Context) {
	// Why check whether the Transport `t` started or not, here and not -for instance- in
	// t.Terminate()?
	// Because in t.Terminate() the programmer (i.e. you & me) would stumble upon an error while
	// trying close an uninitialised net.UDPConn or something like that: it's mostly harmless
	// because its effects are immediate. But if you try to start a Transport `t` for the second
	// (or the third, 4th, ...) time, it will keep spawning goroutines and any small mistake may
	// end up in a debugging horror.
	//                                                                   Here ends my justification.
	if t.started {
		log.Panic().Msg("Attempting to Start() a mainline/Transport that has been already started! (Programmer error.)")
	}
	t.started = true
	t.ctx, t.cancel = context.WithCancel(parent)
	t.group, t.ctx = errgroup.WithContext(t.ctx)

	var err error
	t.fd, err = net.ListenPacket(t.network, t.laddr)

	if err != nil {
		log.Fatal().Msg("Could NOT bind the socket!")
		log.Fatal().Err(err)
	}

	t.group.Go(t.readMessages)
	t.group.Go(t.sendLoop)
}

// Terminate terminates the DHT transport layer.
func (t *Transport) Terminate() {
	t.stopOnce.Do(func() {
		close(t.done)
		if t.cancel != nil {
			t.cancel()
		}
		if t.fd != nil {
			_ = t.fd.Close()
		}
		if t.group != nil {
			_ = t.group.Wait()
		}
	})
}

// readMessages is a goroutine!
func (t *Transport) readMessages() error {
	for {
		n, fromSA, err := t.fd.ReadFrom(t.buffer)
		if err != nil {
			select {
			case <-t.done:
				return nil
			default:
				log.Warn().Err(err).Msg("Could NOT read an UDP packet!")
				continue
			}
		}

		if n == 0 {
			/* Datagram sockets in various domains  (e.g., the UNIX and Internet domains) permit
			 * zero-length datagrams. When such a datagram is received, the return value (n) is 0.
			 */
			continue
		}

		var msg Message
		err = bencode.Unmarshal(t.buffer[:n], &msg)
		if err != nil {
			// couldn't unmarshal packet data
			continue
		}

		addr, err := netip.ParseAddrPort(fromSA.String())
		if err != nil {
			continue
		}
		t.onMessage(&msg, addr)
	}
}

// WriteMessages writes a KRPC message to the specified address.
func (t *Transport) WriteMessages(msg *Message, addr netip.AddrPort) {
	select {
	case <-t.done:
		return
	case t.sendChan <- sendRequest{msg, addr}:
	default:
		// Drop message if channel is full
	}
}

func (t *Transport) sendLoop() error {
	for {
		select {
		case <-t.done:
			return nil
		case req := <-t.sendChan:
			select {
			case <-t.done:
				return nil
			default:
			}
			t.send(req)
		}
	}
}

func (t *Transport) send(req sendRequest) {
	if t.limiter != nil {
		if err := t.limiter.Wait(t.ctx); err != nil {
			return
		}
	}

	select {
	case <-t.done:
		return
	default:
		t.writeImmediately(req.msg, req.addr)
	}
}

func (t *Transport) writeImmediately(msg *Message, addr netip.AddrPort) {
	data, err := bencode.Marshal(msg)
	if err != nil {
		log.Panic().Msg("Could NOT marshal an outgoing message! (Programmer error.)")
	}

	_, err = t.fd.WriteTo(data, net.UDPAddrFromAddrPort(addr))
	if err != nil {
		select {
		case <-t.done:
			return
		default:
		}
		log.Warn().Msg("Could NOT write an UDP packet!")
		log.Warn().Err(err)
	}
}
