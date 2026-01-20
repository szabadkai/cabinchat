package core

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"

	"cabinchat/media"
)

// PendingFile represents a file offer waiting for acceptance
type PendingFile struct {
	From     string
	Filename string
	Size     string
}

// ClientCallbacks defines events for the UI to handle
type ClientCallbacks struct {
	OnMessageReceived func(msg Message)
	OnSystemMessage   func(text string)
	OnUserList        func(users []string)
	OnFileOffer       func(offer PendingFile)
	OnFileAccepted    func(sender string)
	OnFileRejected    func(sender string)
	OnFileReceived    func(filename string, data string, sender string)
	OnConnectionLost  func()
}

// ChatClient represents a chat client connection
type ChatClient struct {
	conn            net.Conn
	nick            string
	reader          *bufio.Reader
	pingStart       time.Time
	pendingFile     *PendingFile // incoming offer
	lastOfferedFile string       // path of file we offered
	lastOfferedTo   string       // who we offered to
	mediaManager    *media.MediaManager
	callbacks       ClientCallbacks
}

// NewChatClient creates a new client and connects to the host
func NewChatClient(host string, port int, nick string, app fyne.App, callbacks ClientCallbacks) (*ChatClient, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	client := &ChatClient{
		conn:      conn,
		nick:      nick,
		reader:    bufio.NewReader(conn),
		callbacks: callbacks,
	}

	// Initialize Media Manager
	client.mediaManager = media.NewMediaManager(app, func(target string, data string) {
		msg := Message{
			Type:   MsgTypeWebRTC,
			Nick:   nick,
			Text:   "signal",
			Data:   data,
			Target: target,
		}
		SendMessage(conn, msg)
	})

	// Send join message
	err = SendMessage(conn, Message{Type: MsgTypeJoin, Nick: nick})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to join: %w", err)
	}

	return client, nil
}

// Start begins the chat client listener
func (c *ChatClient) Start() {
	// Start receive loop in background
	go c.receiveLoop()
}

// receiveLoop reads messages from the server
func (c *ChatClient) receiveLoop() {
	for {
		msg, err := ReadMessage(c.reader)
		if err != nil {
			if c.callbacks.OnConnectionLost != nil {
				c.callbacks.OnConnectionLost()
			}
			return
		}

		switch msg.Type {
		case MsgTypeMsg:
			if c.callbacks.OnMessageReceived != nil {
				c.callbacks.OnMessageReceived(msg)
			}
		case MsgTypeSystem:
			if c.callbacks.OnSystemMessage != nil {
				c.callbacks.OnSystemMessage(msg.Text)
			}
		case MsgTypePong:
			// Just log locally or update UI status if we had one for ping
			elapsed := time.Since(c.pingStart)
			if c.callbacks.OnSystemMessage != nil {
				c.callbacks.OnSystemMessage(fmt.Sprintf("Pong! %dms", elapsed.Milliseconds()))
			}
		case MsgTypeUserList:
			if c.callbacks.OnUserList != nil {
				users := strings.Split(msg.Text, ", ")
				c.callbacks.OnUserList(users)
			}
		case MsgTypeFileOffer:
			c.pendingFile = &PendingFile{From: msg.Nick, Filename: msg.Text, Size: msg.Data}
			if c.callbacks.OnFileOffer != nil {
				c.callbacks.OnFileOffer(*c.pendingFile)
			}
		case MsgTypeFileAcc:
			if c.lastOfferedFile != "" {
				c.sendActualFile(c.lastOfferedFile, msg.Nick)
				c.lastOfferedFile = ""
				c.lastOfferedTo = ""
				if c.callbacks.OnFileAccepted != nil {
					c.callbacks.OnFileAccepted(msg.Nick)
				}
			}
		case MsgTypeFileRej:
			c.lastOfferedFile = ""
			c.lastOfferedTo = ""
			if c.callbacks.OnFileRejected != nil {
				c.callbacks.OnFileRejected(msg.Nick)
			}
		case MsgTypeFile:
			// Actual file data received
			// For now, auto-save to current dir, but UI notification is important
			saveFile(msg.Text, msg.Data, msg.Nick)
			if c.callbacks.OnFileReceived != nil {
				c.callbacks.OnFileReceived(msg.Text, msg.Data, msg.Nick)
			}
		case MsgTypeWebRTC:
			c.mediaManager.HandleSignal(msg.Nick, msg.Data)
		}
	}
}

