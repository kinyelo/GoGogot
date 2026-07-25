package whisper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/aspasskiy/gogogot/internal/channel"
	"github.com/aspasskiy/gogogot/internal/transport"

	"github.com/rs/zerolog/log"
)

type Channel struct {
	baseURL  string
	username string
	password string
	token    string
	selfID   int64 // bot's own user ID from login
	ownerID  int64 // 0 = respond to all users

	client  *wsClient
	handler channel.Handler
}

func New(baseURL, username, password string, ownerID int64) (*Channel, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("WHISPER_BASE_URL is required")
	}
	if username == "" {
		return nil, fmt.Errorf("WHISPER_USERNAME is required")
	}
	if password == "" {
		return nil, fmt.Errorf("WHISPER_PASSWORD is required")
	}

	ch := &Channel{
		baseURL:  baseURL,
		username: username,
		password: password,
		ownerID:  ownerID,
	}

	if err := ch.login(); err != nil {
		return nil, fmt.Errorf("whisper login: %w", err)
	}

	return ch, nil
}

type loginResponse struct {
	UserID     int64  `json:"userId"`
	Token      string `json:"token"`
	Requires2FA bool  `json:"requires2fa,omitempty"`
}

func (c *Channel) login() error {
	body := map[string]string{
		"identifier": c.username,
		"password":   c.password,
	}
	data, _ := json.Marshal(body)

	u := c.baseURL + "/api/v1/auth/login"
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("login failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if result.Requires2FA {
		return fmt.Errorf("login requires 2FA — bot accounts must have 2FA disabled")
	}

	if result.Token == "" {
		return fmt.Errorf("login returned empty token")
	}

	if result.UserID == 0 {
		return fmt.Errorf("login returned empty user ID")
	}

	c.token = result.Token
	c.selfID = result.UserID
	return nil
}

func (c *Channel) Name() string { return "whisper" }

func (c *Channel) OwnerReplier() transport.Replier {
	ownerID := c.ownerID
	if ownerID == 0 {
		ownerID = c.selfID
	}
	return &replier{ch: c, dmUserID: ownerID}
}

func (c *Channel) Run(ctx context.Context, handler channel.Handler) error {
	c.handler = handler

	client := newWSClient(c.baseURL, c.token)
	c.client = client

	go func() {
		if err := client.Run(ctx); err != nil && ctx.Err() == nil {
			log.Error().Err(err).Msg("whisper: ws client stopped")
		}
	}()

	log.Info().Int64("self_id", c.selfID).Int64("owner_id", c.ownerID).Msg("whisper channel started")
	c.readLoop(ctx)
	return ctx.Err()
}

func (c *Channel) readLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-c.client.ReadCh():
			c.handleRawMessage(ctx, data)
		}
	}
}

func (c *Channel) handleRawMessage(ctx context.Context, data []byte) {
	var env struct {
		Type string `json:"type"`
	}

	if err := json.Unmarshal(data, &env); err != nil {
		log.Warn().Err(err).Msg("whisper: failed to parse message envelope")
		return
	}

	switch env.Type {
	case "connected":
		log.Info().Msg("whisper: received connected event")

	case "room_message":
		var msg roomMsgPayload
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Warn().Err(err).Msg("whisper: failed to parse room_message")
			return
		}
		c.handleRoomMessage(ctx, msg)

	case "dm":
		var msg dmPayload
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Warn().Err(err).Msg("whisper: failed to parse dm")
			return
		}
		c.handleDM(ctx, msg)

	case "typing", "presence", "reaction_added", "reaction_removed",
		"room_created", "room_updated", "room_deleted",
		"member_removed", "member_left", "room_invite",
		"message_updated", "message_deleted",
		"dm_updated", "dm_deleted",
		"room_read", "dm_read_receipt",
		"chat_cleaned", "dm_cleaned",
		"user_messages_deleted", "user_dm_messages_deleted",
		"user_updated", "user_status",
		"rate_limit", "error":
		return

	default:
		log.Trace().Str("type", env.Type).Msg("whisper: unhandled message type")
	}
}

func (c *Channel) handleRoomMessage(ctx context.Context, msg roomMsgPayload) {
	if c.ownerID != 0 && msg.UserID != c.ownerID {
		return
	}

	text := strings.TrimSpace(msg.Body)
	if text == "" {
		return
	}

	c.handler(ctx, channel.Message{
		Text:        text,
		Attachments: convertAttachments(msg.Attachments),
		Reply:       &replier{ch: c, roomID: msg.RoomID},
	})
}

func (c *Channel) handleDM(ctx context.Context, msg dmPayload) {
	if c.ownerID != 0 && msg.SenderID != c.ownerID {
		return
	}

	text := strings.TrimSpace(msg.Body)
	if text == "" && len(msg.Attachments) == 0 {
		return
	}

	c.handler(ctx, channel.Message{
		Text:        text,
		Attachments: convertAttachments(msg.Attachments),
		Reply:       &replier{ch: c, dmUserID: msg.SenderID},
	})
}
