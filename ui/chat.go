package ui

import (
	"fmt"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// ChatScreen represents the main chat interface
type ChatScreen struct {
	App    *App
	Nick   string
	IsHost bool

	// UI Components
	Container  *fyne.Container
	HistoryBox *fyne.Container
	Scroll     *container.Scroll
	Input      *widget.Entry
	UserList   *widget.Label
	Status     *widget.Label

	// Actions
	OnSend func(text string)
}

// NewChatScreen creates the chat UI layout
func NewChatScreen(app *App, nick string, isHost bool, onSend func(string)) *ChatScreen {
	cs := &ChatScreen{
		App:    app,
		Nick:   nick,
		IsHost: isHost,
		OnSend: onSend,
	}

	// 1. Sidebar (User List)
	cs.UserList = widget.NewLabel("Online:\n(Connecting...)")
	sidebar := container.NewVBox(
		widget.NewLabelWithStyle("Room Users", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		cs.UserList,
	)

	// 2. Chat History Area
	cs.HistoryBox = container.NewVBox()
	cs.Scroll = container.NewScroll(cs.HistoryBox)

	// 3. Input Area
	cs.Input = widget.NewEntry()
	cs.Input.SetPlaceHolder("Type a message...")
	cs.Input.OnSubmitted = func(text string) {
		if text == "" {
			return
		}
		cs.Input.SetText("")
		if cs.OnSend != nil {
			cs.OnSend(text)
		}
	}

	sendBtn := widget.NewButton("Send", func() {
		cs.Input.OnSubmitted(cs.Input.Text)
	})

	inputBar := container.NewBorder(nil, nil, nil, sendBtn, cs.Input)

	// 4. Header / Media Controls
	role := "Client"
	if isHost {
		role = "Host"
	}
	cs.Status = widget.NewLabel(fmt.Sprintf("%s (%s)", nick, role))

	callBtn := widget.NewButton("📞 Call", func() {
		// Trigger Call Dialog or Command
		if cs.OnSend != nil {
			cs.OnSend("/call") // We'll user slash command shortcuts for now
		}
	})
	screenBtn := widget.NewButton("📺 Share", func() {
		if cs.OnSend != nil {
			cs.OnSend("/share")
		}
	})

	header := container.NewHBox(
		cs.Status,
		layout.NewSpacer(),
		callBtn,
		screenBtn,
	)

	// Assemble layout
	// Border: Top=Header, Bottom=Input, Left=Sidebar, Center=History
	content := container.NewBorder(header, inputBar, sidebar, nil, cs.Scroll)

	cs.Container = content

	return cs
}

// AppendMessage adds a message bubble to the history
func (cs *ChatScreen) AppendMessage(nick, text string, isMe bool) {
	label := widget.NewLabel(text)
	label.Wrapping = fyne.TextWrapWord
	label.TextStyle = fyne.TextStyle{Monospace: true}

	// Simple styling
	var content fyne.CanvasObject
	if isMe {
		// Align right
		label.Alignment = fyne.TextAlignTrailing
		content = label
	} else {
		// Align left with nick
		nickLabel := canvas.NewText(nick, color.RGBA{R: 100, G: 100, B: 255, A: 255})
		nickLabel.TextSize = 10
		content = container.NewVBox(nickLabel, label)
	}

	cs.HistoryBox.Add(content)
	cs.Scroll.ScrollToBottom()
}

// AppendSystemMessage adds a system notice
func (cs *ChatScreen) AppendSystemMessage(text string) {
	label := widget.NewLabel(text)
	label.Alignment = fyne.TextAlignCenter
	label.TextStyle = fyne.TextStyle{Italic: true}

	cs.HistoryBox.Add(label)
	cs.Scroll.ScrollToBottom()
}

// UpdateUserList updates the sidebar
func (cs *ChatScreen) UpdateUserList(users []string) {
	cs.UserList.SetText(strings.Join(users, "\n"))
}
