package core

import "fmt"

// Settings holds user-configurable options
var Settings = struct {
	Nick  string
	Sound bool
	Port  int
}{
	Nick:  "",
	Sound: true,
	Port:  7777,
}

// PlayBell plays a terminal bell sound for notifications
func PlayBell() {
	if Settings.Sound {
		fmt.Print("\a")
	}
}
