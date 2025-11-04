package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/grandcat/zeroconf"
)

// StartMDNSBrowser starts mDNS/DNS-SD browsing for common printer service types
// and invokes enqueue for each discovered IPv4 address. It runs until the
// context is canceled. The caller is responsible for de-duplicating IPs.
func StartMDNSBrowser(ctx context.Context, enqueue func(string) bool) {
	svcTypes := []string{"_ipp._tcp", "_ipps._tcp", "_printer._tcp"}
	for _, st := range svcTypes {
		st := st
		go func() {
			resolver, err := zeroconf.NewResolver(nil)
			if err != nil {
				Info("mDNS resolver error: " + err.Error())
				return
			}
			entries := make(chan *zeroconf.ServiceEntry)
			// consume entries
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					case e, ok := <-entries:
						if !ok {
							return
						}
						for _, ip := range e.AddrIPv4 {
							_ = enqueue(ip.String())
						}
					}
				}
			}()
			Info(fmt.Sprintf("mDNS browse start: %s", st))
			// zeroconf.Browse will run until ctx is done and closes the entries channel
			if err := resolver.Browse(ctx, st, "local.", entries); err != nil {
				Info("mDNS browse error: " + err.Error())
			}
			// Browse has completed and closed the channel, wait for consumer to finish
			time.Sleep(100 * time.Millisecond)
		}()
	}
}
