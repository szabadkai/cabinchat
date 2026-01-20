package core

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"github.com/grandcat/zeroconf"

	"cabinchat/media"
)

// Client represents a connected chat client
type Client struct {
	conn   net.Conn
	nick   string
	reader *bufio.Reader
}

// PendingOffer tracks a file offer awaiting acceptance
type PendingOffer struct {
	SenderNick    string
	SenderConn    net.Conn
	Filename      string
	RecipientNick string
}

// HostCallbacks defines events for the host UI
type HostCallbacks struct {
	OnMessageReceived func(msg Message)
	OnSystemMessage   func(text string)
	OnUserList        func(users []string) // Triggered when someone joins/leaves
	OnFileOffer       func(offer PendingOffer)
	OnFileReceived    func(filename string, data string, sender string)
}

// Host manages the chat room server
type Host struct {
	listener        net.Listener
	clients         map[net.Conn]*Client
	mutex           sync.RWMutex
	nick            string
	pendingOffers   map[string]*PendingOffer // key: sender nick
	hostPendingFile *PendingOffer            // incoming file offer for host
	mediaManager    *media.MediaManager
	callbacks       HostCallbacks
	app             fyne.App
	mdnsServer      *zeroconf.Server
}

// NewHost creates a new chat host
func NewHost(nick string, app fyne.App, callbacks HostCallbacks) *Host {
	return &Host{
		clients:       make(map[net.Conn]*Client),
		nick:          nick,
		pendingOffers: make(map[string]*PendingOffer),
		callbacks:     callbacks,
		app:           app,
	}
}

