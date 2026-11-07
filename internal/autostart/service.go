// Package autostart installs a service for the current user, without elevation.
package autostart

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Plan struct {
	ID, Path, Content                       string
	Install, Start, Stop, Status, Uninstall [][]string
}

func escaped(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func quoted(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`).Replace(s) + `"`
}
func windowsArg(s string) string {
	// CommandLineToArgvW quoting, including backslashes before a closing quote.
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for _, r := range s {
		if r == '\\' {
			slashes++
			continue
		}
		if r == '"' {
			b.WriteString(strings.Repeat(`\`, slashes*2+1))
		} else {
			b.WriteString(strings.Repeat(`\`, slashes))
		}
		slashes = 0
		b.WriteRune(r)
	}
	b.WriteString(strings.Repeat(`\`, slashes*2))
	b.WriteByte('"')
	return b.String()
}
func Build(platform, home, config, executable, userID string) (Plan, error) {
	for _, value := range []string{home, config, executable, userID} {
		if value == "" || strings.ContainsAny(value, "\r\n\x00") {
			return Plan{}, errors.New("invalid service path or user")
		}
	}
	hash := sha256.Sum256([]byte(config))
	id := fmt.Sprintf("ranjit-%x", hash[:6])
	p := Plan{ID: id}
	switch platform {
	case "darwin":
		p.ID = "dev.getcortexlabs." + id
		p.Path = filepath.Join(home, "Library", "LaunchAgents", p.ID+".plist")
		p.Content = `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key><string>` + p.ID + `</string><key>ProgramArguments</key><array><string>` + escaped(executable) + `</string><string>--config</string><string>` + escaped(config) + `</string><string>serve</string></array><key>RunAtLoad</key><true/><key>KeepAlive</key><true/><key>ThrottleInterval</key><integer>30</integer><key>Umask</key><integer>63</integer></dict></plist>`
		domain := "gui/" + userID
		p.Start = [][]string{{"launchctl", "bootstrap", domain, p.Path}}
		p.Stop = [][]string{{"launchctl", "bootout", domain + "/" + p.ID}}
		p.Status = [][]string{{"launchctl", "print", domain + "/" + p.ID}}
		p.Install = p.Start
		p.Uninstall = p.Stop
	case "linux":
		p.Path = filepath.Join(home, ".config", "systemd", "user", id+".service")
		p.Content = "[Unit]\nDescription=Ranjit personal finance\n\n[Service]\nExecStart=" + quoted(executable) + " --config " + quoted(config) + " serve\nRestart=on-failure\nRestartSec=30\nUMask=0077\nNoNewPrivileges=true\n\n[Install]\nWantedBy=default.target\n"
		p.Start = [][]string{{"systemctl", "--user", "start", id}}
		p.Stop = [][]string{{"systemctl", "--user", "stop", id}}
		p.Status = [][]string{{"systemctl", "--user", "status", id}}
		p.Install = [][]string{{"systemctl", "--user", "daemon-reload"}, {"systemctl", "--user", "enable", "--now", id}}
		p.Uninstall = [][]string{{"systemctl", "--user", "disable", "--now", id}}
	case "windows":
		p.Path = filepath.Join(filepath.Dir(config), id+".task.xml")
		p.Content = `<?xml version="1.0" encoding="UTF-8"?><Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task"><Triggers><LogonTrigger><Enabled>true</Enabled><UserId>` + escaped(userID) + `</UserId></LogonTrigger></Triggers><Principals><Principal id="User"><UserId>` + escaped(userID) + `</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals><Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><ExecutionTimeLimit>PT0S</ExecutionTimeLimit><RestartOnFailure><Interval>PT1M</Interval><Count>3</Count></RestartOnFailure></Settings><Actions Context="User"><Exec><Command>` + escaped(executable) + `</Command><Arguments>` + escaped("--config "+windowsArg(config)+" serve") + `</Arguments></Exec></Actions></Task>`
		p.Start = [][]string{{"schtasks", "/Run", "/TN", id}}
		p.Stop = [][]string{{"schtasks", "/End", "/TN", id}}
		p.Status = [][]string{{"schtasks", "/Query", "/TN", id}}
		p.Install = append([][]string{{"schtasks", "/Create", "/TN", id, "/XML", p.Path}}, p.Start...)
		p.Uninstall = [][]string{{"schtasks", "/Delete", "/TN", id, "/F"}}
	default:
		return p, errors.New("user services support Linux, macOS and Windows")
	}
	return p, nil
}
func (p Plan) Run(ctx context.Context, action string) (string, error) {
	var commands [][]string
	switch action {
	case "install":
		commands = p.Install
	case "start":
		commands = p.Start
	case "stop":
		commands = p.Stop
	case "status":
		commands = p.Status
	case "uninstall":
		commands = p.Uninstall
	default:
		return "", errors.New("use service install|start|stop|status|uninstall")
	}
	if action == "install" {
		if err := os.MkdirAll(filepath.Dir(p.Path), 0700); err != nil {
			return "", err
		}
		f, err := os.OpenFile(p.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return "", fmt.Errorf("service file already exists or is not writable; use service start, or uninstall first: %w", err)
		}
		_, err = f.WriteString(p.Content)
		closeErr := f.Close()
		if err != nil {
			return "", err
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	var result strings.Builder
	for _, args := range commands {
		out, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
		result.Write(out)
		if err != nil {
			return result.String(), fmt.Errorf("%s failed: %w (service file: %s)", args[0], err, p.Path)
		}
	}
	if action == "uninstall" {
		if err := os.Remove(p.Path); err != nil && !os.IsNotExist(err) {
			return result.String(), err
		}
	}
	return result.String(), nil
}
