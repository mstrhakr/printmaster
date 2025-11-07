module printmaster/agent

go 1.24.0

require (
	github.com/gosnmp/gosnmp v1.42.1
	github.com/grandcat/zeroconf v1.0.0
	github.com/kardianos/service v1.2.4
	modernc.org/sqlite v1.39.1
	printmaster/common v0.0.0
)

replace printmaster/common => ../common

require (
	github.com/BurntSushi/toml v1.5.0 // indirect
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/miekg/dns v1.1.27 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/crypto v0.43.0 // indirect
	golang.org/x/exp v0.0.0-20250620022241-b7579e27df2b // indirect
	golang.org/x/net v0.46.0 // indirect
	golang.org/x/sys v0.37.0 // indirect
	modernc.org/libc v1.66.10 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)
