package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/app"
	"github.com/getcortexlabs/ranjit/internal/config"
	"github.com/getcortexlabs/ranjit/internal/service"
	"github.com/getcortexlabs/ranjit/internal/store"
	"github.com/getcortexlabs/ranjit/internal/worker"
)

func (c cli) sync(ctx context.Context, command string, args []string) error {
	f := c.flags(command)
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() > 1 {
		return errors.New("use ranjit sync [perfil]")
	}
	selected := f.Arg(0)
	if command == "demo" {
		if f.NArg() != 0 {
			return errors.New("use ranjit demo")
		}
		p, path, err := personal(true)
		if err != nil {
			return err
		}
		found := false
		for _, profile := range p.Profiles {
			if profile.Name == "demo" {
				if profile.Provider != "sandbox" {
					return errors.New("o perfil demo já pertence a outro banco")
				}
				found = true
			}
		}
		if !found {
			p.Profiles = append(p.Profiles, config.Profile{Name: "demo", Provider: "sandbox"})
			if err = config.SavePersonal(path, p); err != nil {
				return err
			}
		}
		selected = "demo"
		fmt.Fprintln(c.out, "Demonstração local: contas e lançamentos fictícios.")
	}
	cfg, err := localConfig(false)
	if err != nil {
		return err
	}
	// An unrelated broken profile must not prevent syncing the selected account.
	if selected != "" && cfg.Provider == "profiles" {
		profiles := []config.Profile{}
		for _, p := range cfg.Profiles {
			if p.Name == selected {
				profiles = append(profiles, p)
			}
		}
		if len(profiles) == 0 {
			return fmt.Errorf("perfil %s não encontrado", selected)
		}
		cfg.Profiles = profiles
	}
	source, closeSource, err := app.Source(cfg)
	if err != nil {
		return err
	}
	defer closeSource()
	if len(source.Institutions()) == 0 {
		return errors.New("configure um perfil bancário com ranjit configure; para extratos, use ranjit import")
	}
	db, err := store.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer db.Close()
	svc := service.New(db, source, cfg.BaseURL)
	connections, err := allConnections(ctx, db)
	if err != nil {
		return err
	}
	jobs := []string{}
	for _, i := range source.Institutions() {
		var connection ranjit.Connection
		for _, existing := range connections {
			if existing.InstitutionID == i.ID && existing.Status == "active" {
				connection = existing
				break
			}
		}
		if connection.ID == "" {
			for _, existing := range connections {
				if existing.InstitutionID == i.ID && existing.AuthorizationMode == "api_credentials" && (existing.Status == "paused" || existing.Status == "expired") {
					return fmt.Errorf("acesso pausado ou vencido; use ranjit renew %s para manter o histórico", existing.ID)
				}
			}
			response, err := svc.Connect(ctx, ranjit.ConnectRequest{CustomerID: "personal", InstitutionID: i.ID})
			if err != nil {
				return err
			}
			connection = response.Connection
			if response.AuthorizationURL != "" {
				u, err := url.Parse(response.AuthorizationURL)
				if err != nil {
					return err
				}
				connection, err = svc.Decide(ctx, u.Query().Get("token"), "approve")
				if err != nil {
					return err
				}
			}
		}
		job, err := db.Enqueue(ctx, connection.ID, time.Now())
		if err != nil {
			return err
		}
		jobs = append(jobs, job.ID)
	}
	// The one-shot worker sees only the chosen connections; the server worker
	// remains responsible for its full registry and schedule.
	w := worker.New(db, source, cfg.SyncInterval, c.logger)
	w.ConnectionIDs = map[string]bool{}
	for _, id := range jobs {
		job, err := db.Job(ctx, id)
		if err != nil {
			return err
		}
		w.ConnectionIDs[job.ConnectionID] = true
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for {
		if err = w.Step(ctx); err != nil {
			return err
		}
		done := true
		for _, id := range jobs {
			j, err := db.Job(ctx, id)
			if err != nil {
				return err
			}
			if j.Status == "failed" || j.Status == "cancelled" {
				return fmt.Errorf("sincronização %s: %s; use ranjit doctor --live", j.Status, j.ErrorCode)
			}
			if j.Status != "succeeded" {
				done = false
			}
		}
		if done {
			fmt.Fprintln(c.out, "Sincronização concluída. Use ranjit accounts ou ranjit transactions.")
			return nil
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.New("tempo de espera encerrado; o progresso está salvo. Rode ranjit sync novamente")
		case <-timer.C:
		}
	}
}
