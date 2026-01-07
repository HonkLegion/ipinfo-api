package dataset

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func DownloadToFile(ctx context.Context, url, token, target string) error {
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		return fmt.Errorf("cache_file points to a directory, must be a file: %s", target)
	}

	dir := filepath.Dir(target)

	if dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
	}

	tmp := target + ".tmp"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer out.Close()

	var reader io.Reader = resp.Body

	if strings.HasSuffix(url, ".gz") ||
		strings.Contains(resp.Header.Get("Content-Type"), "gzip") {

		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return err
		}
		defer gz.Close()
		reader = gz
	}

	if _, err := io.Copy(out, reader); err != nil {
		return err
	}

	if err := out.Sync(); err != nil {
		return err
	}

	return os.Rename(tmp, target)
}
