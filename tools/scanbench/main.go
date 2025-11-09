package main

import (
    "flag"
    "fmt"
    "math"
    "net"
    "os"
    "runtime"
    "time"
)

func humanBytes(n uint64) string {
    if n == 0 {
        return "0B"
    }
    const unit = 1024
    if n < unit {
        return fmt.Sprintf("%dB", n)
    }
    div, exp := uint64(unit), 0
    for e := n / unit; e >= unit; e /= unit {
        div *= unit
        exp++
    }
    prefixes := "KMGTPE"
    return fmt.Sprintf("%.1f%ciB", float64(n)/float64(div), prefixes[exp])
}

// estimateBytesPerIP is a conservative estimate of memory usage per IP string
// when stored in a slice of strings. This is only an estimate for guidance.
const estimateBytesPerIP = 32

func cidrCount(ipnet *net.IPNet) (uint64, error) {
    ones, bits := ipnet.Mask.Size()
    hostBits := bits - ones
    if hostBits >= 63 { // avoid shifting by >=64
        return math.MaxUint64, nil
    }
    if hostBits >= 31 {
        // >2^30 addresses - treat as huge
        return 1 << 30, nil
    }
    return 1 << uint(hostBits), nil
}

func main() {
    cidr := flag.String("cidr", "", "CIDR to analyze (e.g. 10.2.0.0/16)")
    mode := flag.String("mode", "count", "mode: count|simulate")
    sample := flag.Int("sample", 1000, "when simulate: how many samples to process evenly across the range")
    maxAlloc := flag.Uint64("max-alloc", 1_000_000, "max addresses allowed to fully allocate (unsafe beyond this)")
    force := flag.Bool("force", false, "if set, allow allocations beyond max-alloc")
    flag.Parse()

    if *cidr == "" {
        fmt.Fprintln(os.Stderr, "error: -cidr is required")
        flag.Usage()
        os.Exit(2)
    }

    _, ipnet, err := net.ParseCIDR(*cidr)
    if err != nil {
        fmt.Fprintf(os.Stderr, "invalid CIDR: %v\n", err)
        os.Exit(2)
    }

    cnt, err := cidrCount(ipnet)
    if err != nil {
        fmt.Fprintf(os.Stderr, "failed to count addresses: %v\n", err)
        os.Exit(2)
    }

    fmt.Printf("CIDR: %s\n", *cidr)
    fmt.Printf("Estimated addresses: %d\n", cnt)

    estBytes := uint64(cnt) * estimateBytesPerIP
    fmt.Printf("Estimated memory to store all addresses (approx): %s\n", humanBytes(estBytes))

    if *mode == "count" {
        fmt.Printf("Mode=count: done.\n")
        return
    }

    // simulate mode
    fmt.Printf("Mode=simulate: sample=%d, max-alloc=%d, force=%v\n", *sample, *maxAlloc, *force)

    if cnt > *maxAlloc && !*force {
        fmt.Printf("Address count %d exceeds max-alloc %d. Use --force to override. Aborting.\n", cnt, *maxAlloc)
        os.Exit(1)
    }

    // If fully allocating is requested and safe-ish, attempt to allocate and iterate.
    start := time.Now()
    // compute start uint32 and total (as uint64)
    ip4 := ipnet.IP.To4()
    if ip4 == nil {
        fmt.Fprintln(os.Stderr, "only IPv4 supported")
        os.Exit(2)
    }
    startVal := uint32(ip4[0])<<24 | uint32(ip4[1])<<16 | uint32(ip4[2])<<8 | uint32(ip4[3])

    // if count is reasonable, iterate all. Otherwise perform sampling.
    if cnt <= *maxAlloc || *force {
        // full iteration (may be large)
        processed := uint64(0)
        // If huge, we won't actually store strings; just iterate and touch a counter.
        for i := uint64(0); i < cnt; i++ {
            _ = startVal + uint32(i) // cheap touch
            processed++
            // avoid busy spin in very tight loops so runtime stats are meaningful
            if processed%1000000 == 0 {
                runtime.Gosched()
            }
        }
        dur := time.Since(start)
        fmt.Printf("Iterated %d addresses in %s (no allocation).\n", processed, dur)
        return
    }

    // sampling path: pick `sample` addresses evenly across the range
    if *sample <= 0 {
        fmt.Fprintln(os.Stderr, "sample must be > 0")
        os.Exit(2)
    }
    step := float64(cnt) / float64(*sample)
    processed := 0
    for i := 0; i < *sample; i++ {
        idx := uint64(float64(i) * step)
        if idx >= cnt {
            idx = cnt - 1
        }
        _ = startVal + uint32(idx)
        processed++
        // simulate small work
        time.Sleep(1 * time.Millisecond)
    }
    dur := time.Since(start)
    fmt.Printf("Sampled %d addresses across range in %s (simulated work).\n", processed, dur)
    fmt.Println("Note: this is a lightweight sampling run. For full scan throughput, run a real probe benchmark against a representative target with the scanner's probe code.")
}
