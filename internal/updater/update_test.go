package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func archive(t *testing.T, zipped bool) []byte {
	t.Helper()
	var b bytes.Buffer
	data := []byte("binary fixture")
	if zipped {
		w := zip.NewWriter(&b)
		f, _ := w.Create("ranjit.exe")
		f.Write(data)
		w.Close()
	} else {
		gz := gzip.NewWriter(&b)
		w := tar.NewWriter(gz)
		w.WriteHeader(&tar.Header{Name: "ranjit", Mode: 0755, Size: int64(len(data))})
		w.Write(data)
		w.Close()
		gz.Close()
	}
	return b.Bytes()
}
func TestUpdateVerifiesBeforeChangingExecutable(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "ranjit")
	os.WriteFile(target, []byte("old"), 0755)
	data := archive(t, runtime.GOOS == "windows")
	bad := false
	client := &Client{HTTP: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		body := data
		if strings.HasSuffix(r.URL.Path, "checksums.txt") {
			ext := ".tar.gz"
			if runtime.GOOS == "windows" {
				ext = ".zip"
			}
			hash := sha256.Sum256(data)
			if bad {
				hash = [32]byte{}
			}
			body = fmt.Appendf(nil, "%x  ranjit_v0.2.0_%s_%s%s\n", hash, runtime.GOOS, runtime.GOARCH, ext)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	bad = true
	if _, err := client.Stage(t.Context(), "v0.2.0", target); err == nil {
		t.Fatal("accepted corrupt download")
	}
	before, _ := os.ReadFile(target)
	if string(before) != "old" {
		t.Fatal("failed verification changed executable")
	}
	bad = false
	stage, err := client.Stage(t.Context(), "v0.2.0", target)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := Install(stage, target)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := os.ReadFile(previous)
	current, _ := os.ReadFile(target)
	if string(old) != "old" || string(current) != "binary fixture" {
		t.Fatal("update or rollback copy damaged")
	}
}
func TestArchivesAndVersions(t *testing.T) {
	for _, zipped := range []bool{false, true} {
		name := "ranjit"
		if zipped {
			name += ".exe"
		}
		got, err := extract(archive(t, zipped), name, zipped)
		if err != nil || string(got) != "binary fixture" {
			t.Fatal(err)
		}
		if _, err = extract(archive(t, zipped), "../ranjit", zipped); err == nil {
			t.Fatal("extracted unrelated file")
		}
	}
	if !Newer("v0.10.0", "v0.2.0") || Newer("v0.1.0", "v0.2.0") || Newer("v1.0.0-beta", "v0.2.0") || Newer("v1.0.0", "dev") {
		t.Fatal("version ordering")
	}
	c := New()
	req, _ := http.NewRequest("GET", "http://github.com/file", nil)
	if c.HTTP.CheckRedirect(req, nil) == nil {
		t.Fatal("HTTPS downgrade")
	}
	req, _ = http.NewRequest("GET", "https://evil.example/file", nil)
	if c.HTTP.CheckRedirect(req, nil) == nil {
		t.Fatal("untrusted redirect")
	}
}

func TestReplaceRunningExecutable(t *testing.T) {
	if os.Getenv("RANJIT_TEST_UPDATE_HELPER") == "1" {
		target, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		stage := filepath.Join(filepath.Dir(target), "staged")
		if err = os.WriteFile(stage, []byte("replacement"), 0755); err != nil {
			t.Fatal(err)
		}
		if _, err = Install(stage, target); err != nil {
			t.Fatal(err)
		}
		return
	}
	original, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "runner.exe")
	if err = os.WriteFile(target, data, 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), target, "-test.run=^TestReplaceRunningExecutable$")
	cmd.Env = append(os.Environ(), "RANJIT_TEST_UPDATE_HELPER=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("replace active executable: %v %s", err, output)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "replacement" {
		t.Fatal("replacement missing", err)
	}
}
