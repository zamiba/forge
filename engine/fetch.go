package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// DefaultHTTPClient is used when Options.HTTPClient is nil. The timeout is
// generous because build steps routinely fetch multi-gigabyte source archives
// and game assets over slow links.
var DefaultHTTPClient = &http.Client{Timeout: 30 * time.Minute}

// fetch downloads url to dest, creating parent directories, and reports
// progress as it goes. The download is written to a temporary file and renamed
// into place so an interrupted fetch cannot leave a truncated file that a later
// run would mistake for a complete one.
func fetch(ctx context.Context, client *http.Client, url, dest string, onPct func(int)) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, url)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dest), ".forge-fetch-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpName) // no-op once the rename below has succeeded
	}()

	src := io.Reader(resp.Body)
	if onPct != nil {
		src = &progressReader{r: resp.Body, total: resp.ContentLength, onPct: onPct}
	}
	if _, err := io.Copy(tmp, src); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, dest)
}

// progressReader reports whole-percent progress as it is read through.
type progressReader struct {
	r       io.Reader
	total   int64
	read    int64
	onPct   func(int)
	lastPct int
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	pr.read += int64(n)
	if pr.total > 0 {
		pct := int(pr.read * 100 / pr.total)
		if pct != pr.lastPct {
			pr.lastPct = pct
			pr.onPct(pct)
		}
	}
	return n, err
}
