package network

import (
	"context"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type ListenerStats struct {
	TotalBytesReceived uint64
	TotalPackets       uint64
	ActiveConnections  int32
	TotalConnections   uint64
	CloseCalled        uint64
	PanicRecovered     uint64
}

type ZeroCopyUDPListener struct {
	addr         string
	conn         *net.UDPConn
	connMu       sync.Mutex
	bufPool      *sync.Pool
	callback     func([]byte)
	closed       atomic.Bool
	closeOnce    sync.Once
	stats        ListenerStats
	lastListenAt atomic.Int64
	ctx          context.Context
	cancel       context.CancelFunc
}

func NewZeroCopyUDPListener(addr string) *ZeroCopyUDPListener {
	ctx, cancel := context.WithCancel(context.Background())
	return &ZeroCopyUDPListener{
		addr:   addr,
		ctx:    ctx,
		cancel: cancel,
		bufPool: &sync.Pool{
			New: func() interface{} {
				b := make([]byte, 65536)
				return &b
			},
		},
	}
}

func (l *ZeroCopyUDPListener) Listen(callback func([]byte)) {
	defer func() {
		if r := recover(); r != nil {
			atomic.AddUint64(&l.stats.PanicRecovered, 1)
			log.Printf("PANIC RECOVERED in UDP Listen: %v", r)
			time.Sleep(1 * time.Second)
			if !l.closed.Load() {
				go l.Listen(callback)
			}
		}
	}()

	now := time.Now().UnixNano()
	lastListen := l.lastListenAt.Load()
	if now-lastListen < int64(100*time.Millisecond) {
		log.Printf("WARNING: UDP listener restart too frequent, throttling...")
		time.Sleep(200 * time.Millisecond)
	}
	l.lastListenAt.Store(now)

	if l.closed.Load() {
		log.Printf("UDP listener already closed, aborting Listen")
		return
	}

	l.callback = callback

	udpAddr, err := net.ResolveUDPAddr("udp", l.addr)
	if err != nil {
		log.Printf("UDP Resolve error: %v", err)
		if !l.closed.Load() {
			time.Sleep(1 * time.Second)
			go l.Listen(callback)
		}
		return
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		log.Printf("UDP Listen error: %v", err)
		if !l.closed.Load() {
			time.Sleep(1 * time.Second)
			go l.Listen(callback)
		}
		return
	}

	l.connMu.Lock()
	if l.conn != nil {
		log.Printf("WARNING: Closing stale UDP connection before assigning new one")
		l.conn.Close()
	}
	l.conn = conn
	l.connMu.Unlock()

	rawConn, err := conn.SyscallConn()
	if err == nil {
		rawConn.Control(func(fd uintptr) {
			_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, 16*1024*1024)
			_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
		})
	}

	log.Printf("Zero-copy UDP listener started on %s", l.addr)

	go func() {
		<-l.ctx.Done()
		l.connMu.Lock()
		c := l.conn
		l.connMu.Unlock()
		if c != nil {
			c.Close()
		}
	}()

	for {
		if l.closed.Load() || l.ctx.Err() != nil {
			return
		}

		bufPtr := l.bufPool.Get().(*[]byte)
		buf := *bufPtr

		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(buf)

		if err != nil {
			l.bufPool.Put(bufPtr)

			if l.closed.Load() || l.ctx.Err() != nil {
				return
			}

			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}

			log.Printf("UDP Read error: %v", err)

			if isRecoverableNetError(err) && !l.closed.Load() {
				log.Printf("Attempting UDP listener recovery...")
				l.connMu.Lock()
				if l.conn == conn {
					l.conn = nil
				}
				l.connMu.Unlock()
				conn.Close()
				time.Sleep(500 * time.Millisecond)
				go l.Listen(callback)
				return
			}

			continue
		}

		if n > 0 {
			atomic.AddUint64(&l.stats.TotalPackets, 1)
			atomic.AddUint64(&l.stats.TotalBytesReceived, uint64(n))

			data := make([]byte, n)
			copy(data, buf[:n])
			l.bufPool.Put(bufPtr)

			func() {
				defer func() {
					if r := recover(); r != nil {
						atomic.AddUint64(&l.stats.PanicRecovered, 1)
						log.Printf("PANIC RECOVERED in UDP callback: %v", r)
					}
				}()
				l.callback(data)
			}()
		} else {
			l.bufPool.Put(bufPtr)
		}
	}
}