// SendText processes input from UI (commands or regular text)
func (c *ChatClient) SendText(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}

	// Process slash commands
	result := ProcessCommand(text, c.nick)
	output := ""

	if result.Handled {
		output = result.LocalOutput

		if result.ShouldQuit {
			c.Close()
			return output, nil
		}
		if result.NickChange != "" {
			oldNick := c.nick
			c.nick = result.NickChange
			SendMessage(c.conn, Message{Type: MsgTypeNick, Nick: oldNick, Text: result.NickChange})
			// UI should update nick display via return value or callback if needed
		}
		if result.RequestUsers {
			SendMessage(c.conn, Message{Type: MsgTypeUserList})
		}
		if result.SendPing {
			c.pingStart = time.Now()
			SendMessage(c.conn, Message{Type: MsgTypePing})
		}
		// FileSend and FilePicker need rework for UI.
		// For now we assume UI handles file picking separately.
		// If user types /send <file>, we might support it if path is valid.
		if result.FileSend != nil {
			c.sendFileOffer(result.FileSend.Path, result.FileSend.Target)
			output += fmt.Sprintf("Offering file: %s\n", result.FileSend.Path)
		}

		if result.AcceptFile {
			if c.pendingFile != nil {
				SendMessage(c.conn, Message{Type: MsgTypeFileAcc, Nick: c.nick, Text: c.pendingFile.From})
				output += fmt.Sprintf("Accepted file from %s\n", c.pendingFile.From)
				c.pendingFile = nil
			} else {
				output += "No pending file to accept\n"
			}
		}
		if result.RejectFile {
			if c.pendingFile != nil {
				SendMessage(c.conn, Message{Type: MsgTypeFileRej, Nick: c.nick, Text: c.pendingFile.From})
				output += fmt.Sprintf("Rejected file from %s\n", c.pendingFile.From)
				c.pendingFile = nil
			} else {
				output += "No pending file to reject\n"
			}
		}
		if result.Message != nil {
			SendMessage(c.conn, *result.Message)
		}
		if result.StartCall != "" {
			c.mediaManager.StartCall(result.StartCall)
			output += fmt.Sprintf("Calling %s...\n", result.StartCall)
		}
		if result.StartShare != "" {
			c.mediaManager.StartShare(result.StartShare)
			output += fmt.Sprintf("Sharing screen with %s...\n", result.StartShare)
		}
		return output, nil
	}

	// Regular message
	err := SendMessage(c.conn, Message{Type: MsgTypeMsg, Nick: c.nick, Text: text})
	return "", err
}

// OfferFile is called by UI when user drags a file or picks one
func (c *ChatClient) OfferFile(path string, target string) {
	c.sendFileOffer(path, target)
}

// sendFileOffer sends a file offer (not the actual file yet)
func (c *ChatClient) sendFileOffer(path string, target string) {
	info, err := os.Stat(path)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if info.Size() > 5*1024*1024 {
		fmt.Println("File too large (max 5MB)")
		return
	}

	// Format size for display
	size := ""
	if info.Size() < 1024 {
		size = fmt.Sprintf("%dB", info.Size())
	} else if info.Size() < 1024*1024 {
		size = fmt.Sprintf("%.1fKB", float64(info.Size())/1024)
	} else {
		size = fmt.Sprintf("%.1fMB", float64(info.Size())/(1024*1024))
	}

	filename := filepath.Base(path)
	msg := Message{
		Type:   MsgTypeFileOffer,
		Nick:   c.nick,
		Text:   filename,
		Data:   size,
		Target: target,
	}
	SendMessage(c.conn, msg)

	// Track what we offered for when accept comes back
	c.lastOfferedFile = path
	c.lastOfferedTo = target

	if target != "" {
		if c.callbacks.OnSystemMessage != nil {
			c.callbacks.OnSystemMessage(fmt.Sprintf("Offered %s (%s) to %s", filename, size, target))
		}
	} else {
		if c.callbacks.OnSystemMessage != nil {
			c.callbacks.OnSystemMessage(fmt.Sprintf("Offered %s (%s) to everyone", filename, size))
		}
	}
}

// sendActualFile reads and sends the actual file data
func (c *ChatClient) sendActualFile(path string, target string) {
	data, err := os.ReadFile(path)
	if err != nil {
		if c.callbacks.OnSystemMessage != nil {
			c.callbacks.OnSystemMessage(fmt.Sprintf("Error reading file: %v", err))
		}
		return
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	filename := filepath.Base(path)

	msg := Message{
		Type:   MsgTypeFile,
		Nick:   c.nick,
		Text:   filename,
		Data:   encoded,
		Target: target,
	}
	SendMessage(c.conn, msg)
	if c.callbacks.OnSystemMessage != nil {
		c.callbacks.OnSystemMessage(fmt.Sprintf("File sent (%d bytes)", len(data)))
	}
}

// saveFile saves a received file to the current directory
func saveFile(filename string, data string, from string) {
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		fmt.Printf("Error decoding file: %v\n", err)
		return
	}

	// Sanitize filename
	safeName := filepath.Base(filename)
	err = os.WriteFile(safeName, decoded, 0644)
	if err != nil {
		fmt.Printf("Error saving file: %v\n", err)
		return
	}

	// We'll let the callback handle the notification
}

// Close disconnects the client
func (c *ChatClient) Close() {
	if c.mediaManager != nil {
		c.mediaManager.Stop()
	}
	if c.conn != nil {
		c.conn.Close()
	}
}
