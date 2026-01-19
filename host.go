package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Client represents a connected chat client
type Client struct {
	conn   net.Conn
	nick   string
	reader *bufio.Reader
}

// PendingOffer tracks a file offer awaiting acceptance
type PendingOffer struct {
	SenderNick   string
	SenderConn   net.Conn
	Filename     string
	RecipientNick string
}

// Host manages the chat room server
type Host struct {
	listener      net.Listener
	clients       map[net.Conn]*Client
	mutex         sync.RWMutex
	nick          string
	pendingOffers map[string]*PendingOffer // key: "sender->recipient"
}

// NewHost creates a new chat host
func NewHost(nick string) *Host {
	return &Host{
		clients:       make(map[net.Conn]*Client),
		nick:          nick,
		pendingOffers: make(map[string]*PendingOffer),
	}
}

// Start begins hosting the chat room
func (h *Host) Start() error {
	// Start mDNS advertisement
	mdns, err := StartMDNSAdvertisement()
	if err != nil {
		fmt.Printf("⚠️  mDNS advertisement failed: %v (room still accessible via IP)\n", err)
	} else {
		defer mdns.Shutdown()
	}

	// Start TCP listener
	addr := fmt.Sprintf(":%d", Settings.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}
	h.listener = listener

	localIP := getLocalIP()
	fmt.Printf("🏠 Hosting room on %s:%d\n", localIP, Settings.Port)
	fmt.Printf("👤 You are: %s (host)\n", h.nick)
	fmt.Println("📝 Type messages and press Enter to send. Ctrl+C to exit.")
	fmt.Println(strings.Repeat("─", 50))

	// Start accepting connections
	go h.acceptConnections()

	// Start host's input loop
	h.hostInputLoop()

	return nil
}

// acceptConnections handles incoming client connections
func (h *Host) acceptConnections() {
	for {
		conn, err := h.listener.Accept()
		if err != nil {
			return // Listener closed
		}
		go h.handleClient(conn)
	}
}

// handleClient manages a single client connection
func (h *Host) handleClient(conn net.Conn) {
	reader := bufio.NewReader(conn)

	// Wait for join message
	msg, err := ReadMessage(reader)
	if err != nil || msg.Type != MsgTypeJoin {
		conn.Close()
		return
	}

	client := &Client{
		conn:   conn,
		nick:   msg.Nick,
		reader: reader,
	}

	// Add client
	h.mutex.Lock()
	h.clients[conn] = client
	h.mutex.Unlock()

	// Announce join
	PlayBell()
	fmt.Printf("-> %s joined\n", client.nick)
	h.broadcast(Message{Type: MsgTypeSystem, Text: fmt.Sprintf("%s joined", client.nick)}, conn)

	// Read messages from client
	for {
		msg, err := ReadMessage(reader)
		if err != nil {
			break
		}

		switch msg.Type {
		case MsgTypeMsg:
			PlayBell()
			fmt.Printf("[%s]: %s\n", client.nick, msg.Text)
			h.broadcast(Message{Type: MsgTypeMsg, Nick: client.nick, Text: msg.Text}, nil)

		case MsgTypeNick:
			oldNick := client.nick
			client.nick = msg.Text
			fmt.Printf("-> %s is now known as %s\n", oldNick, client.nick)
			h.broadcast(Message{Type: MsgTypeSystem, Text: fmt.Sprintf("%s is now known as %s", oldNick, client.nick)}, conn)

		case MsgTypePing:
			SendMessage(conn, Message{Type: MsgTypePong})

		case MsgTypeUserList:
			users := h.getUserList()
			SendMessage(conn, Message{Type: MsgTypeUserList, Text: users})

		case MsgTypeFileOffer:
			// Store the offer and forward to recipient
			offerMsg := Message{Type: MsgTypeFileOffer, Nick: client.nick, Text: msg.Text, Data: msg.Data}
			if msg.Target != "" {
				// Targeted offer
				key := fmt.Sprintf("%s->%s", client.nick, msg.Target)
				h.pendingOffers[key] = &PendingOffer{
					SenderNick:    client.nick,
					SenderConn:    conn,
					Filename:      msg.Text,
					RecipientNick: msg.Target,
				}
				h.sendToNick(msg.Target, offerMsg)
				fmt.Printf("-> %s offers %s to %s\n", client.nick, msg.Text, msg.Target)
			} else {
				// Broadcast offer - simplified: just forward to all
				h.broadcast(offerMsg, conn)
				fmt.Printf("-> %s offers %s to everyone\n", client.nick, msg.Text)
			}

		case MsgTypeFileAcc:
			// Recipient accepted - tell sender to send the file
			key := fmt.Sprintf("%s->%s", msg.Text, client.nick) // msg.Text = sender nick
			if offer, ok := h.pendingOffers[key]; ok {
				// Tell sender their offer was accepted
				SendMessage(offer.SenderConn, Message{Type: MsgTypeFileAcc, Nick: client.nick, Text: offer.Filename})
				delete(h.pendingOffers, key)
				fmt.Printf("-> %s accepted file from %s\n", client.nick, msg.Text)
			}

		case MsgTypeFileRej:
			// Recipient rejected
			key := fmt.Sprintf("%s->%s", msg.Text, client.nick)
			if offer, ok := h.pendingOffers[key]; ok {
				SendMessage(offer.SenderConn, Message{Type: MsgTypeFileRej, Nick: client.nick})
				delete(h.pendingOffers, key)
				fmt.Printf("-> %s rejected file from %s\n", client.nick, msg.Text)
			}

		case MsgTypeFile:
			// Actual file data - route to target or broadcast
			fileMsg := Message{Type: MsgTypeFile, Nick: client.nick, Text: msg.Text, Data: msg.Data}
			if msg.Target != "" {
				h.sendToNick(msg.Target, fileMsg)
				fmt.Printf("-> %s sent file %s to %s\n", client.nick, msg.Text, msg.Target)
			} else {
				PlayBell()
				hostSaveFile(msg.Text, msg.Data, client.nick)
				h.broadcast(fileMsg, conn)
				fmt.Printf("-> %s shared file %s\n", client.nick, msg.Text)
			}
		}
	}

	// Client disconnected
	h.mutex.Lock()
	delete(h.clients, conn)
	h.mutex.Unlock()
	conn.Close()

	PlayBell()
	fmt.Printf("<- %s left\n", client.nick)
	h.broadcast(Message{Type: MsgTypeSystem, Text: fmt.Sprintf("%s left", client.nick)}, nil)
}

