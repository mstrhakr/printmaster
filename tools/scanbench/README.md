ScanBench - CIDR / Scan sizing helper

This small tool helps estimate how many IPv4 addresses a CIDR contains, the approximate memory cost to store them all as strings, and perform a lightweight sampling simulation.

Usage (from repository root):

# Count addresses only (fast)
go run tools/scanbench/main.go -cidr 10.2.0.0/16 -mode=count

# Simulate sampling 1000 addresses across the range (safe)
go run tools/scanbench/main.go -cidr 10.2.0.0/16 -mode=simulate -sample=1000

# Attempt a full iteration (dangerous for large ranges). Limit is 1_000_000 addresses unless --force is used.
go run tools/scanbench/main.go -cidr 10.2.0.0/16 -mode=simulate --max-alloc=2000000 --force

Notes and recommendations
- The default behaviour is conservative: the tool will refuse to fully iterate very large ranges unless you increase --max-alloc and pass --force.
- Storing all addresses as strings is memory-heavy. The tool shows an approximate bytes estimate (very rough).
- Scanning a /16 (65536 addresses) may be feasible depending on your probe latency and concurrency. Use a probe benchmark (real SNMP/TCP probes) to estimate throughput. If your per-probe latency is ~50ms and you run 100 concurrent workers, you'll get approx 2000 probes/sec and finish ~32s for 65k addresses. Adjust expectations accordingly.
- A /8 (16,777,216 addresses) is generally impractical to scan exhaustively without special infrastructure, heavy parallelism, and careful network planning.

Suggested next steps
- Use this tool to estimate counts and memory.
- Create a small probe benchmark that runs the real scanner probe (TCP or SNMP) against a local lab farm to measure real per-probe latency and concurrency behaviour.
- If you need to support larger CIDR expansions in production, consider:
  - Keeping a default guard (like the existing 10k) and making it configurable in UI/config with clear warning.
  - Adding a two-stage discovery: fast liveness (ICMP/TCP port probe) then deep SNMP scans only for alive hosts.
  - Breaking large ranges into smaller jobs and rate-limiting / concurrency controls.
