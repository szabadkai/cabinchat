package core

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
)

// getLocalIP returns the preferred outbound IP of this machine
func getLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "unknown"
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

// getSubnetIPs returns a list of IPs to scan in the local /24 subnet
func getSubnetIPs() []string {
	localIP := getLocalIP()
	if localIP == "unknown" {
		return nil
	}

	parts := strings.Split(localIP, ".")
	if len(parts) != 4 {
		return nil
	}

	prefix := strings.Join(parts[:3], ".")
	var ips []string
	for i := 1; i < 255; i++ {
		ip := fmt.Sprintf("%s.%d", prefix, i)
		if ip != localIP {
			ips = append(ips, ip)
		}
	}
	return ips
}

// promptInput reads a line from stdin with a prompt
func promptInput(prompt string) string {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

// promptYesNo asks a yes/no question and returns true for yes
func promptYesNo(prompt string) bool {
	answer := promptInput(prompt + " (y/n): ")
	answer = strings.ToLower(answer)
	return answer == "y" || answer == "yes"
}