func (l *ZeroCopyUDPListener) Close() {
	l.closeOnce.Do(func() {
		atomic.AddUint64(&l.stats.CloseCalled, 1)
		log.Printf("Closing UDP listener on %s", l.addr)

		l.closed.Store(true)
		l.cancel()

		l.connMu.Lock()
		c := l.conn
		l.conn = nil
		l.connMu.Unlock()

		if c != nil {
			if err := c.Close(); err != nil {
				log.Printf("Error closing UDP connection: %v", err)
			}
		}

		log.Printf("UDP listener on %s closed successfully", l.addr)
	})
}

func (l *ZeroCopyUDPListener) GetStats() ListenerStats {
	return ListenerStats{
		TotalBytesReceived: atomic.LoadUint64(&l.stats.TotalBytesReceived),
		TotalPackets:       atomic.LoadUint64(&l.stats.TotalPackets),
		ActiveConnections:  atomic.LoadInt32(&l.stats.ActiveConnections),
		TotalConnections:   atomic.LoadUint64(&l.stats.TotalConnections),
		CloseCalled:        atomic.LoadUint64(&l.stats.CloseCalled),
		PanicRecovered:     atomic.LoadUint64(&l.stats.PanicRecovered),
	}
}

type ZeroCopyTCPListener struct {
	addr             string
	listener         *net.TCPListener
	callback         func([]byte)
	closed           atomic.Bool
	closeOnce        sync.Once
	connMu           sync.Mutex
	activeConns      map[*net.TCPConn]struct{}
	stats            ListenerStats
	lastListenAt     atomic.Int64
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
}

func NewZeroCopyTCPListener(addr string) *ZeroCopyTCPListener {
	ctx, cancel := context.WithCancel(context.Background())
	return &ZeroCopyTCPListener{
		addr:        addr,
		ctx:         ctx,
		cancel:      cancel,
		activeConns: make(map[*net.TCPConn]struct{}),
	}
}

func (l *ZeroCopyTCPListener) Listen(callback func([]byte)) {
	defer func() {
		if r := recover(); r != nil {
			atomic.AddUint64(&l.stats.PanicRecovered, 1)
			log.Printf("PANIC RECOVERED in TCP Listen: %v", r)
			time.Sleep(1 * time.Second)
			if !l.closed.Load() {
				go l.Listen(callback)
			}
		}
	}()

	now := time.Now().UnixNano()
	lastListen := l.lastListenAt.Load()
	if now-lastListen < int64(100*time.Millisecond) {
		log.Printf("WARNING: TCP listener restart too frequent, throttling...")
		time.Sleep(200 * time.Millisecond)
	}
	l.lastListenAt.Store(now)

	if l.closed.Load() {
		log.Printf("TCP listener already closed, aborting Listen")
		return
	}

	l.callback = callback

	tcpAddr, err := net.ResolveTCPAddr("tcp", l.addr)
	if err != nil {
		log.Printf("TCP Resolve error: %v", err)
		if !l.closed.Load() {
			time.Sleep(1 * time.Second)
			go l.Listen(callback)
		}
		return
	}

	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		log.Printf("TCP Listen error: %v", err)
		if !l.closed.Load() {
			time.Sleep(1 * time.Second)
			go l.Listen(callback)
		}
		return
	}

	l.connMu.Lock()
	if l.listener != nil {
		log.Printf("WARNING: Closing stale TCP listener before assigning new one")
		l.listener.Close()
	}
	l.listener = listener
	l.connMu.Unlock()

	log.Printf("Zero-copy TCP listener started on %s", l.addr)

	go func() {
		<-l.ctx.Done()
		l.connMu.Lock()
		lis := l.listener
		l.connMu.Unlock()
		if lis != nil {
			lis.Close()
		}
	}()

	for {
		if l.closed.Load() || l.ctx.Err() != nil {
			return
		}

		_ = listener.SetDeadline(time.Now().Add(500 * time.Millisecond))
		conn, err := listener.AcceptTCP()

		if err != nil {
			if l.closed.Load() || l.ctx.Err() != nil {
				return
			}

			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}

			log.Printf("TCP Accept error: %v", err)

			if isRecoverableNetError(err) && !l.closed.Load() {
				log.Printf("Attempting TCP listener recovery...")
				l.connMu.Lock()
				if l.listener == listener {
					l.listener = nil
				}
				l.connMu.Unlock()
				listener.Close()
				time.Sleep(500 * time.Millisecond)
				go l.Listen(callback)
				return
			}

			continue
		}

		atomic.AddInt32(&l.stats.ActiveConnections, 1)
		atomic.AddUint64(&l.stats.TotalConnections, 1)

		l.connMu.Lock()
		l.activeConns[conn] = struct{}{}
		l.connMu.Unlock()

		l.wg.Add(1)
		go func(c *net.TCPConn) {
			defer func() {
				if r := recover(); r != nil {
					atomic.AddUint64(&l.stats.PanicRecovered, 1)
					log.Printf("PANIC RECOVERED in TCP handler: %v", r)
				}
			}()
			l.handleConnection(c)
		}(conn)
	}
}

