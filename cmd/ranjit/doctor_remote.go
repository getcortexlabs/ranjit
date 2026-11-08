package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/getcortexlabs/ranjit"
)

type doctorCheck struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Action  string `json:"action,omitempty"`
}
type doctorReport struct {
	SchemaVersion int           `json:"schema_version"`
	Status        string        `json:"status"`
	ServerVersion string        `json:"server_version,omitempty"`
	Checks        []doctorCheck `json:"checks"`
}

func (c cli) remoteDoctor(ctx context.Context, address, key string, timeout time.Duration, asJSON bool) error {
	report := doctorReport{SchemaVersion: 1, Status: "ok", Checks: []doctorCheck{}}
	add := func(id, status, message, action string) {
		report.Checks = append(report.Checks, doctorCheck{id, status, message, action})
		if status == "error" || status == "warning" && report.Status == "ok" {
			report.Status = status
		}
	}
	if timeout <= 0 || timeout > time.Minute {
		return errors.New("--timeout deve ser maior que zero e no máximo 1m")
	}
	client, err := ranjit.NewClient(address, key)
	if err != nil {
		add("configuration", "error", "URL ou chave da API ausente/inválida.", "Configure RANJIT_URL como origem HTTP(S) e RANJIT_API_KEY como segredo do ambiente.")
		return c.printDoctor(report, asJSON)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err = client.Health(ctx); err != nil {
		add("reachability", "error", "O serviço não respondeu ao healthcheck esperado.", "Confira URL, certificado TLS, proxy e conectividade.")
		return c.printDoctor(report, asJSON)
	}
	add("reachability", "ok", "Serviço acessível.", "")
	if _, err = client.Institutions(ctx); err != nil {
		if errors.Is(err, ranjit.ErrUnauthorized) {
			add("authentication", "error", "Chave da API recusada.", "Confira a chave e o prazo de uma eventual rotação.")
		} else {
			add("authentication", "error", "Não foi possível confirmar o acesso à API.", "Confira o proxy, a disponibilidade do serviço e a URL configurada.")
		}
		return c.printDoctor(report, asJSON)
	}
	add("authentication", "ok", "Chave aceita.", "")
	if err = client.Ready(ctx); err != nil {
		add("database", "error", "A prontidão do armazenamento não foi confirmada.", "Confira o banco e a conexão configurada no serviço.")
	} else {
		add("database", "ok", "Armazenamento pronto.", "")
	}
	diagnostics, err := client.Diagnostics(ctx)
	if err != nil {
		var api *ranjit.APIError
		if errors.As(err, &api) && api.StatusCode == 404 {
			add("diagnostics", "warning", "Esta instalação não disponibiliza diagnósticos.", "Confira a versão instalada.")
		} else {
			add("diagnostics", "error", "Não foi possível ler os diagnósticos.", "Confira disponibilidade, autenticação e versão do serviço.")
		}
		return c.printDoctor(report, asJSON)
	}
	if safeVersion(diagnostics.Version) {
		report.ServerVersion = diagnostics.Version
		add("version", "ok", "Versão do servidor identificada.", "")
	} else {
		add("version", "warning", "O servidor não informou uma versão reconhecida.", "Confira o binário ou a tag da imagem instalada.")
	}
	if diagnostics.Operations == nil {
		add("workers", "warning", "Esta versão não informa a atividade dos workers.", "Atualize para uma versão com diagnósticos operacionais.")
	} else {
		if diagnostics.Operations.Database != "ready" {
			add("operational_database", "error", "A leitura das filas falhou.", "Confira a disponibilidade e as permissões do banco.")
		}
		for _, name := range []string{"sync", "webhook"} {
			worker, ok := diagnostics.Operations.Workers[name]
			switch {
			case !ok:
				add("worker_"+name, "warning", "Atividade do worker não informada.", "Confira a versão do serviço.")
			case worker.State == "disabled":
				add("worker_"+name, "ok", "Worker desabilitado na configuração.", "")
			case worker.State == "starting":
				add("worker_"+name, "warning", "Worker iniciando.", "Repita o diagnóstico após a inicialização.")
			case !worker.Healthy:
				add("worker_"+name, "error", "Worker sem progresso saudável.", "Confira os logs e a disponibilidade do armazenamento.")
			default:
				add("worker_"+name, "ok", "Worker em atividade ou aguardando trabalho.", "")
			}
		}
		if diagnostics.Operations.WebhookQueue.Counts["failed"] > 0 {
			add("webhook_deliveries", "warning", "Há notificações com tentativas esgotadas.", "Confira o receptor e reenvie as entregas pela API.")
		}
		if diagnostics.Operations.SyncQueue.RecentFailed > 0 {
			add("synchronizations", "warning", "Há sincronizações que falharam nas últimas 24 horas.", "Consulte os jobs e confira os perfis bancários afetados.")
		}
	}
	if len(diagnostics.UnavailableProfiles) > 0 {
		add("profiles", "warning", "Há perfis indisponíveis no servidor.", "Confira as credenciais e os certificados no ambiente do Ranjit.")
	}
	for _, cert := range diagnostics.Certificates {
		if cert.Level != "ok" {
			add("certificates", "warning", "Há certificados que precisam de atenção.", "Consulte os diagnósticos do servidor e renove os certificados necessários.")
			break
		}
	}
	return c.printDoctor(report, asJSON)
}

func (c cli) printDoctor(report doctorReport, asJSON bool) error {
	if asJSON {
		if err := json.NewEncoder(c.out).Encode(report); err != nil {
			return err
		}
	} else {
		if report.ServerVersion != "" {
			fmt.Fprintln(c.out, "Ranjit", report.ServerVersion)
		}
		for _, check := range report.Checks {
			label := map[string]string{"ok": "OK", "warning": "AVISO", "error": "ERRO"}[check.Status]
			fmt.Fprintf(c.out, "%s  %s: %s\n", label, check.ID, check.Message)
			if check.Action != "" {
				fmt.Fprintln(c.out, "    "+check.Action)
			}
		}
	}
	if report.Status == "error" {
		return errors.New("diagnóstico remoto encontrou falhas")
	}
	return nil
}

func safeVersion(v string) bool {
	if v == "" || len(v) > 64 {
		return false
	}
	for _, r := range v {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '+') {
			return false
		}
	}
	return true
}
