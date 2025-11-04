package agent

import (
	"bufio"
	// "fmt" kept for future debugging
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

// ARPEntry represents a single ARP/neighbor cache entry
type ARPEntry struct {
	IP  string `json:"ip"`
	MAC string `json:"mac"`
}

// GetARPTable returns ARP/neighbor cache entries found on the host.
// It's a best-effort, cross-platform reader: Linux uses /proc/net/arp,
// other systems try `arp -a` output.
func GetARPTable() ([]ARPEntry, error) {
	if runtime.GOOS == "linux" {
		return parseProcNetARP()
	}
	// Fallback to arp -a for windows/mac
	return parseArpA()
}

func parseProcNetARP() ([]ARPEntry, error) {
	f, err := os.Open("/proc/net/arp")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	entries := []ARPEntry{}
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			first = false
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		ip := fields[0]
		mac := fields[3]
		if mac == "00:00:00:00:00:00" {
			continue
		}
		entries = append(entries, ARPEntry{IP: ip, MAC: mac})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func parseArpA() ([]ARPEntry, error) {
	cmd := exec.Command("arp", "-a")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	text := string(out)
	lines := strings.Split(text, "\n")
	entries := []ARPEntry{}
	// Example lines vary by OS; use regex to capture ip and mac
	re := regexp.MustCompile(`([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+).*?(([0-9a-fA-F]{2}[:-]){5}([0-9a-fA-F]{2}))`)
	for _, l := range lines {
		m := re.FindStringSubmatch(l)
		if len(m) >= 3 {
			ip := m[1]
			mac := m[2]
			entries = append(entries, ARPEntry{IP: ip, MAC: mac})
		}
	}
	return entries, nil
}