// broadcast sends a message to all connected clients
func (h *Host) broadcast(msg Message, exclude net.Conn) {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	for conn := range h.clients {
		if conn != exclude {
			SendMessage(conn, msg)
		}
	}
}

// getUserList returns a comma-separated list of all connected users
func (h *Host) getUserList() string {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	names := []string{h.nick + " (host)"}
	for _, client := range h.clients {
		names = append(names, client.nick)
	}
	return strings.Join(names, ", ")
}

// sendToNick sends a message to a specific user by nickname
func (h *Host) sendToNick(nick string, msg Message) bool {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	for _, client := range h.clients {
		if client.nick == nick {
			SendMessage(client.conn, msg)
			return true
		}
	}
	return false
}

// hostSaveFile saves a received file (host version - uses same logic as client)
func hostSaveFile(filename string, data string, from string) {
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		fmt.Printf("Error decoding file: %v\n", err)
		return
	}
	safeName := filepath.Base(filename)
	err = os.WriteFile(safeName, decoded, 0644)
	if err != nil {
		fmt.Printf("Error saving file: %v\n", err)
		return
	}
	fmt.Printf("-> Received %s from %s (%d bytes)\n", safeName, from, len(decoded))
}

// hostSendFile sends a file from the host to clients
func (h *Host) hostSendFile(path string, target string) {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		return
	}
	if len(data) > 5*1024*1024 {
		fmt.Println("File too large (max 5MB)")
		return
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	filename := filepath.Base(path)
	msg := Message{Type: MsgTypeFile, Nick: h.nick, Text: filename, Data: encoded}

	if target != "" {
		if h.sendToNick(target, msg) {
			fmt.Printf("-> Sent %s to %s (%d bytes)\n", filename, target, len(data))
		} else {
			fmt.Printf("User %s not found\n", target)
		}
	} else {
		h.broadcast(msg, nil)
		fmt.Printf("-> Sent %s to everyone (%d bytes)\n", filename, len(data))
	}
}

// hostPickAndSendFile shows simple numbered file picker for host
func (h *Host) hostPickAndSendFile() {
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
	h.hostSendFile(selectedFile, target)
}

// hostInputLoop reads input from the host user
func (h *Host) hostInputLoop() {
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
		result := ProcessCommand(text, h.nick)
		if result.Handled {
			if result.LocalOutput != "" {
				fmt.Print(result.LocalOutput)
			}
			if result.ShouldQuit {
				h.Shutdown()
				os.Exit(0)
			}
			if result.NickChange != "" {
				oldNick := h.nick
				h.nick = result.NickChange
				fmt.Printf("-> You are now known as %s\n", h.nick)
				h.broadcast(Message{Type: MsgTypeSystem, Text: fmt.Sprintf("%s is now known as %s", oldNick, h.nick)}, nil)
			}
			if result.RequestUsers {
				fmt.Printf("-> Online: %s\n", h.getUserList())
			}
			if result.FileSend != nil {
				h.hostSendFile(result.FileSend.Path, result.FileSend.Target)
			}
			if result.FilePicker {
				h.hostPickAndSendFile()
			}
			if result.Message != nil {
				fmt.Printf("[%s]: %s\n", result.Message.Nick, result.Message.Text)
				h.broadcast(*result.Message, nil)
			}
			continue
		}

		// Regular message
		fmt.Printf("[%s]: %s\n", h.nick, text)
		msg := Message{Type: MsgTypeMsg, Nick: h.nick, Text: text}
		h.broadcast(msg, nil)
	}
}

// Shutdown closes the host
func (h *Host) Shutdown() {
	if h.listener != nil {
		h.listener.Close()
	}

	h.mutex.Lock()
	for conn := range h.clients {
		SendMessage(conn, Message{Type: MsgTypeSystem, Text: "Room closed by host"})
		conn.Close()
	}
	h.mutex.Unlock()
}
