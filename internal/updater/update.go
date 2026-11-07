// Package updater installs only stable release assets from the Ranjit repository.
package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const repository = "https://github.com/getcortexlabs/ranjit"

var stable = regexp.MustCompile(`^v(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})$`)

type Client struct{ HTTP *http.Client }

func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 || req.URL.Scheme != "https" || req.URL.User != nil {
			return errors.New("unsafe release redirect")
		}
		host := req.URL.Hostname()
		if host != "github.com" && host != "release-assets.githubusercontent.com" && host != "objects.githubusercontent.com" {
			return errors.New("unexpected release host")
		}
		return nil
	}}}
}
func (c *Client) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ranjit-updater")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, errors.New("could not download release; check connectivity and try again")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("release download returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("release file exceeds size limit")
	}
	return data, nil
}
func (c *Client) Latest(ctx context.Context) (string, error) {
	data, err := c.get(ctx, "https://api.github.com/repos/getcortexlabs/ranjit/releases/latest", 1<<20)
	if err != nil {
		return "", err
	}
	var r struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if json.Unmarshal(data, &r) != nil || !stable.MatchString(r.Tag) || r.Draft || r.Prerelease {
		return "", errors.New("invalid stable release")
	}
	return r.Tag, nil
}
func Newer(candidate, current string) bool {
	if !stable.MatchString(candidate) || !stable.MatchString(current) {
		return false
	}
	a, b := strings.Split(candidate[1:], "."), strings.Split(current[1:], ".")
	for i := range a {
		x, _ := strconv.Atoi(a[i])
		y, _ := strconv.Atoi(b[i])
		if x != y {
			return x > y
		}
	}
	return false
}

// Stage verifies the archive before extracting its single executable beside target.
func (c *Client) Stage(ctx context.Context, tag, target string) (string, error) {
	if !stable.MatchString(tag) {
		return "", errors.New("invalid version")
	}
	ext, binary := ".tar.gz", "ranjit"
	if runtime.GOOS == "windows" {
		ext, binary = ".zip", "ranjit.exe"
	}
	name := "ranjit_" + tag + "_" + runtime.GOOS + "_" + runtime.GOARCH + ext
	base := repository + "/releases/download/" + tag + "/"
	sums, err := c.get(ctx, base+"checksums.txt", 64<<10)
	if err != nil {
		return "", err
	}
	archive, err := c.get(ctx, base+name, 64<<20)
	if err != nil {
		return "", err
	}
	if err = verify(archive, sums, name); err != nil {
		return "", err
	}
	data, err := extract(archive, binary, ext == ".zip")
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp(filepath.Dir(target), ".ranjit-update-*")
	if err != nil {
		return "", err
	}
	path := f.Name()
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(path)
		}
	}()
	if _, err = f.Write(data); err != nil {
		return "", err
	}
	if err = f.Chmod(0755); err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	ok = true
	return path, nil
}
func verify(data, sums []byte, name string) error {
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[1] == name {
			if want != "" {
				return errors.New("duplicate checksum")
			}
			want = f[0]
		}
	}
	hash := sha256.Sum256(data)
	if want != hex.EncodeToString(hash[:]) {
		return errors.New("release checksum mismatch; installed version unchanged")
	}
	return nil
}
func extract(data []byte, binary string, zipped bool) ([]byte, error) {
	var result []byte
	found := false
	read := func(r io.Reader, size int64) error {
		if found || size < 1 || size > 128<<20 {
			return errors.New("invalid executable in release")
		}
		var err error
		result, err = io.ReadAll(io.LimitReader(r, (128<<20)+1))
		if err != nil {
			return err
		}
		if int64(len(result)) != size {
			return errors.New("incomplete release executable")
		}
		found = true
		return nil
	}
	if zipped {
		z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		for _, f := range z.File {
			if f.Name != binary {
				continue
			}
			if !f.Mode().IsRegular() {
				return nil, errors.New("release executable is not regular")
			}
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			err = read(r, int64(f.UncompressedSize64))
			r.Close()
			if err != nil {
				return nil, err
			}
		}
	} else {
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		tr := tar.NewReader(io.LimitReader(gz, 130<<20))
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if h.Name != binary {
				continue
			}
			if h.Typeflag != tar.TypeReg {
				return nil, errors.New("release executable is not regular")
			}
			if err = read(tr, h.Size); err != nil {
				return nil, err
			}
		}
	}
	if !found {
		return nil, errors.New("release executable missing")
	}
	return result, nil
}

// Install keeps the previous executable; a failed replacement restores it.
func Install(staged, target string) (string, error) {
	info, err := os.Lstat(target)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("installed executable must be a regular file")
	}
	if filepath.Dir(staged) != filepath.Dir(target) {
		return "", errors.New("update must be staged beside executable")
	}
	backup := target + ".previous-" + time.Now().UTC().Format("20060102T150405.000000000")
	if runtime.GOOS == "windows" {
		if err = os.Rename(target, backup); err != nil {
			return "", err
		}
		if err = os.Rename(staged, target); err != nil {
			return "", errors.Join(err, os.Rename(backup, target))
		}
	} else {
		if err = os.Link(target, backup); err != nil {
			return "", err
		}
		if err = os.Rename(staged, target); err != nil {
			_ = os.Remove(backup)
			return "", err
		}
	}
	return backup, nil
}
