package connpool

import (
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	// DefaultMaxConnsPerHost is the default number of connections towards an
	// address.
	DefaultMaxConnsPerHost = 10
)

// ErrTimeout represents that no connection could be grabbed from the pool after
// the connection timeout.
type ErrTimeout struct{}

func (err ErrTimeout) Error() string {
	return "dial timeout error"
}

func (err ErrTimeout) Temporary() bool {
	return true
}

func (err ErrTimeout) Timeout() bool {
	return true
}

type DialFunc func(network, addr string) (net.Conn, error)

// Dialer is an http dialer that uses a capped connection pool to bound
// the number of parallel connections towards each address.
type Dialer struct {
	sync.Mutex
	conns map[netAndAddr]*pool
	// MaxConnPerHost is the maximum number of parallel connections dialed for
	// each host. If it is set to 0 we use DefaultMaxConnsPerHost.
	MaxConnPerHost int
	// Dialer is the underlying network dialer.
	Dialer net.Dialer
}

type netAndAddr struct {
	net  string
	addr string
}

func (d *Dialer) pool(network, addr string) *pool {
	d.Lock()
	defer d.Unlock()

	max := d.MaxConnPerHost
	if max == 0 {
		max = DefaultMaxConnsPerHost
	}

	if d.conns == nil {
		d.conns = make(map[netAndAddr]*pool)
	}

	p, ok := d.conns[netAndAddr{network, addr}]
	if !ok {
		p = &pool{
			connCh: make(chan *conn),
			tokens: max,
		}
		d.conns[netAndAddr{network, addr}] = p
	}

	return p
}

func (d *Dialer) Dial(network, addr string) (net.Conn, error) {
	pool := d.pool(network, addr)
	conn, err := pool.maybeDial(network, addr, d.Dialer.Dial)
	if err != nil {
		return nil, err
	}

	if err == nil && conn != nil {
		return conn, nil
	}

	toch := time.After(d.Dialer.Timeout)
	for {
		select {
		case conn := <-pool.connCh:
			return conn, nil

		case <-time.After(10 * time.Millisecond):
			conn, err := pool.maybeDial(network, addr, d.Dialer.Dial)
			if err != nil {
				return nil, err
			}

			if err == nil && conn != nil {
				return conn, nil
			}

		case <-toch:
			return nil, ErrTimeout{}
		}
	}
}

type pool struct {
	sync.Mutex

	connCh chan *conn // Used to wait for a new free connection.
	tokens int        // Cap minus the number of open connections.
}

func (p *pool) maybeDial(network, addr string, d DialFunc) (net.Conn,
	error) {

	if p.getToken() != 0 {
		c, err := d(network, addr)
		if err != nil {
			p.putToken()
			return c, err
		}

		pc := &conn{
			Conn: c,
			pool: p,
		}
		return pc, nil
	}

	return nil, nil
}

func (p *pool) getToken() int {
	p.Lock()
	defer p.Unlock()

	t := p.tokens
	if t > 0 {
		p.tokens--
	}
	return t
}

func (p *pool) putToken() int {
	p.Lock()
	defer p.Unlock()

	t := p.tokens
	p.tokens++
	return t
}

type conn struct {
	net.Conn
	pool *pool
}

func (c *conn) Close() error {
	select {
	case c.pool.connCh <- c:
		return nil
	default:
		c.pool.putToken()
		return c.Conn.Close()
	}
}

// NewHTTPClient creates an HTTP client, with the given timeout. Unlike
// the http package, this method does not allow more than maxConnPerHost
// connections towards each remote host.
func NewHTTPClient(maxConnPerHost int, timeout time.Duration) *http.Client {
	return newHTTPClient(maxConnPerHost, timeout)
}
