package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/getcortexlabs/ranjit/internal/config"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func checkLocalUI(origin string) error {
	u, err := url.Parse(origin)
	if err != nil || (u.Hostname() != "localhost" && !net.ParseIP(u.Hostname()).IsLoopback()) {
		return errors.New("o painel requer base_url em localhost ou IP de loopback")
	}
	return nil
}
func dashboardLink(cfg config.Config) (string, error) {
	if err := checkLocalUI(cfg.BaseURL); err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, cfg.BaseURL+"/ui/link", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("o servidor local ainda não está disponível")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", errors.New("servidor não aceitou a chave local; confira RANJIT_CONFIG e a porta")
	}
	var out struct {
		Path string `json:"path"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&out); err != nil {
		return "", err
	}
	if !strings.HasPrefix(out.Path, "/app/#") || len(out.Path) > 128 {
		return "", errors.New("resposta inesperada do painel")
	}
	return cfg.BaseURL + out.Path, nil
}
func showDashboard(link string, noBrowser bool) error {
	fmt.Println("Painel local (link de uso único, válido por 1 minuto):", link)
	if noBrowser {
		return nil
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", link)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", link)
	default:
		cmd = exec.Command("xdg-open", link)
	}
	return cmd.Run()
}
