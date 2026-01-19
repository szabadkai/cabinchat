package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PendingFile represents a file offer waiting for acceptance
type PendingFile struct {
	From     string
	Filename string
	Size     string
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
}

// NewChatClient creates a new client and connects to the host
func NewChatClient(host string, port int, nick string) (*ChatClient, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	client := &ChatClient{
		conn:   conn,
		nick:   nick,
		reader: bufio.NewReader(conn),
	}

	// Send join message
	err = SendMessage(conn, Message{Type: MsgTypeJoin, Nick: nick})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to join: %w", err)
	}

	return client, nil
}

// Start begins the chat client
func (c *ChatClient) Start() {
	fmt.Printf("✅ Connected to room!\n")
	fmt.Printf("👤 You are: %s\n", c.nick)
	fmt.Println("📝 Type messages and press Enter to send. Ctrl+C to exit.")
	fmt.Println(strings.Repeat("─", 50))

	// Start receive loop in background
	go c.receiveLoop()

	// Run send loop in foreground
	c.sendLoop()
}

// receiveLoop reads messages from the server
func (c *ChatClient) receiveLoop() {
	for {
		msg, err := ReadMessage(c.reader)
		if err != nil {
			fmt.Println("\nDisconnected from room")
			os.Exit(0)
			return
		}

		switch msg.Type {
		case MsgTypeMsg:
			// Don't print our own messages (we already see them when typing)
			if msg.Nick != c.nick {
				PlayBell()
				fmt.Printf("[%s]: %s\n", msg.Nick, msg.Text)
			}
		case MsgTypeSystem:
			PlayBell()
			fmt.Printf("-> %s\n", msg.Text)
		case MsgTypePong:
			elapsed := time.Since(c.pingStart)
			fmt.Printf("-> Pong! %dms\n", elapsed.Milliseconds())
		case MsgTypeUserList:
			fmt.Printf("-> Online: %s\n", msg.Text)
		case MsgTypeFileOffer:
			// Someone wants to send us a file
			PlayBell()
			c.pendingFile = &PendingFile{From: msg.Nick, Filename: msg.Text, Size: msg.Data}
			fmt.Printf("\n-> %s wants to send you: %s (%s)\n", msg.Nick, msg.Text, msg.Data)
			fmt.Println("   Type /accept or /reject")
		case MsgTypeFileAcc:
			// Our offer was accepted - send the actual file
			if c.lastOfferedFile != "" {
				fmt.Printf("-> %s accepted, sending file...\n", msg.Nick)
				c.sendActualFile(c.lastOfferedFile, msg.Nick)
				c.lastOfferedFile = ""
				c.lastOfferedTo = ""
			}
		case MsgTypeFileRej:
			// Our offer was rejected
			fmt.Printf("-> %s rejected your file\n", msg.Nick)
			c.lastOfferedFile = ""
			c.lastOfferedTo = ""
		case MsgTypeFile:
			// Actual file data received
			PlayBell()
			saveFile(msg.Text, msg.Data, msg.Nick)
		}
	}
}

// sendLoop reads user input and sends messages
func (c *ChatClient) sendLoop() {
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		text := strings.TrimSpace(line)
		if text == "" {
			continue
		}

		// Process slash commands
		result := ProcessCommand(text, c.nick)
		if result.Handled {
			if result.LocalOutput != "" {
				fmt.Print(result.LocalOutput)
			}
			if result.ShouldQuit {
				c.Close()
				os.Exit(0)
			}
			if result.NickChange != "" {
				oldNick := c.nick
				c.nick = result.NickChange
				SendMessage(c.conn, Message{Type: MsgTypeNick, Nick: oldNick, Text: result.NickChange})
				fmt.Printf("-> You are now known as %s\n", c.nick)
			}
			if result.RequestUsers {
				SendMessage(c.conn, Message{Type: MsgTypeUserList})
			}
			if result.SendPing {
				c.pingStart = time.Now()
				SendMessage(c.conn, Message{Type: MsgTypePing})
			}
			if result.FileSend != nil {
				c.sendFileOffer(result.FileSend.Path, result.FileSend.Target)
			}
			if result.FilePicker {
				c.pickAndSendFile()
			}
			if result.AcceptFile {
				if c.pendingFile != nil {
					SendMessage(c.conn, Message{Type: MsgTypeFileAcc, Nick: c.nick, Text: c.pendingFile.From})
					fmt.Printf("-> Accepted file from %s\n", c.pendingFile.From)
					c.pendingFile = nil
				} else {
					fmt.Println("No pending file to accept")
				}
			}
			if result.RejectFile {
				if c.pendingFile != nil {
					SendMessage(c.conn, Message{Type: MsgTypeFileRej, Nick: c.nick, Text: c.pendingFile.From})
					fmt.Printf("-> Rejected file from %s\n", c.pendingFile.From)
					c.pendingFile = nil
				} else {
					fmt.Println("No pending file to reject")
				}
			}
			if result.Message != nil {
				fmt.Printf("[%s]: %s\n", result.Message.Nick, result.Message.Text)
				SendMessage(c.conn, *result.Message)
			}
			continue
		}

		// Regular message
		fmt.Printf("[%s]: %s\n", c.nick, text)
		err = SendMessage(c.conn, Message{Type: MsgTypeMsg, Nick: c.nick, Text: text})
		if err != nil {
			fmt.Println("Failed to send message")
		break
		}
	}
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
		fmt.Printf("-> Offered %s (%s) to %s (waiting for accept)\n", filename, size, target)
	} else {
		fmt.Printf("-> Offered %s (%s) to everyone (waiting for accept)\n", filename, size)
	}
}

// sendActualFile reads and sends the actual file data
func (c *ChatClient) sendActualFile(path string, target string) {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
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
	fmt.Printf("-> File sent (%d bytes)\n", len(data))
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

	fmt.Printf("-> Received %s from %s (%d bytes)\n", safeName, from, len(decoded))
}

// pickAndSendFile shows a simple numbered file picker
func (c *ChatClient) pickAndSendFile() {
	entries, err := os.ReadDir(".")
	if err != nil {
		fmt.Printf("Error reading directory: %v\n", err)
		return
	}

	var files []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e)
		}
	}

	if len(files) == 0 {
		fmt.Println("No files in current directory")
		return
	}

	fmt.Println("\n+--- Files ---+")
	for i, f := range files {
		info, _ := f.Info()
		size := ""
		if info != nil {
			if info.Size() < 1024 {
				size = fmt.Sprintf("%dB", info.Size())
			} else if info.Size() < 1024*1024 {
				size = fmt.Sprintf("%.1fKB", float64(info.Size())/1024)
			} else {
				size = fmt.Sprintf("%.1fMB", float64(info.Size())/(1024*1024))
			}
		}
		fmt.Printf("| [%2d] %-20s %8s |\n", i+1, f.Name(), size)
	}
	fmt.Println("+-------------+")

	choice := promptInput(fmt.Sprintf("Pick (1-%d, 0=cancel): ", len(files)))
	var idx int
	fmt.Sscanf(choice, "%d", &idx)
	if idx < 1 || idx > len(files) {
		fmt.Println("Cancelled")
		return
	}

	selectedFile := files[idx-1].Name()
	target := promptInput("Target nick (Enter=all): ")
	c.sendFileOffer(selectedFile, target)
}

// Close disconnects the client
func (c *ChatClient) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}
