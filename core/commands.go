package core

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// CommandResult represents the result of processing a slash command
type CommandResult struct {
	Handled      bool
	Message      *Message // nil if command was local-only (like /help)
	LocalOutput  string   // Text to print locally
	ShouldQuit   bool
	NickChange   string           // New nickname if changing
	RequestUsers bool             // Request user list from host
	SendPing     bool             // Send ping to host
	FileSend     *FileSendRequest // File to send
	FilePicker   bool             // Show interactive file picker
	AcceptFile   bool             // Accept pending file transfer
	RejectFile   bool             // Reject pending file transfer
	StartCall    string           // Target nick for VOIP call
	StartShare   string           // Target nick for Screen Share
}

// FileSendRequest holds file transfer info
type FileSendRequest struct {
	Path   string
	Target string // empty = broadcast to all
}

// ProcessCommand handles slash commands, returns true if handled
func ProcessCommand(input string, nick string) CommandResult {
	if !strings.HasPrefix(input, "/") {
		return CommandResult{Handled: false}
	}

	parts := strings.SplitN(input, " ", 2)
	cmd := strings.ToLower(parts[0])
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}

	switch cmd {
	case "/help", "/?":
		return CommandResult{
			Handled:     true,
			LocalOutput: helpText(),
		}

	case "/me":
		if args == "" {
			return CommandResult{Handled: true, LocalOutput: "Usage: /me <action>"}
		}
		return CommandResult{
			Handled: true,
			Message: &Message{Type: MsgTypeMsg, Nick: "*", Text: fmt.Sprintf("%s %s", nick, args)},
		}

	case "/slap":
		target := args
		if target == "" {
			target = "themselves"
		}
		text := fmt.Sprintf("%s slaps %s around a bit with a large trout 🐟", nick, target)
		return CommandResult{
			Handled: true,
			Message: &Message{Type: MsgTypeMsg, Nick: "*", Text: text},
		}

	case "/shrug":
		return CommandResult{
			Handled: true,
			Message: &Message{Type: MsgTypeMsg, Nick: nick, Text: "¯\\_(ツ)_/¯"},
		}

	case "/flip", "/tableflip":
		return CommandResult{
			Handled: true,
			Message: &Message{Type: MsgTypeMsg, Nick: nick, Text: "(╯°□°)╯︵ ┻━┻"},
		}

	case "/unflip":
		return CommandResult{
			Handled: true,
			Message: &Message{Type: MsgTypeMsg, Nick: nick, Text: "┬─┬ノ( º _ ºノ)"},
		}

	case "/rage":
		rages := []string{
			"ASDFJKL;ASDJFKL;ASDJF",
			"@#$%^&*!@#$%^&*",
			"REEEEEEEEEE",
			"I FLIP ALL THE TABLES (╯°□°)╯︵ ┻━┻ ︵ ╯(°□° ╯)",
			"KEYBOARD SMASH: " + randomSmash(),
		}
		return CommandResult{
			Handled: true,
			Message: &Message{Type: MsgTypeMsg, Nick: nick, Text: rages[rand.Intn(len(rages))]},
		}

	case "/dice", "/roll":
		n := rand.Intn(6) + 1
		return CommandResult{
			Handled: true,
			Message: &Message{Type: MsgTypeMsg, Nick: "*", Text: fmt.Sprintf("%s rolls a dice and gets %d", nick, n)},
		}

	case "/coin", "/flip-coin":
		result := "heads"
		if rand.Intn(2) == 1 {
			result = "tails"
		}
		return CommandResult{
			Handled: true,
			Message: &Message{Type: MsgTypeMsg, Nick: "*", Text: fmt.Sprintf("%s flips a coin: %s!", nick, result)},
		}

	case "/lenny":
		return CommandResult{
			Handled: true,
			Message: &Message{Type: MsgTypeMsg, Nick: nick, Text: "( \u0361\u00b0 \u035c\u0296 \u0361\u00b0)"},
		}

	case "/disapprove":
		return CommandResult{
			Handled: true,
			Message: &Message{Type: MsgTypeMsg, Nick: nick, Text: "\u0ca0_\u0ca0"},
		}

	case "/fight":
		target := args
		if target == "" {
			target = "the air"
		}
		moves := []string{
			fmt.Sprintf("%s throws a punch at %s!", nick, target),
			fmt.Sprintf("%s challenges %s to mortal combat!", nick, target),
			fmt.Sprintf("%s summons a mass of wild ferrets to attack %s!", nick, target),
		}
		return CommandResult{
			Handled: true,
			Message: &Message{Type: MsgTypeMsg, Nick: "*", Text: moves[rand.Intn(len(moves))]},
		}

	case "/nick":
		if args == "" {
			return CommandResult{Handled: true, LocalOutput: "Usage: /nick <newnickname>"}
		}
		newNick := strings.TrimSpace(args)
		if len(newNick) > 20 {
			return CommandResult{Handled: true, LocalOutput: "Nickname too long (max 20 chars)"}
		}
		return CommandResult{
			Handled:    true,
			NickChange: newNick,
		}

	case "/users", "/who", "/list":
		return CommandResult{
			Handled:      true,
			RequestUsers: true,
		}

	case "/time":
		now := time.Now().Format("Mon Jan 2 15:04:05 2006")
		return CommandResult{
			Handled:     true,
			LocalOutput: fmt.Sprintf("Current time: %s", now),
		}

	case "/clear", "/cls":
		// ANSI escape to clear screen
		return CommandResult{
			Handled:     true,
			LocalOutput: "\033[2J\033[H",
		}

	case "/ping":
		return CommandResult{
			Handled:  true,
			SendPing: true,
		}

	case "/send":
		// Usage: /send <file> [nick] or /send @ for picker
		if args == "" {
			return CommandResult{Handled: true, LocalOutput: "Usage: /send <filepath> [nick] or /send @ to pick\n"}
		}
		if args == "@" {
			return CommandResult{
				Handled:    true,
				FilePicker: true,
			}
		}
		parts := strings.SplitN(args, " ", 2)
		filePath := parts[0]
		target := ""
		if len(parts) > 1 {
			target = strings.TrimSpace(parts[1])
		}
		return CommandResult{
			Handled:  true,
			FileSend: &FileSendRequest{Path: filePath, Target: target},
		}

	case "/accept", "/y", "/yes":
		return CommandResult{
			Handled:    true,
			AcceptFile: true,
		}

	case "/reject", "/n", "/no", "/decline":
		return CommandResult{
			Handled:    true,
			RejectFile: true,
		}

	case "/call":
		// Usage: /call <nick>
		if args == "" {
			return CommandResult{Handled: true, LocalOutput: "Usage: /call <nick>\n"}
		}
		return CommandResult{
			Handled:   true,
			StartCall: strings.TrimSpace(args),
		}

	case "/share":
		// Usage: /share <nick>
		if args == "" {
			return CommandResult{Handled: true, LocalOutput: "Usage: /share <nick>\n"}
		}
		return CommandResult{
			Handled:    true,
			StartShare: strings.TrimSpace(args),
		}

	case "/quit", "/exit", "/q":
		return CommandResult{
			Handled:     true,
			LocalOutput: "Leaving...\n",
			ShouldQuit:  true,
		}

	default:
		return CommandResult{
			Handled:     true,
			LocalOutput: fmt.Sprintf("Unknown command: %s (try /help)", cmd),
		}
	}
}

