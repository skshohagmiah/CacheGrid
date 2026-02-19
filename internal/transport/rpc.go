package transport

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/vmihailenco/msgpack/v5"
)

// Operation codes for the RPC protocol.
const (
	opGet          byte = 1
	opSet          byte = 2
	opDelete       byte = 3
	opExists       byte = 4
	opMGet         byte = 5
	opIncr         byte = 6
	opLockAcquire  byte = 7
	opLockRelease  byte = 8
	opLockExtend   byte = 9
)

// request is the wire format for an RPC request.
type request struct {
	Op    byte          `msgpack:"op"`
	Key   string        `msgpack:"key,omitempty"`
	Keys  []string      `msgpack:"keys,omitempty"`
	Value []byte        `msgpack:"value,omitempty"`
	TTL   time.Duration `msgpack:"ttl,omitempty"`
	Tags  []string      `msgpack:"tags,omitempty"`
	Delta int64         `msgpack:"delta,omitempty"`
	Token string        `msgpack:"token,omitempty"`
}

// response is the wire format for an RPC response.
type response struct {
	Value        []byte            `msgpack:"value,omitempty"`
	Values       map[string][]byte `msgpack:"values,omitempty"`
	Found        bool              `msgpack:"found,omitempty"`
	OK           bool              `msgpack:"ok,omitempty"`
	IntVal       int64             `msgpack:"int_val,omitempty"`
	Token        string            `msgpack:"token,omitempty"`
	FencingToken uint64            `msgpack:"fencing_token,omitempty"`
	Acquired     bool              `msgpack:"acquired,omitempty"`
	Error        string            `msgpack:"error,omitempty"`
}

// TCPTransport implements Transport using TCP with msgpack encoding.
type TCPTransport struct {
	mu       sync.RWMutex
	conns    map[string]net.Conn
	handler  LocalHandler
	listener net.Listener
	timeout  time.Duration
	done     chan struct{}
}

// NewTCPTransport creates a new TCP-based transport.
func NewTCPTransport(handler LocalHandler, timeout time.Duration) *TCPTransport {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &TCPTransport{
		conns:   make(map[string]net.Conn),
		handler: handler,
		timeout: timeout,
		done:    make(chan struct{}),
	}
}

func (t *TCPTransport) Start(listenAddr string) error {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("transport: failed to listen on %s: %w", listenAddr, err)
	}
	t.listener = ln
	go t.acceptLoop()
	return nil
}

func (t *TCPTransport) Stop() error {
	close(t.done)
	if t.listener != nil {
		t.listener.Close()
	}
	t.mu.Lock()
	for addr, conn := range t.conns {
		conn.Close()
		delete(t.conns, addr)
	}
	t.mu.Unlock()
	return nil
}

func (t *TCPTransport) acceptLoop() {
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			select {
			case <-t.done:
				return
			default:
				continue
			}
		}
		go t.handleConn(conn)
	}
}

func (t *TCPTransport) handleConn(conn net.Conn) {
	defer conn.Close()
	dec := msgpack.NewDecoder(conn)
	enc := msgpack.NewEncoder(conn)

	for {
		var req request
		if err := dec.Decode(&req); err != nil {
			if err == io.EOF {
				return
			}
			return
		}

		resp := t.processRequest(&req)
		if err := enc.Encode(resp); err != nil {
			return
		}
	}
}

func (t *TCPTransport) processRequest(req *request) response {
	switch req.Op {
	case opGet:
		val, found := t.handler.HandleGet(req.Key)
		return response{Value: val, Found: found}
	case opSet:
		t.handler.HandleSet(req.Key, req.Value, req.TTL, req.Tags)
		return response{OK: true}
	case opDelete:
		ok := t.handler.HandleDelete(req.Key)
		return response{OK: ok}
	case opExists:
		ok := t.handler.HandleExists(req.Key)
		return response{OK: ok}
	case opMGet:
		vals := t.handler.HandleMGet(req.Keys)
		return response{Values: vals}
	case opIncr:
		val, err := t.handler.HandleIncr(req.Key, req.Delta)
		resp := response{IntVal: val}
		if err != nil {
			resp.Error = err.Error()
		}
		return resp
	case opLockAcquire:
		token, fencing, acquired := t.handler.HandleLockAcquire(req.Key, req.TTL)
		return response{Token: token, FencingToken: fencing, Acquired: acquired}
	case opLockRelease:
		ok := t.handler.HandleLockRelease(req.Key, req.Token)
		return response{OK: ok}
	case opLockExtend:
		ok := t.handler.HandleLockExtend(req.Key, req.Token, req.TTL)
		return response{OK: ok}
	default:
		return response{Error: "unknown operation"}
	}
}

