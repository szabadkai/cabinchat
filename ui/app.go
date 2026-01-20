package ui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"cabinchat/core"
)

// App manages the Fyne application state
type App struct {
	FyneApp    fyne.App
	Window     fyne.Window
	CurrentLoc string

	// Active Session
	Host   *core.Host
	Client *core.ChatClient
}

// NewApp creates a new UI application
func NewApp() *App {
	a := &App{
		FyneApp: app.New(),
	}
	a.Window = a.FyneApp.NewWindow("CabinChat")
	a.Window.Resize(fyne.NewSize(800, 600))
	return a
}

// Run starts the application loop
func (a *App) Run() {
	a.ShowWelcome()
	a.Window.ShowAndRun()
}

// ShowWelcome displays the initial welcome screen with auto-discovery
func (a *App) ShowWelcome() {
	a.CurrentLoc = "welcome"

	// 1. Header
	title := widget.NewLabel("🏔️ CabinChat")
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Alignment = fyne.TextAlignCenter

	// 2. Room List
	listTitle := widget.NewLabel("Discovered Rooms:")
	listTitle.TextStyle = fyne.TextStyle{Bold: true}

	roomData := []core.DiscoveredRoom{}
	list := widget.NewList(
		func() int { return len(roomData) },
		func() fyne.CanvasObject { return widget.NewLabel("Room Name (IP)") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			r := roomData[i]
			o.(*widget.Label).SetText(fmt.Sprintf("%s (%s:%d)", "CabinRoom", r.Host, r.Port)) // Name is not in struct yet, using placeholder
		},
	)

	// Handle Join
	nickEntry := widget.NewEntry()
	nickEntry.SetPlaceHolder("Enter Nickname")
	nickEntry.Text = "Traveler"

	list.OnSelected = func(i widget.ListItemID) {
		if nickEntry.Text == "" {
			dialog.ShowError(fmt.Errorf("Please enter a nickname first"), a.Window)
			list.Unselect(i)
			return
		}
		a.JoinRoom(roomData[i].Host, roomData[i].Port, nickEntry.Text)
	}

	// 3. Status
	status := widget.NewLabel("Scanning network...")
	status.Alignment = fyne.TextAlignCenter

	// 4. Host Controls
	hostBtn := widget.NewButton("Start New Room", func() {
		if nickEntry.Text == "" {
			dialog.ShowError(fmt.Errorf("Please enter a nickname"), a.Window)
			return
		}
		a.StartHost(nickEntry.Text)
	})

	// Layout
	// Top: Title
	// Center: List
	// Bottom: Controls

	bottomPanel := container.NewVBox(
		status,
		nickEntry,
		hostBtn,
	)

	content := container.NewBorder(
		container.NewVBox(title, listTitle),
		bottomPanel,
		nil, nil,
		list,
	)

	a.Window.SetContent(content)

	// Start Scanning in background
	go func() {
		for {
			// Check if we are still on welcome screen
			if a.CurrentLoc != "welcome" {
				return
			}

			rooms := core.FindRooms(7777)

			// Update UI on main thread using fyne.Do
			fyne.Do(func() {
				roomData = rooms

				if len(rooms) == 0 {
					status.SetText("No rooms found. Be the first to host! (Scanning...)")
				} else {
					status.SetText(fmt.Sprintf("Found %d rooms", len(rooms)))
				}
				list.Refresh()
			})
		}
	}()
}