// Start begins hosting the chat room
func (h *Host) Start() error {
	// Start mDNS advertisement
	// Start mDNS advertisement
	server, err := StartMDNSAdvertisement()
	if err != nil {
		fmt.Printf("⚠️  mDNS advertisement failed: %v (room still accessible via IP)\n", err)
	} else {
		h.mdnsServer = server
	}

	// Start TCP listener
	addr := fmt.Sprintf(":%d", Settings.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}
	h.listener = listener

	// Initialize Media Manager for Host
	h.mediaManager = media.NewMediaManager(h.app, func(target string, data string) {
		msg := Message{
			Type:   MsgTypeWebRTC,
			Nick:   h.nick,
			Text:   "signal",
			Data:   data,
			Target: target,
		}
		if target != "" {
			h.sendToNick(target, msg)
		} else {
			// Broadcast? Not really typical for signaling
		}
	})

	localIP := getLocalIP()
	if h.callbacks.OnSystemMessage != nil {
		h.callbacks.OnSystemMessage(fmt.Sprintf("Hosting room on %s:%d", localIP, Settings.Port))
	}

	// Start accepting connections
	go h.acceptConnections()

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
	// PlayBell() // UI should handle sound
	if h.callbacks.OnSystemMessage != nil {
		h.callbacks.OnSystemMessage(fmt.Sprintf("%s joined", client.nick))
	}
	h.broadcast(Message{Type: MsgTypeSystem, Text: fmt.Sprintf("%s joined", client.nick)}, conn)
	if h.callbacks.OnUserList != nil {
		h.callbacks.OnUserList(strings.Split(h.getUserList(), ", "))
	}

	// Read messages from client
	for {
		msg, err := ReadMessage(reader)
		if err != nil {
			break
		}

		switch msg.Type {
		case MsgTypeMsg:
			// PlayBell()
			if h.callbacks.OnMessageReceived != nil {
				h.callbacks.OnMessageReceived(Message{Nick: client.nick, Text: msg.Text})
			}
			h.broadcast(Message{Type: MsgTypeMsg, Nick: client.nick, Text: msg.Text}, nil)

		case MsgTypeNick:
			oldNick := client.nick
			client.nick = msg.Text
			sysMsg := fmt.Sprintf("%s is now known as %s", oldNick, client.nick)
			if h.callbacks.OnSystemMessage != nil {
				h.callbacks.OnSystemMessage(sysMsg)
			}
			if h.callbacks.OnUserList != nil {
				h.callbacks.OnUserList(strings.Split(h.getUserList(), ", "))
			}
			h.broadcast(Message{Type: MsgTypeSystem, Text: sysMsg}, conn)

		case MsgTypePing:
			SendMessage(conn, Message{Type: MsgTypePong})

		case MsgTypeUserList:
			users := h.getUserList()
			SendMessage(conn, Message{Type: MsgTypeUserList, Text: users})

		case MsgTypeFileOffer:
			// Store the offer and forward to recipient(s)
			offerMsg := Message{Type: MsgTypeFileOffer, Nick: client.nick, Text: msg.Text, Data: msg.Data}
			// Store by sender nick only - any recipient can accept
			h.pendingOffers[client.nick] = &PendingOffer{
				SenderNick:    client.nick,
				SenderConn:    conn,
				Filename:      msg.Text,
				RecipientNick: msg.Target, // may be empty for broadcast
			}
			if msg.Target != "" {
				if msg.Target == h.nick {
					// Targeted offer to host
					h.hostPendingFile = &PendingOffer{
						SenderNick: client.nick,
						SenderConn: conn,
						Filename:   msg.Text,
					}
					// PlayBell()
					if h.callbacks.OnFileOffer != nil {
						h.callbacks.OnFileOffer(*h.hostPendingFile)
					}
				} else {
					h.sendToNick(msg.Target, offerMsg)
					if h.callbacks.OnSystemMessage != nil {
						h.callbacks.OnSystemMessage(fmt.Sprintf("%s offers %s to %s", client.nick, msg.Text, msg.Target))
					}
				}
			} else {
				// Broadcast offer to all clients
				h.broadcast(offerMsg, conn)
				// Also track for host
				h.hostPendingFile = &PendingOffer{
					SenderNick: client.nick,
					SenderConn: conn,
					Filename:   msg.Text,
				}
				// PlayBell()
				if h.callbacks.OnFileOffer != nil {
					h.callbacks.OnFileOffer(*h.hostPendingFile)
				}
			}

		case MsgTypeFileAcc:
			// Recipient accepted - tell sender to send the file
			senderNick := msg.Text // msg.Text = sender nick they're accepting from
			if offer, ok := h.pendingOffers[senderNick]; ok {
				// Tell sender their offer was accepted, include who accepted
				SendMessage(offer.SenderConn, Message{Type: MsgTypeFileAcc, Nick: client.nick, Text: offer.Filename})
				delete(h.pendingOffers, senderNick)
				if h.callbacks.OnSystemMessage != nil {
					h.callbacks.OnSystemMessage(fmt.Sprintf("%s accepted file from %s", client.nick, senderNick))
				}
			}

		case MsgTypeFileRej:
			// Recipient rejected
			senderNick := msg.Text
			if offer, ok := h.pendingOffers[senderNick]; ok {
				SendMessage(offer.SenderConn, Message{Type: MsgTypeFileRej, Nick: client.nick})
				delete(h.pendingOffers, senderNick)
				if h.callbacks.OnSystemMessage != nil {
					h.callbacks.OnSystemMessage(fmt.Sprintf("%s rejected file from %s", client.nick, senderNick))
				}
			}

		case MsgTypeFile:
			// Actual file data - route to target or broadcast
			fileMsg := Message{Type: MsgTypeFile, Nick: client.nick, Text: msg.Text, Data: msg.Data}
			if msg.Target != "" {
				if msg.Target == h.nick {
					// Sent to host
					// PlayBell()
					hostSaveFile(msg.Text, msg.Data, client.nick)
					if h.callbacks.OnFileReceived != nil {
						h.callbacks.OnFileReceived(msg.Text, msg.Data, client.nick)
					}
				} else {
					h.sendToNick(msg.Target, fileMsg)
					if h.callbacks.OnSystemMessage != nil {
						h.callbacks.OnSystemMessage(fmt.Sprintf("%s sent file %s to %s", client.nick, msg.Text, msg.Target))
					}
				}
			} else {
				// PlayBell()
				hostSaveFile(msg.Text, msg.Data, client.nick)
				if h.callbacks.OnFileReceived != nil {
					h.callbacks.OnFileReceived(msg.Text, msg.Data, client.nick)
				}
				h.broadcast(fileMsg, conn)
				if h.callbacks.OnSystemMessage != nil {
					h.callbacks.OnSystemMessage(fmt.Sprintf("%s shared file %s", client.nick, msg.Text))
				}
			}

		case MsgTypeWebRTC:
			// Route signal
			if msg.Target == h.nick {
				// For host
				h.mediaManager.HandleSignal(client.nick, msg.Data)
			} else {
				// Forward to target
				forwardMsg := Message{Type: MsgTypeWebRTC, Nick: client.nick, Data: msg.Data, Target: msg.Target}
				if !h.sendToNick(msg.Target, forwardMsg) {
					// Target not found
				}
			}
		}
	}

	// Client disconnected
	h.mutex.Lock()
	delete(h.clients, conn)
	h.mutex.Unlock()
	conn.Close()

	// PlayBell()
	sysMsg := fmt.Sprintf("%s left", client.nick)
	if h.callbacks.OnSystemMessage != nil {
		h.callbacks.OnSystemMessage(sysMsg)
	}
	if h.callbacks.OnUserList != nil {
		h.callbacks.OnUserList(strings.Split(h.getUserList(), ", "))
	}
	h.broadcast(Message{Type: MsgTypeSystem, Text: sysMsg}, nil)
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
			if h.callbacks.OnSystemMessage != nil {
				h.callbacks.OnSystemMessage(fmt.Sprintf("Sent %s to %s (%d bytes)", filename, target, len(data)))
			}
		} else {
			if h.callbacks.OnSystemMessage != nil {
				h.callbacks.OnSystemMessage(fmt.Sprintf("User %s not found", target))
			}
		}
	} else {
		h.broadcast(msg, nil)
		if h.callbacks.OnSystemMessage != nil {
			h.callbacks.OnSystemMessage(fmt.Sprintf("Sent %s to everyone (%d bytes)", filename, len(data)))
		}
	}
}

