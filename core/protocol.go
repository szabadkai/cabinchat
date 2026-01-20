package core

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
)

// Message types
const (
	MsgTypeJoin      = "join"
	MsgTypeMsg       = "msg"
	MsgTypeSystem    = "system"
	MsgTypeLeave     = "leave"
	MsgTypeNick      = "nick"     // Nick change: Nick=old, Text=new
	MsgTypeUserList  = "userlist" // Text contains comma-separated users
	MsgTypePing      = "ping"
	MsgTypePong      = "pong"
	MsgTypeFileOffer = "fileoffer" // File offer: Nick=sender, Text=filename, Data=size
	MsgTypeFileAcc   = "fileacc"   // Accept: Nick=recipient, Text=sender (who to accept from)
	MsgTypeFileRej   = "filerej"   // Reject: Nick=recipient, Text=sender
	MsgTypeFile      = "file"      // Actual file data: Nick=sender, Text=filename, Data=base64
	MsgTypeWebRTC    = "webrtc"    // WebRTC signal: Nick=sender, Target=recipient, Data=JSON(Signal)
)

// Message represents a chat message
type Message struct {
	Type   string `json:"type"`
	Nick   string `json:"nick,omitempty"`
	Text   string `json:"text,omitempty"`
	Data   string `json:"data,omitempty"`   // Base64 file content
	Target string `json:"target,omitempty"` // Target nick for DMs/files
}

// SendMessage writes a JSON message followed by newline to connection
func SendMessage(conn net.Conn, msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(conn, "%s\n", data)
	return err
}

// ReadMessage reads a single JSON message from buffered reader
func ReadMessage(reader *bufio.Reader) (Message, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return Message{}, err
	}
	var msg Message
	err = json.Unmarshal([]byte(line), &msg)
	return msg, err
}
