# Range syntax and parsing rules

This file documents what the server accepts in the range editor textarea and how the `ParseRangeText` behavior works.

Supported formats
- Single IPv4 address: e.g. `192.168.1.42`
- CIDR notation: e.g. `192.168.1.0/24`
- Full range (start-end): e.g. `192.168.1.10-192.168.1.20`
- Short-hand range (right side maps to last N octets):
  - `192.168.1.10-20` expands to `192.168.1.10` through `192.168.1.20`.
  - `10-20` is interpreted relative to the default/prepended subnet only when configured in the UI as Add/Override behavior.
- Last-octet wildcard: `192.168.1.x` or `192.168.1.*` (expands the last octet 0..255)

Behaviors and constraints
- Only IPv4 is supported by the parser. IPv6 lines are ignored with an error.
- The parser deduplicates addresses and enforces a default expansion limit of 4096 addresses to avoid accidental large scans.
- Empty and comment lines starting with `#` are ignored.

Error reporting
- The server returns a parse error which includes the offending line and a short message. The UI shows an alert for parse failures.

Persistence
- Saved ranges are persisted to `config.json` in the agent's working directory using `SaveConfig`/`LoadConfig` helpers.
