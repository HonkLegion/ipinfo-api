package dataset

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func Download(ctx context.Context, url, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: %s", resp.Status)
	}

	var reader io.Reader = resp.Body

	// automaticky rozbal .gz (podle URL nebo Content-Type)
	if strings.HasSuffix(url, ".gz") ||
		strings.Contains(resp.Header.Get("Content-Type"), "gzip") {

		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer gz.Close()

		reader = gz
	}

	return io.ReadAll(reader)
}
