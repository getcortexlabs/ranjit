package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/config"
	"github.com/getcortexlabs/ranjit/internal/store"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
)

const help = `Ranjit — suas contas, no seu computador.

Comece sem credenciais bancárias:
  ranjit ui                             Abrir o painel no navegador
  ranjit demo                           Experimentar com dados fictícios
  ranjit import extrato.ofx              Importar um extrato OFX
  ranjit import --account pessoal extrato.csv
  ranjit status                         Ver conexões e última sincronização
  ranjit accounts                       Ver contas e saldos
  ranjit transactions                   Ver lançamentos

Conexão automática (exige acesso à API do banco):
  ranjit configure                      Assistente de configuração
  ranjit doctor                         Conferir configuração e certificados
  ranjit doctor --live                   Conferir também o acesso ao saldo
  ranjit sync [perfil]                   Conectar e sincronizar agora
  ranjit serve                          API local e sincronização periódica

Outros comandos:
  ranjit pause ID                        Pausar coleta, preservando histórico
  ranjit renew ID                        Renovar acesso e retomar coleta
  ranjit backup copia.db                 Salvar cópia consistente dos dados
  ranjit restore --replace copia.db      Restaurar com o serviço parado
  ranjit service install                Iniciar ao entrar no computador
  ranjit update                         Atualizar para a versão estável
  ranjit validate perfil                Testar acesso e gerar relatório local
  ranjit revoke ID                       Revogar conexão e apagar seus dados
  ranjit key                            Mostrar chave da API local
  ranjit version                        Mostrar versão
  ranjit init                           Gerar .env para o fluxo com Make/Docker

Use --help em um comando para ver suas opções.
RANJIT_CONFIG escolhe outro arquivo de configuração.
Os dados ficam locais. Os conectores bancários são experimentais.
`

type cli struct {
	in     io.Reader
	out    io.Writer
	logger *slog.Logger
}

func (c cli) command(ctx context.Context, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(c.out, help)
		return nil
	}
	switch args[0] {
	case "help", "--help", "-h":
		fmt.Fprint(c.out, help)
		return nil
	case "configure":
		return c.configure(args[1:])
	case "backup", "restore", "pause", "renew":
		return c.maintenance(ctx, args[0], args[1:])
	case "service":
		return c.serviceCommand(ctx, args[1:])
	case "update":
		return c.update(ctx, args[1:])
	case "validate":
		return c.validate(ctx, args[1:])
	case "doctor":
		return c.doctor(ctx, args[1:])
	case "import":
		return c.importFile(ctx, args[1:])
	case "status", "accounts", "transactions", "key", "revoke":
		return c.inspect(ctx, args[0], args[1:])
	case "sync", "demo":
		return c.sync(ctx, args[0], args[1:])
	default:
		return fmt.Errorf("comando desconhecido: %s; use ranjit --help", args[0])
	}
}

func (c cli) flags(name string) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(c.out)
	return f
}

func personal(create bool) (config.Personal, string, error) {
	path, err := config.PersonalPath()
	if err != nil {
		return config.Personal{}, path, err
	}
	p, err := config.ReadPersonal(path)
	if os.IsNotExist(err) && create {
		p = config.NewPersonal()
		err = config.SavePersonal(path, p)
	}
	if os.IsNotExist(err) {
		return p, path, errors.New("nenhuma configuração; rode ranjit configure, ranjit demo ou ranjit import extrato.ofx")
	}
	return p, path, err
}

func localConfig(create bool) (config.Config, error) {
	if os.Getenv("RANJIT_CONFIG") == "" && os.Getenv("RANJIT_API_KEY") != "" {
		return config.Load()
	}
	p, path, err := personal(create)
	if err != nil {
		return config.Config{}, err
	}
	return p.Runtime(path), nil
}

func allConnections(ctx context.Context, db *store.Store) ([]ranjit.Connection, error) {
	var connections []ranjit.Connection
	cursor := ""
	for {
		page, err := db.Connections(ctx, "personal", cursor, 100, time.Now())
		if err != nil {
			return nil, err
		}
		connections = append(connections, page.Data...)
		if page.NextCursor == "" {
			return connections, nil
		}
		cursor = page.NextCursor
	}
}

func money(m ranjit.Money) string {
	sign := ""
	n := m.AmountMinor
	if n < 0 {
		sign = "-"
		n = -n
	}
	return fmt.Sprintf("%sR$ %d,%02d", sign, n/100, n%100)
}
func terminal(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || r >= 0x80 && r <= 0x9f {
			return ' '
		}
		return r
	}, s)
}