// getConn returns a pooled connection or creates a new one.
func (t *TCPTransport) getConn(addr string) (net.Conn, error) {
	t.mu.RLock()
	conn, ok := t.conns[addr]
	t.mu.RUnlock()
	if ok {
		return conn, nil
	}

	conn, err := net.DialTimeout("tcp", addr, t.timeout)
	if err != nil {
		return nil, err
	}

	t.mu.Lock()
	// Check again in case another goroutine created it
	if existing, ok := t.conns[addr]; ok {
		t.mu.Unlock()
		conn.Close()
		return existing, nil
	}
	t.conns[addr] = conn
	t.mu.Unlock()
	return conn, nil
}

// closeConn removes and closes a pooled connection.
func (t *TCPTransport) closeConn(addr string) {
	t.mu.Lock()
	if conn, ok := t.conns[addr]; ok {
		conn.Close()
		delete(t.conns, addr)
	}
	t.mu.Unlock()
}

func (t *TCPTransport) call(ctx context.Context, addr string, req *request) (*response, error) {
	conn, err := t.getConn(addr)
	if err != nil {
		return nil, fmt.Errorf("transport: dial %s: %w", addr, err)
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(t.timeout)
	}
	conn.SetDeadline(deadline)

	enc := msgpack.NewEncoder(conn)
	dec := msgpack.NewDecoder(conn)

	if err := enc.Encode(req); err != nil {
		t.closeConn(addr)
		return nil, fmt.Errorf("transport: encode to %s: %w", addr, err)
	}

	var resp response
	if err := dec.Decode(&resp); err != nil {
		t.closeConn(addr)
		return nil, fmt.Errorf("transport: decode from %s: %w", addr, err)
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("transport: remote error from %s: %s", addr, resp.Error)
	}

	return &resp, nil
}

// CloseConnection closes a specific peer connection (called on node leave).
func (t *TCPTransport) CloseConnection(addr string) {
	t.closeConn(addr)
}

// --- Transport interface implementation ---

func (t *TCPTransport) RemoteGet(ctx context.Context, addr string, key string) ([]byte, bool, error) {
	resp, err := t.call(ctx, addr, &request{Op: opGet, Key: key})
	if err != nil {
		return nil, false, err
	}
	return resp.Value, resp.Found, nil
}

func (t *TCPTransport) RemoteSet(ctx context.Context, addr string, key string, value []byte, ttl time.Duration, tags []string) error {
	_, err := t.call(ctx, addr, &request{Op: opSet, Key: key, Value: value, TTL: ttl, Tags: tags})
	return err
}

func (t *TCPTransport) RemoteDelete(ctx context.Context, addr string, key string) error {
	_, err := t.call(ctx, addr, &request{Op: opDelete, Key: key})
	return err
}

func (t *TCPTransport) RemoteExists(ctx context.Context, addr string, key string) (bool, error) {
	resp, err := t.call(ctx, addr, &request{Op: opExists, Key: key})
	if err != nil {
		return false, err
	}
	return resp.OK, nil
}

func (t *TCPTransport) RemoteMGet(ctx context.Context, addr string, keys []string) (map[string][]byte, error) {
	resp, err := t.call(ctx, addr, &request{Op: opMGet, Keys: keys})
	if err != nil {
		return nil, err
	}
	return resp.Values, nil
}

func (t *TCPTransport) RemoteIncr(ctx context.Context, addr string, key string, delta int64) (int64, error) {
	resp, err := t.call(ctx, addr, &request{Op: opIncr, Key: key, Delta: delta})
	if err != nil {
		return 0, err
	}
	return resp.IntVal, nil
}

func (t *TCPTransport) RemoteLockAcquire(ctx context.Context, addr string, key string, ttl time.Duration) (string, uint64, bool, error) {
	resp, err := t.call(ctx, addr, &request{Op: opLockAcquire, Key: key, TTL: ttl})
	if err != nil {
		return "", 0, false, err
	}
	return resp.Token, resp.FencingToken, resp.Acquired, nil
}

func (t *TCPTransport) RemoteLockRelease(ctx context.Context, addr string, key string, token string) (bool, error) {
	resp, err := t.call(ctx, addr, &request{Op: opLockRelease, Key: key, Token: token})
	if err != nil {
		return false, err
	}
	return resp.OK, nil
}

func (t *TCPTransport) RemoteLockExtend(ctx context.Context, addr string, key string, token string, ttl time.Duration) (bool, error) {
	resp, err := t.call(ctx, addr, &request{Op: opLockExtend, Key: key, Token: token, TTL: ttl})
	if err != nil {
		return false, err
	}
	return resp.OK, nil
}
