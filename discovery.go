package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
)

const (
	ServiceName = "_cabinchat._tcp"
	Domain      = "local."
)

// DiscoveredRoom represents a found chatroom
type DiscoveredRoom struct {
	Host string
	Port int
}

// DiscoverRoom looks for an existing CabinChat room on the network
func DiscoverRoom() (*DiscoveredRoom, error) {
	fmt.Println("🔍 Searching for nearby rooms...")

	// Try mDNS first
	room, err := discoverMDNS()
	if err == nil && room != nil {
		return room, nil
	}

	// Fallback to port scanning on Windows
	if runtime.GOOS == "windows" {
		fmt.Println("   mDNS failed, trying network scan...")
		return discoverFallback()
	}

	return nil, nil
}

// discoverMDNS uses mDNS/Bonjour to find rooms
func discoverMDNS() (*DiscoveredRoom, error) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, err
	}

	entries := make(chan *zeroconf.ServiceEntry)
	var foundRoom *DiscoveredRoom

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() {
		for entry := range entries {
			if len(entry.AddrIPv4) > 0 {
				foundRoom = &DiscoveredRoom{
					Host: entry.AddrIPv4[0].String(),
					Port: entry.Port,
				}
				cancel()
				return
			}
		}
	}()

	err = resolver.Browse(ctx, ServiceName, Domain, entries)
	if err != nil {
		return nil, err
	}

	<-ctx.Done()
	return foundRoom, nil
}

// discoverFallback scans local subnet for the chat port (Windows fallback)
func discoverFallback() (*DiscoveredRoom, error) {
	ips := getSubnetIPs()
	if len(ips) == 0 {
		return nil, nil
	}

	var wg sync.WaitGroup
	found := make(chan *DiscoveredRoom, 1)

	// Limit concurrent connections
	semaphore := make(chan struct{}, 50)

	for _, ip := range ips {
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			addr := fmt.Sprintf("%s:%d", ip, Settings.Port)
			conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
			if err == nil {
				conn.Close()
				select {
				case found <- &DiscoveredRoom{Host: ip, Port: Settings.Port}:
				default:
				}
			}
		}(ip)
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case room := <-found:
		return room, nil
	case <-done:
		return nil, nil
	case <-time.After(5 * time.Second):
		return nil, nil
	}
}

// StartMDNSAdvertisement advertises the room via mDNS
func StartMDNSAdvertisement() (*zeroconf.Server, error) {
	hostname, _ := os.Hostname()
	server, err := zeroconf.Register(
		hostname,
		ServiceName,
		Domain,
		Settings.Port,
		[]string{"CabinChat room"},
		nil,
	)
	return server, err
}
