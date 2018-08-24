package transit

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// buffSize 单次接收数据缓存大小
const buffSize = 1024

// Transit 将客户端请求发送到与之规则匹配的
// TCP长连接中并接收长连接返回的数据发送给客户端.
type Transit struct {
	// 匹配规则, 127.0.0.1 -> 127.0.0.1:8001,
	// 127.0.0.1;192.168.1.1 -> 127.0.0.1:8001
	matchRule map[string]string

	// idleConn map[string][]
	// 空闲连接通道
	idleConnCh map[string]chan *persistConn

	// 获取空闲连接锁
	idleMu sync.Mutex

	// 监听器
	listener net.Listener

	// 是否关闭连接
	closed bool
}

// persistConn TCP长连接
type persistConn struct {
	// TCP 连接
	conn net.Conn

	// 错误信息通道
	errch chan error

	// 开始写入数据通道
	writech chan struct{}

	// head data 头部信息,被提前读取出,用于获取被转发IP地址
	hb []byte

	// 开始读取数据通道
	readch chan struct{}

	// 完成通知通道
	donech chan struct{}

	// 关闭连接通知通道
	closech chan struct{}

	// 写入客户端Writer
	bw *bufio.Writer

	// 读取客户端Reader
	br *bufio.Reader
}

// NewTransit 返回结构
func NewTransit(matchRule map[string]string) *Transit {
	return &Transit{
		matchRule:  matchRule,
		idleConnCh: make(map[string]chan *persistConn),
	}
}

// Close 关闭连接
func (t *Transit) Close() error {
	if !t.closed {
		for _, ch := range t.idleConnCh {
			if ch != nil {
				pc := <-ch
				fmt.Println("开始关闭连接")
				pc.conn.Close()
			}
		}
		t.closed = true
		return t.listener.Close()
	}
	return nil
}

// ListenAndServe 监听端口并接受请求
func (t *Transit) ListenAndServe(addr string) error {
	if len(t.matchRule) <= 0 {
		return errors.New("matchRule is nil")
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	t.listener = ln
	defer t.Close()
	// defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if t.closed {
				return nil
			}
			fmt.Printf("accept error:%s\n", err)
			continue
		}

		if key := t.getPersistConnKey(conn); key != "" {
			go t.handlePersistConn(key, conn)
		} else {
			go t.handleClientConn(conn)
		}

	}
}

// handleClientConn 处理客户端连接
func (t *Transit) handleClientConn(conn net.Conn) {
	// fmt.Println(t.matchRule)
	// conn.SetDeadline(time.Now().Add(10 * time.Second))
	addr := conn.RemoteAddr()
	fmt.Printf("client conn:%s connect.\n", addr.String())
	// 关闭客户端连接
	defer func() {
		conn.Close()
		fmt.Printf("client conn:%s close.\n", addr.String())
	}()
	ip, hb := t.getForwardedIP(conn)
	// key := addr.(*net.TCPAddr).IP.String()
	// 获取与规则对应的长连接
	pcch := t.getIdleConnCh(ip)
	// pconn := &persistConn{}
	if pcch == nil {
		// 没有找到对应的规则或长连接未连接
		return
	}
	pconn := <-pcch

	pconn.br = bufio.NewReader(conn)
	pconn.bw = bufio.NewWriter(conn)
	pconn.hb = hb
	// 发送处理数据通知
	pconn.writech <- struct{}{}
	pconn.readch <- struct{}{}

	// 处理不同通道的消息
	select {
	case err := <-pconn.errch:
		fmt.Println(err)
	case <-pconn.donech:
		// 长连接完成处理,加入空闲连接通道
		t.putIdleConnCh(ip, pconn)
		fmt.Println("done.")
	case <-pconn.closech:
		// 长连接客户端关闭,移除长连接
		t.removePersistConn(ip, pconn)
		fmt.Println("close.")
	}

}

func (t *Transit) removePersistConn(ip string, pconn *persistConn) {
	fmt.Printf("Remove %s PersistConn.\n", ip)

	t.idleMu.Lock()
	defer t.idleMu.Unlock()

	key := t.getIdleConnKey(ip)
	if key == "" {
		fmt.Printf("Remove %s PersistConn fail.\n", ip)
		return
	}
	pconn.conn.Close()
	delete(t.idleConnCh, key)
	fmt.Printf("Remove %s PersistConn success.\n", ip)
}

func (t *Transit) putIdleConnCh(ip string, pconn *persistConn) {
	fmt.Printf("Put %s IdleConn.\n", ip)

	t.idleMu.Lock()
	defer t.idleMu.Unlock()

	key := t.getIdleConnKey(ip)
	if key == "" {
		fmt.Printf("Put %s IdleConn fail.\n", ip)
		return
	}
	t.idleConnCh[key] <- pconn
	fmt.Printf("Put %s IdleConn success.\n", ip)
}