// SendText processes input from Host UI
func (h *Host) SendText(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}

	// Process slash commands
	result := ProcessCommand(text, h.nick)
	output := ""

	if result.Handled {
		output = result.LocalOutput

		if result.ShouldQuit {
			h.Shutdown()
			// Notify UI to close?
			// For now, return
			return output, nil
		}
		if result.NickChange != "" {
			oldNick := h.nick
			h.nick = result.NickChange
			sysMsg := fmt.Sprintf("%s is now known as %s", oldNick, h.nick)
			h.broadcast(Message{Type: MsgTypeSystem, Text: sysMsg}, nil)
			// Trigger local callback for system message?
			// Actually UI should just update.
		}
		if result.RequestUsers {
			if h.callbacks.OnSystemMessage != nil {
				h.callbacks.OnSystemMessage(fmt.Sprintf("Online: %s", h.getUserList()))
			}
		}
		if result.FileSend != nil {
			h.hostSendFile(result.FileSend.Path, result.FileSend.Target)
			output += fmt.Sprintf("Sending file: %s\n", result.FileSend.Path)
		}
		if result.AcceptFile {
			if h.hostPendingFile != nil {
				SendMessage(h.hostPendingFile.SenderConn, Message{Type: MsgTypeFileAcc, Nick: h.nick, Text: h.hostPendingFile.Filename})
				output += fmt.Sprintf("Accepted file from %s\n", h.hostPendingFile.SenderNick)
				h.hostPendingFile = nil
			} else {
				output += "No pending file to accept\n"
			}
		}
		if result.RejectFile {
			if h.hostPendingFile != nil {
				SendMessage(h.hostPendingFile.SenderConn, Message{Type: MsgTypeFileRej, Nick: h.nick})
				output += fmt.Sprintf("Rejected file from %s\n", h.hostPendingFile.SenderNick)
				h.hostPendingFile = nil
			} else {
				output += "No pending file to reject\n"
			}
		}
		if result.Message != nil {
			h.broadcast(*result.Message, nil)
		}
		if result.StartCall != "" {
			h.mediaManager.StartCall(result.StartCall)
			output += fmt.Sprintf("Calling %s...\n", result.StartCall)
		}
		if result.StartShare != "" {
			h.mediaManager.StartShare(result.StartShare)
			output += fmt.Sprintf("Sharing screen with %s...\n", result.StartShare)
		}
		return output, nil
	}

	// Regular message
	msg := Message{Type: MsgTypeMsg, Nick: h.nick, Text: text}
	h.broadcast(msg, nil)
	return "", nil
}

// OfferFile is called by UI
func (h *Host) OfferFile(path string, target string) {
	h.hostSendFile(path, target)
}

// Shutdown closes the host
func (h *Host) Shutdown() {
	if h.listener != nil {
		h.listener.Close()
	}
	if h.mediaManager != nil {
		h.mediaManager.Stop()
	}
	if h.mdnsServer != nil {
		h.mdnsServer.Shutdown()
	}

	h.mutex.Lock()
	for conn := range h.clients {
		SendMessage(conn, Message{Type: MsgTypeSystem, Text: "Room closed by host"})
		conn.Close()
	}
	h.mutex.Unlock()
}