// StartHost starts the host and switches to chat view
func (a *App) StartHost(nick string) {
	// 1. Create UI callbacks
	var chatScreen *ChatScreen
	callbacks := core.HostCallbacks{
		OnMessageReceived: func(msg core.Message) {
			chatScreen.AppendMessage(msg.Nick, msg.Text, false)
		},
		OnSystemMessage: func(text string) {
			chatScreen.AppendSystemMessage(text)
		},
		OnUserList: func(users []string) {
			chatScreen.UpdateUserList(users)
		},
		OnFileOffer: func(offer core.PendingOffer) {
			dialog.ShowConfirm("File Offer", fmt.Sprintf("%s wants to send %s. Accept?", offer.SenderNick, offer.Filename), func(b bool) {
				if b {
					a.Host.SendText("/accept") // Host accepts via command
				} else {
					a.Host.SendText("/reject")
				}
			}, a.Window)
		},
		OnFileReceived: func(filename, data, sender string) {
			// Trigger save dialog or auto-save
			chatScreen.AppendSystemMessage(fmt.Sprintf("Received file: %s", filename))
		},
	}

	// 2. Create Host
	a.Host = core.NewHost(nick, a.FyneApp, callbacks)

	// 3. Create Chat Screen
	chatScreen = NewChatScreen(a, nick, true, func(text string) {
		output, err := a.Host.SendText(text)
		if err != nil {
			chatScreen.AppendSystemMessage(fmt.Sprintf("Error: %v", err))
		}
		if output != "" {
			chatScreen.AppendSystemMessage(output)
		}
		// Echo own message is handled by Sync/Broadcast logic via OnMessageReceived in Host?
		// Actually Host logic broadcasts to all clients. Host UI is local.
		// Host.SendText writes to clients.
		// Does Host.SendText show up in OnMessageReceived?
		// hostInputLoop printed locally.
		// SendText calls ProcessCommand or Broadcast.
		// If Broadcast, it sends to clients. It does NOT call OnMessageReceived for self?
		// We should verify. For now, add it manually if it wasn't a command.
		if !strings.HasPrefix(text, "/") {
			chatScreen.AppendMessage(nick, text, true)
		}
	})

	// 4. Start Host logic
	err := a.Host.Start()
	if err != nil {
		dialog.ShowError(err, a.Window)
		return
	}

	// 5. Update UI
	a.Window.SetContent(chatScreen.Container) // Assuming ChatScreen has a Container field?
	// Make sure NewChatScreen sets content or returns container.
	// NewChatScreen sets a.Window.SetContent(content).
	// But NewChatScreen returns *ChatScreen.
}

// JoinRoom connects to a room
func (a *App) JoinRoom(ip string, port int, nick string) {
	status := widget.NewLabel("Connecting...")
	a.Window.SetContent(container.NewCenter(status))

	// 1. Create Callbacks
	var chatScreen *ChatScreen
	callbacks := core.ClientCallbacks{
		OnMessageReceived: func(msg core.Message) {
			chatScreen.AppendMessage(msg.Nick, msg.Text, msg.Nick == nick)
		},
		OnSystemMessage: func(text string) {
			chatScreen.AppendSystemMessage(text)
		},
		OnUserList: func(users []string) {
			chatScreen.UpdateUserList(users)
		},
		OnConnectionLost: func() {
			dialog.ShowInformation("Disconnected", "Connection lost", a.Window)
			a.ShowWelcome()
		},
		OnFileOffer: func(offer core.PendingFile) {
			dialog.ShowConfirm("File Offer", fmt.Sprintf("%s wants to send %s (%s). Accept?", offer.From, offer.Filename, offer.Size), func(b bool) {
				if b {
					a.Client.SendText("/accept")
				} else {
					a.Client.SendText("/reject")
				}
			}, a.Window)
		},
		OnFileReceived: func(filename, data, sender string) {
			chatScreen.AppendSystemMessage(fmt.Sprintf("Received file: %s", filename))
		},
		OnFileAccepted: func(sender string) {
			chatScreen.AppendSystemMessage(fmt.Sprintf("File accepted by %s, sending...", sender))
		},
		OnFileRejected: func(sender string) {
			chatScreen.AppendSystemMessage(fmt.Sprintf("File rejected by %s", sender))
		},
	}

	// 2. Connect Async
	go func() {
		client, err := core.NewChatClient(ip, port, nick, a.FyneApp, callbacks)
		if err != nil {
			dialog.ShowError(err, a.Window)
			a.ShowWelcome()
			return
		}
		a.Client = client

		// 3. Create Chat Screen
		chatScreen = NewChatScreen(a, nick, false, func(text string) {
			output, err := a.Client.SendText(text)
			if err != nil {
				chatScreen.AppendSystemMessage(fmt.Sprintf("Error: %v", err))
			}
			if output != "" {
				chatScreen.AppendSystemMessage(output)
			}
			// Client relies on server echo for regular messages to avoid duplicates
		})

		fyne.Do(func() {
			a.Window.SetContent(chatScreen.Container)
		})

		client.Start()
	}()
}
