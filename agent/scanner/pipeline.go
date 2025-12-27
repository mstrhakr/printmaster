package scanner

import (
	"context"
	"math/rand/v2"
	"net"
	"strconv"
	"sync"
	"time"
)

// ScanJob describes a single IP scan request.
type ScanJob struct {
	IP     string
	Source string
	Meta   interface{}
}

// LivenessResult carries the outcome of a liveness probe.
type LivenessResult struct {
	Job       ScanJob
	Alive     bool
	OpenPorts []int
	Err       error
}

// ScannerConfig controls worker counts and timeouts.
type ScannerConfig struct {
	LivenessWorkers int
	LivenessTimeout time.Duration
	LivenessPorts   []int
	// DetectionWorkers controls number of detection-stage workers.
	DetectionWorkers int
	// DetectFunc is an optional override used by detection workers. It should
	// perform any compact SNMP GETs or other checks and return a parsed
	// opaque result, a boolean indicating whether the host appears to be a
	// printer, and an optional error. If nil, detection workers will simply
	// mark hosts with open ports as not-determined (no-op).
	DetectFunc func(ctx context.Context, job ScanJob, openPorts []int) (interface{}, bool, error)
	// Optional probe function override (useful for tests). If nil, the
	// default tcpProbe is used.
	ProbeFunc func(ip string, ports []int, timeout time.Duration) ([]int, error)
	// DeepScanWorkers controls number of deep-scan workers.
	DeepScanWorkers int
	// DeepScanFunc is invoked for confirmed printers. It receives the
	// DetectionResult and should perform the heavy work (full walks / imports).
	// It returns an opaque result (e.g. a PrinterInfo) and an optional error.
	DeepScanFunc func(ctx context.Context, dr DetectionResult) (interface{}, error)
}

// DetectionResult carries the outcome of the detection stage. The Info field
// holds an opaque parsed result (e.g. a PrinterInfo) when IsPrinter is true.
type DetectionResult struct {
	Job       ScanJob
	IsPrinter bool
	Info      interface{}
	Err       error
}

// StartLivenessPool starts a pool of workers that consume ScanJob from jobs
// and emit LivenessResult to the returned channel. The returned channel is
// closed when all workers exit (jobs closed and drained) or ctx is done.
func StartLivenessPool(ctx context.Context, cfg ScannerConfig, jobs <-chan ScanJob) <-chan LivenessResult {
	out := make(chan LivenessResult)
	if cfg.LivenessWorkers <= 0 {
		cfg.LivenessWorkers = 10
	}
	if cfg.LivenessTimeout <= 0 {
		cfg.LivenessTimeout = 500 * time.Millisecond
	}
	if len(cfg.LivenessPorts) == 0 {
		cfg.LivenessPorts = []int{80, 443, 9100}
	}
	probe := cfg.ProbeFunc
	if probe == nil {
		probe = tcpProbe
	}

	var wg sync.WaitGroup
	wg.Add(cfg.LivenessWorkers)
	for i := 0; i < cfg.LivenessWorkers; i++ {
		go func() {
			defer wg.Done()
			// staggered startup to avoid thundering herd
			jitterMs := 100 + rand.IntN(151)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(jitterMs) * time.Millisecond):
			}
			for {
				select {
				case <-ctx.Done():
					return
				case j, ok := <-jobs:
					if !ok {
						return
					}
					open, err := probe(j.IP, cfg.LivenessPorts, cfg.LivenessTimeout)
					res := LivenessResult{Job: j}
					if err == nil && len(open) > 0 {
						res.Alive = true
						res.OpenPorts = open
					} else {
						res.Alive = false
						res.Err = err
					}
					select {
					case <-ctx.Done():
						return
					case out <- res:
					}
				}
			}
		}()
	}

	// close output when workers finish
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

// tcpProbe is the default implementation used by liveness workers.
func tcpProbe(ip string, ports []int, timeout time.Duration) ([]int, error) {
	open := []int{}
	for _, p := range ports {
		addr := net.JoinHostPort(ip, strconv.Itoa(p))
		conn, err := net.DialTimeout("tcp", addr, timeout)
		if err != nil {
			continue
		}
		conn.Close()
		open = append(open, p)
	}
	return open, nil
}

// StartDetectionPool starts a pool of detection workers that consume
// LivenessResult from the in channel and emit DetectionResult to the
// returned channel. DetectionFunc (from cfg) is used to perform compact
// SNMP probes and parsing; it's expected to be supplied by the caller for
// real detection logic. The returned channel is closed when all workers
// exit (in closed and drained) or ctx is done.
func StartDetectionPool(ctx context.Context, cfg ScannerConfig, in <-chan LivenessResult) <-chan DetectionResult {
	out := make(chan DetectionResult)
	if cfg.DetectionWorkers <= 0 {
		cfg.DetectionWorkers = 5
	}
	detect := cfg.DetectFunc
	var wg sync.WaitGroup
	wg.Add(cfg.DetectionWorkers)
	for i := 0; i < cfg.DetectionWorkers; i++ {
		go func() {
			defer wg.Done()
			// staggered startup
			jitterMs := 50 + rand.IntN(201)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(jitterMs) * time.Millisecond):
			}
			for {
				select {
				case <-ctx.Done():
					return
				case lr, ok := <-in:
					if !ok {
						return
					}
					// default: if no detect func provided, forward negative result
					if detect == nil {
						dr := DetectionResult{Job: lr.Job, IsPrinter: false}
						select {
						case <-ctx.Done():
							return
						case out <- dr:
						}
						continue
					}
					info, okp, err := detect(ctx, lr.Job, lr.OpenPorts)
					dr := DetectionResult{Job: lr.Job, IsPrinter: okp, Info: info, Err: err}
					select {
					case <-ctx.Done():
						return
					case out <- dr:
					}
				}
			}
		}()
	}

	// close output when workers finish
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

// StartDeepScanPool starts a pool of workers that consume DetectionResult
// from the in channel and invoke cfg.DeepScanFunc for items where
// DetectionResult.IsPrinter is true. The returned channel emits the opaque
// results returned by DeepScanFunc (nil results are skipped). The returned
// channel is closed when workers exit (in closed and drained) or ctx is done.
func StartDeepScanPool(ctx context.Context, cfg ScannerConfig, in <-chan DetectionResult) <-chan interface{} {
	out := make(chan interface{})
	if cfg.DeepScanWorkers <= 0 {
		cfg.DeepScanWorkers = 5
	}
	deepFn := cfg.DeepScanFunc
	var wg sync.WaitGroup
	wg.Add(cfg.DeepScanWorkers)
	for i := 0; i < cfg.DeepScanWorkers; i++ {
		go func() {
			defer wg.Done()
			// staggered startup
			jitterMs := 50 + rand.IntN(201)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(jitterMs) * time.Millisecond):
			}
			for {
				select {
				case <-ctx.Done():
					return
				case dr, ok := <-in:
					if !ok {
						return
					}
					if !dr.IsPrinter {
						continue
					}
					if deepFn == nil {
						// nothing to do; skip
						continue
					}
					res, err := deepFn(ctx, dr)
					if err != nil {
						// best-effort: attach error as result if non-nil so caller
						// can log or inspect; otherwise skip
						select {
						case <-ctx.Done():
							return
						case out <- err:
						}
						continue
					}
					if res == nil {
						continue
					}
					select {
					case <-ctx.Done():
						return
					case out <- res:
					}
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}
