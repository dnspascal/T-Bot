package api

import (
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"
)

const (
	demoHost = "demo.ctraderapi.com:5035"
	liveHost = "live.ctraderapi.com:5035"

	heartbeatInterval = 10 * time.Second
	readTimeout       = 30 * time.Second
)

type MessageHandler func(payloadType uint32, payload []byte)

type Connection struct {
	host      string
	conn      net.Conn
	mu        sync.Mutex
	handler   MessageHandler
	done      chan struct{}
	dead      chan struct{}
	closeOnce sync.Once
}

func NewConnection(demo bool, handler MessageHandler) *Connection {
	host := liveHost
	if demo {
		host = demoHost
	}
	return &Connection{
		host:    host,
		handler: handler,
		done:    make(chan struct{}),
		dead:    make(chan struct{}),
	}
}

func (c *Connection) Connect() error {
	c.Close()
	c.done = make(chan struct{})
	c.dead = make(chan struct{})
	c.closeOnce = sync.Once{}

	conn, err := tls.Dial("tcp", c.host, &tls.Config{})
	if err != nil {
		return fmt.Errorf("dial %s: %w", c.host, err)
	}
	c.conn = conn
	go c.readLoop()
	go c.heartbeatLoop()
	return nil
}

func (c *Connection) SendRaw(payloadType uint32, inner []byte) error {
	envelope := encodeEnvelope(payloadType, inner)
	frame := make([]byte, 4+len(envelope))
	binary.BigEndian.PutUint32(frame[0:4], uint32(len(envelope)))
	copy(frame[4:], envelope)

	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.conn.Write(frame)
	return err
}

func (c *Connection) readLoop() {
	dead := c.dead
	defer close(dead)

	for {
		select {
		case <-c.done:
			return
		default:
		}

		c.conn.SetReadDeadline(time.Now().Add(readTimeout))

		var msgLen uint32
		if err := binary.Read(c.conn, binary.BigEndian, &msgLen); err != nil {
			if err != io.EOF {
				slog.Error("read error", "err", err)
			}
			return
		}

		buf := make([]byte, msgLen)
		if _, err := io.ReadFull(c.conn, buf); err != nil {
			slog.Error("read body error", "err", err)
			return
		}

		pType := payloadTypeOf(buf)
		payload := payloadOf(buf)
		c.handler(pType, payload)
	}
}

func (c *Connection) heartbeatLoop() {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.SendRaw(51, nil)
		}
	}
}

func (c *Connection) Close() {
	c.closeOnce.Do(func() { close(c.done) })
	if c.conn != nil {
		c.conn.Close()
	}
}