func helpText() string {
	return `
+------------------------------------------+
|           CabinChat Commands             |
+------------------------------------------+
| UTILITY                                  |
|   /nick <name>    Change your nickname   |
|   /users          List online users      |
|   /send <file>    Send a file            |
|   /send @         Pick from list         |
|   /accept         Accept file transfer   |
|   /reject         Reject file transfer   |
|   /call <nick>    Call a user           |
|   /share <nick>   Share screen          |
|   /ping           Check connection       |
|   /time           Show current time      |
|   /clear          Clear screen           |
|   /quit           Leave the room         |
+------------------------------------------+
| FUN                                      |
|   /me <action>    Action message         |
|   /slap <user>    Classic IRC slap       |
|   /shrug          Shrug emoticon         |
|   /flip           Flip a table           |
|   /unflip         Put it back            |
|   /rage           Express yourself       |
|   /dice           Roll a d6              |
|   /coin           Flip a coin            |
|   /lenny          Lenny face             |
|   /disapprove     Look of disapproval    |
|   /fight <who>    Start a fight          |
+------------------------------------------+
`
}

func randomSmash() string {
	chars := "ASDFJKL;QWERTY!@#$%^&*"
	result := make([]byte, 15)
	for i := range result {
		result[i] = chars[rand.Intn(len(chars))]
	}
	return string(result)
}
