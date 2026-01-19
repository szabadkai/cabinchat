package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func main() {
	// Parse command-line flags
	flag.StringVar(&Settings.Nick, "nick", "", "Set your nickname (skip prompt)")
	flag.BoolVar(&Settings.Sound, "sound", true, "Enable sound notifications")
	flag.IntVar(&Settings.Port, "port", 7777, "Port to use for hosting/connecting")
	flag.Parse()

	fmt.Println("🏔️  CabinChat - Local Network Chatroom")
	fmt.Println(strings.Repeat("═", 40))

	// Handle Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Try to discover existing room
	room, err := DiscoverRoom()
	if err != nil {
		fmt.Printf("⚠️  Discovery error: %v\n", err)
	}

	if room != nil {
		// Found a room - ask to join
		fmt.Printf("📡 Found room at %s:%d\n", room.Host, room.Port)
		if promptYesNo("Join this room?") {
			nick := Settings.Nick
			if nick == "" {
				nick = promptInput("Enter your nickname: ")
			}
			if nick == "" {
				nick = "Anonymous"
			}

			client, err := NewChatClient(room.Host, room.Port, nick)
			if err != nil {
				fmt.Printf("❌ Failed to connect: %v\n", err)
				os.Exit(1)
			}

			// Handle shutdown
			go func() {
				<-sigChan
				fmt.Println("\n👋 Leaving room...")
				client.Close()
				os.Exit(0)
			}()

			client.Start()
		} else {
			fmt.Println("👋 Bye!")
		}
	} else {
		// No room found - offer to host
		fmt.Println("📡 No rooms found nearby")
		if promptYesNo("Host a new room?") {
			nick := Settings.Nick
			if nick == "" {
				nick = promptInput("Enter your nickname: ")
			}
			if nick == "" {
				nick = "Host"
			}

			host := NewHost(nick)

			// Handle shutdown
			go func() {
				<-sigChan
				fmt.Println("\n👋 Closing room...")
				host.Shutdown()
				os.Exit(0)
			}()

			err := host.Start()
			if err != nil {
				fmt.Printf("❌ Failed to host: %v\n", err)
				os.Exit(1)
			}
		} else {
			fmt.Println("👋 Bye!")
		}
	}
}
