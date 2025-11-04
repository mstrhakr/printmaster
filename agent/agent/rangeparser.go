package agent

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
)

// ParseError reports an error parsing a specific line
type ParseError struct {
	Line int    `json:"line"`
	Msg  string `json:"msg"`
}

// ParseResult is the result of parsing user-supplied range text
type ParseResult struct {
	IPs        []string     `json:"ips"`
	Count      int          `json:"count"`
	Errors     []ParseError `json:"errors"`
	Normalized []string     `json:"normalized"`
}

// helper: convert net.IP to uint32
func ipToUint32(ip net.IP) uint32 {
	ip = ip.To4()
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

// helper: convert uint32 to net.IP
func uint32ToIP(n uint32) net.IP {
	return net.IPv4(byte(n>>24), byte(n>>16), byte(n>>8), byte(n)).To4()
}

// expandCIDRCount returns the number of addresses in the cidr (including network/broadcast).
func expandCIDRCount(ipnet *net.IPNet) int {
	ones, bits := ipnet.Mask.Size()
	hostBits := bits - ones
	if hostBits >= 31 { // too large to handle comfortably
		return 1 << 30
	}
	return 1 << uint(hostBits)
}

// ParseRangeText parses the text (one entry per line) into a list of IPv4 addresses (strings). It enforces
// a maximum number of addresses (maxAddrs). It supports: single IP, CIDR, full start-end, shorthand start-end
// where right side supplies last N octets, and last-octet wildcard (x or *).
func ParseRangeText(text string, maxAddrs int) (*ParseResult, error) {
	res := &ParseResult{}
	seen := map[string]struct{}{}
	lines := strings.Split(text, "\n")
	for i, raw := range lines {
		lineNo := i + 1
		s := strings.TrimSpace(raw)
		if s == "" || strings.HasPrefix(s, "#") {
			// preserve the raw line in normalized as-is for UI display
			continue
		}
		// CIDR
		if strings.Contains(s, "/") {
			_, ipnet, err := net.ParseCIDR(s)
			if err != nil {
				res.Errors = append(res.Errors, ParseError{Line: lineNo, Msg: "invalid CIDR"})
				continue
			}
			// ensure IPv4
			if ipnet.IP.To4() == nil {
				res.Errors = append(res.Errors, ParseError{Line: lineNo, Msg: "IPv6 not supported"})
				continue
			}
			cnt := expandCIDRCount(ipnet)
			if res.Count+cnt > maxAddrs {
				return res, fmt.Errorf("line %d: expansion would produce %d addresses (over max %d)", lineNo, res.Count+cnt, maxAddrs)
			}
			// iterate addresses
			start := ipToUint32(ipnet.IP.Mask(ipnet.Mask))
			ones, bits := ipnet.Mask.Size()
			total := 1 << uint(bits-ones)
			for j := 0; j < total; j++ {
				ip := uint32ToIP(start + uint32(j)).String()
				if _, ok := seen[ip]; !ok {
					res.IPs = append(res.IPs, ip)
					seen[ip] = struct{}{}
					res.Count++
				}
			}
			res.Normalized = append(res.Normalized, s)
			continue
		}
		// dash range
		if strings.Contains(s, "-") {
			parts := strings.SplitN(s, "-", 2)
			left := strings.TrimSpace(parts[0])
			right := strings.TrimSpace(parts[1])
			lip := net.ParseIP(left)
			if lip == nil || lip.To4() == nil {
				res.Errors = append(res.Errors, ParseError{Line: lineNo, Msg: "left side must be a full IPv4 address"})
				continue
			}
			// if right is full IP
			rip := net.ParseIP(right)
			var endIp net.IP
			if rip != nil && rip.To4() != nil {
				endIp = rip.To4()
			} else {
				// shorthand: right supplies last N octets
				// split both
				lparts := strings.Split(left, ".")
				rparts := strings.Split(right, ".")
				if len(lparts) != 4 || len(rparts) < 1 || len(rparts) > 3 {
					res.Errors = append(res.Errors, ParseError{Line: lineNo, Msg: "invalid shorthand range"})
					continue
				}
				// build end octets by taking first (4-len(rparts)) from left and append rparts
				endOctets := make([]string, 0, 4)
				copyOctets := 4 - len(rparts)
				for k := 0; k < copyOctets; k++ {
					endOctets = append(endOctets, lparts[k])
				}
				endOctets = append(endOctets, rparts...)
				endStr := strings.Join(endOctets, ".")
				if strings.Contains(endStr, "x") || strings.Contains(endStr, "*") {
					res.Errors = append(res.Errors, ParseError{Line: lineNo, Msg: "wildcard not allowed in shorthand right side"})
					continue
				}
				rip2 := net.ParseIP(endStr)
				if rip2 == nil || rip2.To4() == nil {
					res.Errors = append(res.Errors, ParseError{Line: lineNo, Msg: "invalid end IP after shorthand expansion"})
					continue
				}
				endIp = rip2.To4()
			}
			startVal := ipToUint32(lip.To4())
			endVal := ipToUint32(endIp)
			if endVal < startVal {
				res.Errors = append(res.Errors, ParseError{Line: lineNo, Msg: "end address is before start address"})
				continue
			}
			cnt := int(endVal - startVal + 1)
			if res.Count+cnt > maxAddrs {
				return res, fmt.Errorf("line %d: expansion would produce %d addresses (over max %d)", lineNo, res.Count+cnt, maxAddrs)
			}
			for v := startVal; v <= endVal; v++ {
				ip := uint32ToIP(v).String()
				if _, ok := seen[ip]; !ok {
					res.IPs = append(res.IPs, ip)
					seen[ip] = struct{}{}
					res.Count++
				}
			}
			res.Normalized = append(res.Normalized, fmt.Sprintf("%s-%s", left, endIp.String()))
			continue
		}
		// wildcard last octet
		if strings.HasSuffix(s, ".x") || strings.HasSuffix(s, ".*") {
			base := strings.TrimSuffix(strings.TrimSuffix(s, ".x"), ".*")
			// must be first three octets
			parts := strings.Split(base, ".")
			if len(parts) != 3 {
				res.Errors = append(res.Errors, ParseError{Line: lineNo, Msg: "wildcard allowed only on last octet like 192.168.1.x"})
				continue
			}
			prefix := base + "."
			// expand 0..255
			if res.Count+256 > maxAddrs {
				return res, fmt.Errorf("line %d: expansion would exceed max %d", lineNo, maxAddrs)
			}
			for j := 0; j < 256; j++ {
				ip := fmt.Sprintf("%s%d", prefix, j)
				if _, ok := seen[ip]; !ok {
					res.IPs = append(res.IPs, ip)
					seen[ip] = struct{}{}
					res.Count++
				}
			}
			res.Normalized = append(res.Normalized, s)
			continue
		}
		// single ip
		sip := net.ParseIP(s)
		if sip == nil || sip.To4() == nil {
			res.Errors = append(res.Errors, ParseError{Line: lineNo, Msg: "unrecognized format or invalid IPv4"})
			continue
		}
		ipstr := sip.To4().String()
		if _, ok := seen[ipstr]; !ok {
			if res.Count+1 > maxAddrs {
				return res, fmt.Errorf("line %d: expansion would exceed max %d", lineNo, maxAddrs)
			}
			res.IPs = append(res.IPs, ipstr)
			seen[ipstr] = struct{}{}
			res.Count++
		}
		res.Normalized = append(res.Normalized, ipstr)
	}
	return res, nil
}

// SaveConfig persists config to config.json in the current directory
func SaveConfig(cfg interface{}) error {
	fpath := "config.json"
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := fpath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, fpath)
}

// LoadConfig loads config.json into the provided pointer if it exists
func LoadConfig(cfg interface{}) error {
	fpath := "config.json"
	if _, err := os.Stat(fpath); os.IsNotExist(err) {
		return nil
	}
	b, err := os.ReadFile(fpath)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, cfg)
}
