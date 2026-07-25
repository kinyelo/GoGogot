package whisper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"github.com/aspasskiy/gogogot/internal/transport"
)

type replier struct {
	ch       *Channel
	roomID   string
	dmUserID int64
}

func (r *replier) SendText(ctx context.Context, text string) error {
	if r.roomID != "" {
		if err := r.ch.client.JoinRoom(ctx, r.roomID); err != nil {
			return err
		}
		msg := outgoingMessage{
			Type: "room_message",
			Body: text,
		}
		return r.ch.client.Send(ctx, msg)
	}

	if r.dmUserID != 0 {
		msg := outgoingMessage{
			Type:       "dm",
			Body:       text,
			ReceiverID: r.dmUserID,
		}
		return r.ch.client.Send(ctx, msg)
	}

	return nil
}

func (r *replier) SendFile(ctx context.Context, path, caption string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	attID, mimeType, err := r.uploadFile(ctx, filepath.Base(path), data)
	if err != nil {
		return fmt.Errorf("upload file: %w", err)
	}

	if r.roomID != "" {
		return r.sendWithAttachment(ctx, "room_message", r.roomID, 0, caption, attID)
	}
	if r.dmUserID != 0 {
		return r.sendWithAttachment(ctx, "dm", "", r.dmUserID, caption, attID)
	}

	_ = r.SendText(ctx, caption+"\n\n[File: "+filepath.Base(path)+"] ("+mimeType+")")
	return nil
}

func (r *replier) sendWithAttachment(ctx context.Context, msgType, roomID string, receiverID int64, caption string, attachmentID int64) error {
	if roomID != "" {
		if err := r.ch.client.JoinRoom(ctx, roomID); err != nil {
			return err
		}
	}
	msg := outgoingMessage{
		Type:          msgType,
		Body:          caption,
		AttachmentIDs: []int64{attachmentID},
	}
	if receiverID != 0 {
		msg.ReceiverID = receiverID
	}
	return r.ch.client.Send(ctx, msg)
}

func (r *replier) uploadFile(ctx context.Context, filename string, data []byte) (int64, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		return 0, "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(fw, bytes.NewReader(data)); err != nil {
		return 0, "", fmt.Errorf("copy file: %w", err)
	}
	if err := w.Close(); err != nil {
		return 0, "", fmt.Errorf("close multipart: %w", err)
	}

	u := r.ch.baseURL + "/api/v1/attachments"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, &buf)
	if err != nil {
		return 0, "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+r.ch.token)
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return 0, "", fmt.Errorf("upload failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		ID       int64  `json:"id"`
		MimeType string `json:"mimeType"`
		Filename string `json:"filename"`
	}
	if err := decodeJSON(resp.Body, &result); err != nil {
		return 0, "", fmt.Errorf("decode response: %w", err)
	}

	return result.ID, result.MimeType, nil
}

func (r *replier) SendTyping(ctx context.Context) error {
	if r.roomID != "" {
		return r.ch.client.Send(ctx, outgoingMessage{
			Type:   "typing",
			RoomID: r.roomID,
		})
	}
	if r.dmUserID != 0 {
		return r.ch.client.Send(ctx, outgoingMessage{
			Type:       "typing",
			ReceiverID: r.dmUserID,
		})
	}
	return nil
}

func (r *replier) SendAsk(ctx context.Context, prompt string, kind transport.AskKind, options []transport.AskOption) error {
	text := "❓ " + prompt
	if kind == transport.AskConfirm {
		text += "\n\n(Reply with yes/no or your answer)"
	} else if kind == transport.AskChoice && len(options) > 0 {
		text += "\n\nOptions:\n"
		for _, opt := range options {
			text += "- " + opt.Label + "\n"
		}
	}
	return r.SendText(ctx, text)
}

func (r *replier) ConsumeEvents(ctx context.Context, events <-chan transport.Event, replyInbox <-chan string) string {
	_ = r.SendTyping(ctx)

	var (
		finalText string
	)

	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return finalText
			}

			switch e := ev.(type) {
			case transport.ToolStartEvent:
				_ = r.SendTyping(ctx)

			case transport.ProgressEvent:
				_ = r.SendTyping(ctx)

			case transport.MidMessageEvent:
				_ = r.SendText(ctx, e.Text)

			case transport.AskEvent:
				_ = r.SendAsk(ctx, e.Prompt, e.Kind, e.Options)

				if replyInbox != nil {
					select {
					case resp := <-replyInbox:
						if e.ReplyCh != nil {
							e.ReplyCh <- resp
						}
					case <-ctx.Done():
						if e.ReplyCh != nil {
							close(e.ReplyCh)
						}
						return finalText
					}
				} else {
					if e.ReplyCh != nil {
						e.ReplyCh <- "(no interactive input available)"
					}
				}

			case transport.ErrorEvent:
				if ctx.Err() != nil {
					return finalText
				}
				_ = r.SendText(ctx, "Error: "+e.Error)
				return finalText

			case transport.DoneEvent:
				if ctx.Err() != nil {
					return finalText
				}
				if e.Text != "" {
					finalText = e.Text
				}
				return finalText
			}

		case <-ctx.Done():
			return finalText
		}
	}
}

func decodeJSON(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}
