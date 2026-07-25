package whisper

import (
	"strings"

	"github.com/aspasskiy/gogogot/internal/transport"
)

type roomMsgPayload struct {
	Type      string          `json:"type"`
	RoomID    string          `json:"roomId"`
	ID        int64           `json:"id"`
	UserID    int64           `json:"userId"`
	Username  string          `json:"username"`
	FirstName string          `json:"firstName"`
	LastName  string          `json:"lastName"`
	Body      string          `json:"body"`
	Attachments []wsAttachment `json:"attachments"`
}

type dmPayload struct {
	Type      string          `json:"type"`
	ID        int64           `json:"id"`
	SenderID  int64           `json:"senderId"`
	ReceiverID int64          `json:"receiverId"`
	SenderUsername  string    `json:"senderUsername"`
	SenderFirstName string    `json:"senderFirstName"`
	SenderLastName  string    `json:"senderLastName"`
	Body      string          `json:"body"`
	Attachments []wsAttachment `json:"attachments"`
}

type wsAttachment struct {
	ID               int64  `json:"id"`
	Filename         string `json:"filename"`
	OriginalFilename string `json:"originalFilename"`
	MimeType         string `json:"mimeType"`
	Size             int64  `json:"size"`
}

type outgoingMessage struct {
	Type          string  `json:"type"`
	Body          string  `json:"body"`
	RoomID        string  `json:"roomId,omitempty"`
	ReceiverID    int64   `json:"receiverId,omitempty"`
	MessageID     int64   `json:"messageId,omitempty"`
	AttachmentIDs []int64 `json:"attachmentIds,omitempty"`
}

type authMessage struct {
	Type  string `json:"type"`
	Token string `json:"token"`
}

func convertAttachments(atts []wsAttachment) []transport.Attachment {
	if len(atts) == 0 {
		return nil
	}

	var result []transport.Attachment
	for _, a := range atts {
		result = append(result, transport.Attachment{
			Filename: a.OriginalFilename,
			MimeType: a.MimeType,
		})
	}
	return result
}

func normalizeRoomID(raw string) string {
	return strings.TrimSpace(raw)
}