func (l *ZeroCopyTCPListener) handleConnection(conn *net.TCPConn) {
	defer l.wg.Done()
	defer func() {
		atomic.AddInt32(&l.stats.ActiveConnections, -1)
		l.connMu.Lock()
		delete(l.activeConns, conn)
		l.connMu.Unlock()
		conn.Close()
	}()

	rawConn, err := conn.SyscallConn()
	if err == nil {
		rawConn.Control(func(fd uintptr) {
			_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)
			_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, 8*1024*1024)
			_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_KEEPALIVE, 1)
		})
	}

	buf := make([]byte, 65536)
	for {
		if l.closed.Load() || l.ctx.Err() != nil {
			return
		}

		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, err := conn.Read(buf)

		if err != nil {
			if l.closed.Load() || l.ctx.Err() != nil {
				return
			}

			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}

			return
		}

		if n > 0 {
			atomic.AddUint64(&l.stats.TotalPackets, 1)
			atomic.AddUint64(&l.stats.TotalBytesReceived, uint64(n))

			data := make([]byte, n)
			copy(data, buf[:n])

			func() {
				defer func() {
					if r := recover(); r != nil {
						atomic.AddUint64(&l.stats.PanicRecovered, 1)
						log.Printf("PANIC RECOVERED in TCP callback: %v", r)
					}
				}()
				l.callback(data)
			}()
		}
	}
}

func (l *ZeroCopyTCPListener) Close() {
	l.closeOnce.Do(func() {
		atomic.AddUint64(&l.stats.CloseCalled, 1)
		log.Printf("Closing TCP listener on %s", l.addr)

		l.closed.Store(true)
		l.cancel()

		l.connMu.Lock()
		lis := l.listener
		l.listener = nil

		conns := make([]*net.TCPConn, 0, len(l.activeConns))
		for c := range l.activeConns {
			conns = append(conns, c)
		}
		l.activeConns = make(map[*net.TCPConn]struct{})
		l.connMu.Unlock()

		if lis != nil {
			if err := lis.Close(); err != nil {
				log.Printf("Error closing TCP listener: %v", err)
			}
		}

		for _, c := range conns {
			if err := c.Close(); err != nil {
				log.Printf("Error closing TCP connection: %v", err)
			}
		}

		done := make(chan struct{})
		go func() {
			l.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			log.Printf("All TCP connections closed gracefully")
		case <-time.After(5 * time.Second):
			log.Printf("Timeout waiting for TCP connections to close, force closing")
		}

		log.Printf("TCP listener on %s closed successfully", l.addr)
	})
}

func (l *ZeroCopyTCPListener) GetStats() ListenerStats {
	return ListenerStats{
		TotalBytesReceived: atomic.LoadUint64(&l.stats.TotalBytesReceived),
		TotalPackets:       atomic.LoadUint64(&l.stats.TotalPackets),
		ActiveConnections:  atomic.LoadInt32(&l.stats.ActiveConnections),
		TotalConnections:   atomic.LoadUint64(&l.stats.TotalConnections),
		CloseCalled:        atomic.LoadUint64(&l.stats.CloseCalled),
		PanicRecovered:     atomic.LoadUint64(&l.stats.PanicRecovered),
	}
}

func isRecoverableNetError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	recoverableErrors := []string{
		"use of closed network connection",
		"connection reset by peer",
		"broken pipe",
		"connection aborted",
		"i/o timeout",
	}
	for _, r := range recoverableErrors {
		if containsIgnoreCase(errStr, r) {
			return true
		}
	}
	return false
}

func containsIgnoreCase(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	toLower := func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return r
	}
	sLower := make([]rune, 0, len(s))
	for _, r := range s {
		sLower = append(sLower, toLower(r))
	}
	subLower := make([]rune, 0, len(substr))
	for _, r := range substr {
		subLower = append(subLower, toLower(r))
	}
	for i := 0; i <= len(sLower)-len(subLower); i++ {
		match := true
		for j := 0; j < len(subLower); j++ {
			if sLower[i+j] != subLower[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
