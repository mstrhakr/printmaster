package agent

import (
	"io"
	"sync/atomic"
)

// DownloadProgressCallback is called to report download progress.
// percent is 0-100, bytesRead is total bytes downloaded so far.
type DownloadProgressCallback func(percent int, bytesRead int64)

// progressReader wraps an io.Reader and calls a callback to report progress.
type progressReader struct {
	reader      io.Reader
	totalSize   int64
	bytesRead   int64
	callback    DownloadProgressCallback
	lastPercent int32 // atomic to avoid duplicate callbacks for same percentage
}

// newProgressReader creates a new progress reader wrapper.
func newProgressReader(reader io.Reader, totalSize int64, callback DownloadProgressCallback) *progressReader {
	return &progressReader{
		reader:      reader,
		totalSize:   totalSize,
		callback:    callback,
		lastPercent: -1,
	}
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	if n > 0 {
		pr.bytesRead += int64(n)
		if pr.callback != nil && pr.totalSize > 0 {
			percent := int((pr.bytesRead * 100) / pr.totalSize)
			if percent > 100 {
				percent = 100
			}
			// Only call callback when percentage actually changes
			lastPct := atomic.LoadInt32(&pr.lastPercent)
			if int32(percent) > lastPct {
				atomic.StoreInt32(&pr.lastPercent, int32(percent))
				pr.callback(percent, pr.bytesRead)
			}
		}
	}
	return n, err
}
