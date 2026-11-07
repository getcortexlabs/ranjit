package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/getcortexlabs/ranjit/internal/autostart"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
)

func (c cli) serviceCommand(ctx context.Context, args []string) error {
	f := c.flags("service")
	dry := f.Bool("dry-run", false, "mostrar serviço sem instalar")
	// Accept the readable form `service install --dry-run` as well.
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		args = append(append([]string{}, args[1:]...), args[0])
	}
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 1 {
		return errors.New("use ranjit service install|start|stop|status|uninstall [--dry-run]")
	}
	action := f.Arg(0)
	switch action {
	case "install", "start", "stop", "status", "uninstall":
	default:
		return errors.New("unknown service action")
	}
	_, path, err := personal(!*dry && action == "install")
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	u, err := user.Current()
	if err != nil {
		return err
	}
	plan, err := autostart.Build(runtime.GOOS, u.HomeDir, path, exe, u.Uid)
	if err != nil {
		return err
	}
	if *dry {
		fmt.Fprintln(c.out, plan.Path)
		fmt.Fprintln(c.out, plan.Content)
		return nil
	}
	out, err := plan.Run(ctx, action)
	fmt.Fprint(c.out, out)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Serviço %s: %s. Configuração: %s\n", plan.ID, action, path)
	return nil
}
