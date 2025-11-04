package scanner

import (
	"context"
	"fmt"
)

// EnumerateIPs takes a range specification and emits ScanJob to the returned channel.
// The rangeText format supports:
//   - Single IPs: "192.168.1.1"
//   - CIDR: "192.168.1.0/24"
//   - Ranges: "192.168.1.1-254" or "192.168.1.1-192.168.1.254"
//   - Wildcards: "192.168.1.*" or "192.168.1.x"
//
// The parseFunc should parse the rangeText and return a list of IP addresses.
// This allows the scanner package to remain decoupled from the agent package.
func EnumerateIPs(ctx context.Context, rangeText string, source string, parseFunc func(text string, maxAddrs int) ([]string, error), maxAddrs int) <-chan ScanJob {
	jobs := make(chan ScanJob)

	go func() {
		defer close(jobs)

		if parseFunc == nil {
			return
		}

		ips, err := parseFunc(rangeText, maxAddrs)
		if err != nil {
			// Error during parsing - just close the channel
			// Caller should handle parse errors separately before calling EnumerateIPs
			return
		}

		for _, ip := range ips {
			select {
			case <-ctx.Done():
				return
			case jobs <- ScanJob{
				IP:     ip,
				Source: source,
				Meta:   nil,
			}:
			}
		}
	}()

	return jobs
}

// ParseResult represents the result of parsing a range specification.
// This mirrors agent.ParseResult to avoid direct dependency.
type ParseResult struct {
	IPs        []string
	Count      int
	Errors     []ParseError
	Normalized []string
}

// ParseError represents an error parsing a specific line.
type ParseError struct {
	Line int
	Msg  string
}

// ParseRangeAdapter adapts agent.ParseRangeText to return just the IP list.
// This function should be provided by the caller to avoid import cycles.
type ParseRangeAdapter func(text string, maxAddrs int) (*ParseResult, error)

// AdaptParseFunc converts a ParseRangeAdapter to the simpler parseFunc signature.
func AdaptParseFunc(adapter ParseRangeAdapter) func(string, int) ([]string, error) {
	return func(text string, maxAddrs int) ([]string, error) {
		result, err := adapter(text, maxAddrs)
		if err != nil {
			return nil, err
		}
		if result == nil {
			return nil, fmt.Errorf("nil parse result")
		}
		return result.IPs, nil
	}
}
