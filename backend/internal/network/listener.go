package network

import (
	"log"
	"net"
	"sync"
	"syscall"
)

type ZeroCopyUDPListener struct {
	addr     string
	conn     *net.UDPConn
	bufPool  *sync.Pool
	callback func([]byte)
	closed   bool
	mu       sync.RWMutex
}

func NewZeroCopyUDPListener(addr string) *ZeroCopyUDPListener {
	return &ZeroCopyUDPListener{
		addr: addr,
		bufPool: &sync.Pool{
			New: func() interface{} {
				b := make([]byte, 65536)
				return &b
			},
		},
	}
}

func (l *ZeroCopyUDPListener) Listen(callback func([]byte)) {
	l.callback = callback

	udpAddr, err := net.ResolveUDPAddr("udp", l.addr)
	if err != nil {
		log.Printf("UDP Resolve error: %v", err)
		return
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		log.Printf("UDP Listen error: %v", err)
		return
	}

	l.mu.Lock()
	l.conn = conn
	l.mu.Unlock()

	rawConn, err := conn.SyscallConn()
	if err == nil {
		rawConn.Control(func(fd uintptr) {
			_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, 16*1024*1024)
		})
	}

	log.Printf("Zero-copy UDP listener started on %s", l.addr)

	for {
		l.mu.RLock()
		if l.closed {
			l.mu.RUnlock()
			return
		}
		l.mu.RUnlock()

		bufPtr := l.bufPool.Get().(*[]byte)
		buf := *bufPtr

		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			l.bufPool.Put(bufPtr)
			l.mu.RLock()
			if !l.closed {
				log.Printf("UDP Read error: %v", err)
			}
			l.mu.RUnlock()
			continue
		}

		data := make([]byte, n)
		copy(data, buf[:n])
		l.bufPool.Put(bufPtr)

		l.callback(data)
	}
}

func (l *ZeroCopyUDPListener) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	if l.conn != nil {
		l.conn.Close()
	}
}

type ZeroCopyTCPListener struct {
	addr     string
	listener *net.TCPListener
	callback func([]byte)
	closed   bool
	mu       sync.RWMutex
	wg       sync.WaitGroup
}

func NewZeroCopyTCPListener(addr string) *ZeroCopyTCPListener {
	return &ZeroCopyTCPListener{
		addr: addr,
	}
}

func (l *ZeroCopyTCPListener) Listen(callback func([]byte)) {
	l.callback = callback

	tcpAddr, err := net.ResolveTCPAddr("tcp", l.addr)
	if err != nil {
		log.Printf("TCP Resolve error: %v", err)
		return
	}

	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		log.Printf("TCP Listen error: %v", err)
		return
	}

	l.mu.Lock()
	l.listener = listener
	l.mu.Unlock()

	log.Printf("Zero-copy TCP listener started on %s", l.addr)

	for {
		l.mu.RLock()
		if l.closed {
			l.mu.RUnlock()
			return
		}
		l.mu.RUnlock()

		conn, err := listener.AcceptTCP()
		if err != nil {
			l.mu.RLock()
			if !l.closed {
				log.Printf("TCP Accept error: %v", err)
			}
			l.mu.RUnlock()
			continue
		}

		l.wg.Add(1)
		go l.handleConnection(conn)
	}
}

func (l *ZeroCopyTCPListener) handleConnection(conn *net.TCPConn) {
	defer l.wg.Done()
	defer conn.Close()

	rawConn, err := conn.SyscallConn()
	if err == nil {
		rawConn.Control(func(fd uintptr) {
			_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)
			_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, 8*1024*1024)
		})
	}

	buf := make([]byte, 65536)
	for {
		l.mu.RLock()
		if l.closed {
			l.mu.RUnlock()
			return
		}
		l.mu.RUnlock()

		n, err := conn.Read(buf)
		if err != nil {
			return
		}

		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			l.callback(data)
		}
	}
}

func (l *ZeroCopyTCPListener) Close() {
	l.mu.Lock()
	l.closed = true
	if l.listener != nil {
		l.listener.Close()
	}
	l.mu.Unlock()
	l.wg.Wait()
}
