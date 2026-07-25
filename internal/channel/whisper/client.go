package whisper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/rs/zerolog/log"
)

type wsClient struct {
	baseURL string
	token   string

	conn   *websocket.Conn
	mu     sync.Mutex
	dialer websocket.Dialer

	readCh chan []byte

	reconnectCh chan struct{}
	cancel      context.CancelFunc
}

func newWSClient(baseURL, token string) *wsClient {
	return &wsClient{
		baseURL:     baseURL,
		token:       token,
		dialer:      websocket.Dialer{HandshakeTimeout: 10 * time.Second},
		readCh:      make(chan []byte, 256),
		reconnectCh: make(chan struct{}, 1),
	}
}

func (c *wsClient) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	defer cancel()

	for {
		if err := c.connect(ctx); err != nil {
			log.Error().Err(err).Msg("whisper: ws connect failed")
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				continue
			}
		}

		if err := c.auth(ctx); err != nil {
			c.conn.Close()
			log.Error().Err(err).Msg("whisper: ws auth failed")
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				continue
			}
		}

		log.Info().Msg("whisper: ws connected and authenticated")

		readDone := make(chan struct{})
		go c.readLoop(readDone)

		select {
		case <-ctx.Done():
			c.conn.Close()
			<-readDone
			return ctx.Err()
		case <-c.reconnectCh:
			c.conn.Close()
			<-readDone
			log.Info().Msg("whisper: reconnecting")
			continue
		}
	}
}

func (c *wsClient) connect(ctx context.Context) error {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return fmt.Errorf("parse base URL: %w", err)
	}

	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}
	u.Path = "/ws"

	header := http.Header{}
	header.Set("Origin", c.baseURL)

	conn, _, err := c.dialer.DialContext(ctx, u.String(), header)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	c.conn = conn
	return nil
}

func (c *wsClient) auth(ctx context.Context) error {
	msg := map[string]string{"type": "auth", "token": c.token}
	data, _ := json.Marshal(msg)

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

func (c *wsClient) JoinRoom(ctx context.Context, roomID string) error {
	return c.Send(ctx, map[string]string{"type": "join_room", "roomId": roomID})
}

func (c *wsClient) Send(ctx context.Context, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("not connected")
	}

	if err := c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

func (c *wsClient) readLoop(done chan struct{}) {
	defer close(done)

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			log.Warn().Err(err).Msg("whisper: ws read error")
			c.triggerReconnect()
			return
		}

		select {
		case c.readCh <- data:
		default:
			log.Warn().Msg("whisper: read channel full, dropping message")
		}
	}
}

func (c *wsClient) triggerReconnect() {
	select {
	case c.reconnectCh <- struct{}{}:
	default:
	}
}

func (c *wsClient) ReadCh() <-chan []byte {
	return c.readCh
}

func (c *wsClient) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}
