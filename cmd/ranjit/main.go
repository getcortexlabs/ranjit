package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/getcortexlabs/ranjit/internal/app"
	"github.com/getcortexlabs/ranjit/internal/config"
	"github.com/getcortexlabs/ranjit/internal/httpapi"
	"github.com/getcortexlabs/ranjit/internal/service"
	"github.com/getcortexlabs/ranjit/internal/store"
	"github.com/getcortexlabs/ranjit/internal/worker"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("ranjit stopped", "error", err.Error())
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	if len(os.Args) > 2 && os.Args[1] == "--config" {
		if err := os.Setenv("RANJIT_CONFIG", os.Args[2]); err != nil {
			return err
		}
		os.Args = append(os.Args[:1], os.Args[3:]...)
	}
	ui, noBrowser := false, false
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "init":
			if len(os.Args) != 2 {
				return errors.New("use ranjit init")
			}
			return initEnv()
		case "version":
			fmt.Println(version)
			return nil
		case "healthcheck":
			return healthcheck()
		case "ui":
			f := flag.NewFlagSet("ui", flag.ContinueOnError)
			f.BoolVar(&noBrowser, "no-browser", false, "mostrar link sem abrir navegador")
			if err := f.Parse(os.Args[2:]); err != nil {
				if errors.Is(err, flag.ErrHelp) {
					return nil
				}
				return err
			}
			if f.NArg() != 0 {
				return errors.New("use ranjit ui [--no-browser]")
			}
			ui = true
		case "serve":
			if len(os.Args) != 2 {
				return errors.New("use ranjit serve")
			}
		default:
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			err := (cli{in: os.Stdin, out: os.Stdout, logger: logger}).command(ctx, os.Args[1:])
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
	} else if os.Getenv("RANJIT_API_KEY") == "" {
		fmt.Print(help)
		return nil
	}
	if os.Getenv("RANJIT_API_KEY") == "" {
		if _, _, err := personal(true); err != nil {
			return err
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if ui {
		if err = checkLocalUI(cfg.BaseURL); err != nil {
			return err
		}
		if link, e := dashboardLink(cfg); e == nil {
			return showDashboard(link, noBrowser)
		}
	}
	db, err := store.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer db.Close()
	source, closeSource, unavailable, err := app.Available(cfg)
	if err != nil {
		return err
	}
	defer closeSource()
	for _, name := range unavailable {
		logger.Warn("profile unavailable; run ranjit doctor", "profile", name)
	}
	for _, cert := range cfg.Certificates(time.Now()) {
		if cert.Level != "ok" {
			logger.Warn("certificate needs attention", "profile", cert.Profile, "status", cert.Level, "days_remaining", cert.DaysRemaining)
		}
	}
	svc := service.New(db, source, cfg.BaseURL)
	server := &http.Server{Addr: cfg.Addr, Handler: httpapi.New(svc, cfg.APIKey, logger, httpapi.Diagnostics(func() any {
		return map[string]any{"version": version, "certificates": cfg.Certificates(time.Now()), "unavailable_profiles": unavailable}
	})), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); worker.New(db, source, cfg.SyncInterval, logger).Run(workerCtx) }()
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	if ui {
		link, e := dashboardLink(cfg)
		if e != nil {
			_ = server.Close()
			cancelWorker()
			<-workerDone
			return e
		}
		if e = showDashboard(link, noBrowser); e != nil {
			logger.Warn("open the displayed link in your browser")
		}
	}
	logger.Info("ranjit started", "version", version, "addr", cfg.Addr, "provider", cfg.Provider)
	select {
	case <-ctx.Done():
	case err = <-serveErr:
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
		_ = server.Close()
		err = shutdownErr
	}
	cancelWorker()
	<-workerDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func initEnv() error {
	f, err := os.OpenFile(".env", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create .env (existing files are kept): %w", err)
	}
	_, writeErr := fmt.Fprintf(f, "RANJIT_API_KEY=%s\nRANJIT_ADDR=127.0.0.1:8080\nRANJIT_BASE_URL=http://localhost:8080\nRANJIT_DATABASE=./data/ranjit.db\nRANJIT_PROVIDER=sandbox\nRANJIT_SYNC_INTERVAL=1h\n", rand.Text()+rand.Text())
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	fmt.Println("Created .env. Run make run or docker compose up --build.")
	return nil
}

func healthcheck() error {
	addr := os.Getenv("RANJIT_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
		if path, e := config.PersonalPath(); e == nil {
			if p, e := config.ReadPersonal(path); e == nil {
				addr = p.Addr
			}
		}
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/readyz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("readiness returned %d", resp.StatusCode)
	}
	return nil
}
