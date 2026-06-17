package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// releaseBinary describes a prebuilt binary published as a release archive
// (tar.gz or zip) alongside a checksums.txt. installReleaseBinary downloads,
// verifies, extracts, and installs it natively in Go — no bash/curl/tar/unzip
// process required, so it works identically on Linux, macOS, and Windows.
type releaseBinary struct {
	archiveURL  string   // full URL to the .tar.gz/.zip
	checksumURL string   // full URL to checksums.txt
	archiveName string   // archive filename as listed in checksums.txt
	destPath    string   // where to install the extracted binary
	innerNames  []string // acceptable basenames of the binary inside the archive
	isZip       bool
}

// fetchBytes downloads a URL into memory. HTTPS-only (defense in depth, since
// these bytes become executables).
func fetchBytes(url string) ([]byte, error) {
	if !strings.HasPrefix(strings.ToLower(url), "https://") {
		return nil, fmt.Errorf("refusing non-HTTPS URL: %s", url)
	}
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

func installReleaseBinary(rb releaseBinary) error {
	archive, err := fetchBytes(rb.archiveURL)
	if err != nil {
		return fmt.Errorf("download %s: %w", rb.archiveName, err)
	}

	// Checksum verification (fail closed): refuse to install an unverified
	// binary, matching the upstream installers and closing the MITM/supply
	// chain gap.
	sums, err := fetchBytes(rb.checksumURL)
	if err != nil {
		return fmt.Errorf("download checksums.txt: %w", err)
	}
	expected := expectedSHA256(sums, rb.archiveName)
	if expected == "" {
		return fmt.Errorf("checksum for %s not found in checksums.txt", rb.archiveName)
	}
	actual := hex.EncodeToString(sha256Sum(archive))
	if !strings.EqualFold(expected, actual) {
		return fmt.Errorf("checksum mismatch for %s (expected %s, got %s)", rb.archiveName, expected, actual)
	}

	var binData []byte
	if rb.isZip {
		binData, err = extractBinaryFromZip(archive, rb.innerNames)
	} else {
		binData, err = extractBinaryFromTarGz(archive, rb.innerNames)
	}
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(rb.destPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(rb.destPath, binData, 0755); err != nil {
		return fmt.Errorf("write %s: %w", rb.destPath, err)
	}
	return nil
}

func sha256Sum(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

// expectedSHA256 finds the hash for asset in a "checksums.txt" body whose lines
// look like "<hex>  <filename>" (filename may be prefixed with '*').
func expectedSHA256(checksums []byte, asset string) string {
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if name == asset || filepath.Base(name) == asset {
			return strings.ToLower(fields[0])
		}
	}
	return ""
}

func matchesInner(name string, candidates []string) bool {
	base := filepath.Base(name)
	for _, c := range candidates {
		if base == c {
			return true
		}
	}
	return false
}

func extractBinaryFromZip(data []byte, innerNames []string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !matchesInner(f.Name, innerNames) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(io.LimitReader(rc, 512<<20)) // 512MB cap
	}
	return nil, fmt.Errorf("binary %v not found in zip archive", innerNames)
}

func extractBinaryFromTarGz(data []byte, innerNames []string) ([]byte, error) {
	gzr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open gzip: %w", err)
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || !matchesInner(hdr.Name, innerNames) {
			continue
		}
		return io.ReadAll(io.LimitReader(tr, 512<<20))
	}
	return nil, fmt.Errorf("binary %v not found in tar.gz archive", innerNames)
}
