package autostart

import (
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func TestServicePathsEscapingAndUserScope(t *testing.T) {
	for _, platform := range []string{"linux", "darwin", "windows"} {
		p, err := Build(platform, "/Users/a b", "/Users/a b/a & b%/config.json", "/Users/a b/ranjit", "1000")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(p.Content, "api_key") || strings.Contains(p.Content, "root") {
			t.Fatal("service has credentials or elevated user")
		}
		if platform == "linux" {
			if !strings.Contains(p.Content, "b%%/config.json") || p.Install[0][1] != "--user" {
				t.Fatal(p.Content)
			}
		} else {
			decoder := xml.NewDecoder(strings.NewReader(p.Content))
			for {
				_, err := decoder.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal("invalid service XML", err)
				}
			}
			if !strings.Contains(p.Content, "&amp;") {
				t.Fatal("unescaped path")
			}
		}
		another, _ := Build(platform, "/Users/a b", "/Users/a b/other.json", "/Users/a b/ranjit", "1000")
		if another.ID == p.ID {
			t.Fatal("services collided")
		}
	}
	if _, err := Build("linux", "/tmp", "/tmp/a\nExecStart=bad", "/tmp/ranjit", "1000"); err == nil {
		t.Fatal("service line injection")
	}
	if got := windowsArg(`C:\a b\`); got != `"C:\a b\\"` {
		t.Fatal("Windows trailing slash", got)
	}
}