// getIdleConnCh 获取空闲长连接
func (t *Transit) getIdleConnCh(ip string) chan *persistConn {
	fmt.Printf("Get %s IdleConn.\n", ip)

	t.idleMu.Lock()
	defer t.idleMu.Unlock()

	key := t.getIdleConnKey(ip)
	if key == "" {
		return nil
	}
	ch, ok := t.idleConnCh[key]
	if !ok {
		fmt.Printf("Get %s nil IdleConn success.\n", ip)
		return nil
	}
	fmt.Printf("Get %s IdleConn success.\n", ip)
	return ch
}

// handlePersistConn 处理长连接
func (t *Transit) handlePersistConn(key string, conn net.Conn) {
	// 初始化chan
	if _, ok := t.idleConnCh[key]; !ok {
		t.idleConnCh[key] = make(chan *persistConn, 1)
	}

	// 存放长连接
	addr := conn.RemoteAddr().String()
	fmt.Printf("persist conn:%s connect.\n", addr)
	pconn := &persistConn{
		conn:    conn,
		writech: make(chan struct{}, 1),
		readch:  make(chan struct{}, 1),
		donech:  make(chan struct{}, 1),
		closech: make(chan struct{}),
		errch:   make(chan error, 1),
	}
	t.idleConnCh[key] <- pconn
	// 等待读写操作
	go pconn.readLoop()
	go pconn.writeLoop()
	fmt.Printf("persist conn:%s connect success.\n", addr)
}

// readLoop 读取数据发送给长连接
func (pconn *persistConn) readLoop() {
	for {
		select {
		// 接收开始读取客户端数据通知
		case <-pconn.writech:
			// 读取客户端数据
			var b [buffSize]byte
			var n int
			var err error
			n = len(pconn.hb)
			if n > 0 {
				copy(b[:n], pconn.hb)
				pconn.hb = nil
			}
			for {
				if n > 0 {
					// 写入数据到长连接
					_, err = pconn.conn.Write(b[:n])
					if err != nil {
						if err == io.EOF {
							close(pconn.closech)
							return
						}
						pconn.errch <- err
						break
					}
				}
				if n < buffSize {
					break
				}
				n, err = pconn.br.Read(b[:])
				if err != nil {
					fmt.Println("read err", err)
					pconn.errch <- err
					break
				}
			}
		case <-pconn.closech:
			// 接收长连接关闭通知
			return
		}
	}
}

// writeLoop 读取长连接返回数据并写入数据给客户端
func (pconn *persistConn) writeLoop() {
	errch := make(chan error, 1)
	datach := make(chan []byte, 1)
	var b [buffSize]byte
	var n int
	var err error
	go func() {
		for {
			// 读取长连接返回的数据
			n, err = pconn.conn.Read(b[:])
			if err != nil {
				errch <- err
				return
			}
			data := make([]byte, n)
			copy(data[:n], b[:n])
			datach <- data
		}
	}()
	for {
		select {
		// 接收开始写入客户端数据通知
		case <-pconn.readch:
			for {
				select {
				case <-time.After(time.Second):
					fmt.Println("done")
					break
				case <-errch:
					fmt.Println(err)
					if err == io.EOF {
						close(pconn.closech)
						return
					}
					pconn.errch <- err
					break
				case data := <-datach:
					// 将读取的响应写入客户端连接
					fmt.Println(pconn.bw.Write(data))
					if err != nil {
						pconn.errch <- err
						break
					}
					fmt.Println()
					continue
				}
				break
			}
			if err = pconn.bw.Flush(); err != nil {
				// fmt.Println("err", err)
				pconn.errch <- err
				continue
			}
			// 发送写入完成通知
			pconn.donech <- struct{}{}
		case <-pconn.closech:
			// 接收长连接关闭通知
			return
		}
	}
}

// getIdleConnKey 根据IP获取空闲连接key
func (t *Transit) getIdleConnKey(ip string) (key string) {
	for saddr := range t.matchRule {
		saddrs := strings.Split(saddr, ";")
		for _, s := range saddrs {
			if s == ip {
				return saddr
			}
		}
	}
	return
}

// getPersistConnKey 根据匹配规则获取长连接的key
func (t *Transit) getPersistConnKey(conn net.Conn) (key string) {
	addr := conn.RemoteAddr().String()
	for saddr, daddr := range t.matchRule {
		if addr == daddr {
			return saddr
		}
	}
	return
}

// getForwardedIP 获取被转发的IP
func (t *Transit) getForwardedIP(conn net.Conn) (string, []byte) {
	var b [buffSize]byte
	n, err := conn.Read(b[:])
	if err != nil {
		return "", nil
	}

	hs := bytes.Split(b[:n], []byte("\n"))
	for _, l := range hs {
		if bytes.Contains(l, []byte("X-Forwarded-For")) {
			ls := bytes.Split(l, []byte(":"))
			if len(ls) > 1 {
				forwardedIP := string(bytes.Split(ls[1], []byte(";"))[0])
				return strings.TrimSpace(forwardedIP), b[:n]
			}
		}
	}

	return "", nil
}
