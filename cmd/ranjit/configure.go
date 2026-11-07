package main

import (
	"bufio"
	"errors"
	"fmt"
	"github.com/getcortexlabs/ranjit/internal/app"
	"github.com/getcortexlabs/ranjit/internal/config"
	"os"
	"path/filepath"
	"strings"
)

func (c cli) configure(args []string) error {
	f := c.flags("configure")
	var p config.Profile
	f.StringVar(&p.Provider, "bank", "", "file, sandbox, efi, inter, sicoob, c6 ou santander")
	f.StringVar(&p.Name, "name", "", "nome do perfil (ex.: trabalho)")
	f.StringVar(&p.Environment, "environment", "", "sandbox/homologation ou production")
	f.StringVar(&p.ClientID, "client-id", "", "Client ID da aplicação")
	f.StringVar(&p.ClientSecretFile, "secret-file", "", "arquivo com Client Secret")
	f.StringVar(&p.CertificateFile, "cert-file", "", "certificado PEM")
	f.StringVar(&p.PrivateKeyFile, "key-file", "", "chave privada PEM")
	f.StringVar(&p.AccessTokenFile, "token-file", "", "token de sandbox Sicoob")
	f.StringVar(&p.Account, "account", "", "número da conta, conforme o guia do banco")
	f.IntVar(&p.LookbackDays, "days", 7, "histórico de 1 a 90 dias")
	replace := f.Bool("replace", false, "substituir um perfil existente com este nome")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("configure não aceita argumentos posicionais")
	}
	interactive := p.Provider == ""
	reader := bufio.NewReader(c.in)
	ask := func(label, fallback string) (string, error) {
		fmt.Fprintf(c.out, "%s", label)
		if fallback != "" {
			fmt.Fprintf(c.out, " [%s]", fallback)
		}
		fmt.Fprint(c.out, ": ")
		value, err := reader.ReadString('\n')
		if err != nil {
			return "", errors.New("entrada encerrada; use as opções de ranjit configure --help para automatizar")
		}
		value = strings.TrimSpace(value)
		if value == "" {
			value = fallback
		}
		return value, nil
	}
	var err error
	if interactive {
		fmt.Fprintln(c.out, "Escolha file para importar extratos, sandbox para testar ou o nome do banco.")
		p.Provider, err = ask("Banco: file, sandbox, efi, inter, sicoob, c6, santander", "file")
		if err != nil {
			return err
		}
	}
	current, path, err := personal(true)
	if err != nil {
		return err
	}
	if p.Provider == "file" {
		fmt.Fprintf(c.out, "Pronto. Configuração: %s\nUse: ranjit import extrato.ofx\n", path)
		return nil
	}
	if p.Name == "" {
		p.Name = p.Provider
		if interactive {
			p.Name, err = ask("Nome do perfil", p.Name)
			if err != nil {
				return err
			}
		}
	}
	if !config.ValidName(p.Name) {
		return errors.New("nome inválido: use letras, números, _ ou -; até 64 caracteres")
	}
	if p.Provider != "sandbox" {
		if p.Environment == "" {
			p.Environment = "sandbox"
			if p.Provider == "efi" {
				p.Environment = "homologation"
			}
		}
		if interactive {
			fmt.Fprintf(c.out, "Guia: https://github.com/getcortexlabs/ranjit/blob/main/docs/%s.md\n", p.Provider)
			p.Environment, err = ask("Ambiente", p.Environment)
			if err != nil {
				return err
			}
			p.ClientID, err = ask("Client ID", "")
			if err != nil {
				return err
			}
			if p.Provider == "sicoob" && p.Environment == "sandbox" {
				p.AccessTokenFile, err = ask("Caminho do arquivo com o token", "")
				if err != nil {
					return err
				}
			} else {
				if p.Provider != "sicoob" {
					p.ClientSecretFile, err = ask("Caminho do arquivo com Client Secret", "")
					if err != nil {
						return err
					}
				}
				p.CertificateFile, err = ask("Caminho do certificado PEM", "")
				if err != nil {
					return err
				}
				p.PrivateKeyFile, err = ask("Caminho da chave privada PEM", "")
				if err != nil {
					return err
				}
			}
			if p.Provider == "inter" || p.Provider == "sicoob" || p.Provider == "santander" {
				p.Account, err = ask("Conta (Santander: 4 dígitos da agência + ponto + 12 da conta)", "")
				if err != nil {
					return err
				}
			}
		}
	}
	for _, value := range []*string{&p.ClientSecretFile, &p.CertificateFile, &p.PrivateKeyFile, &p.AccessTokenFile} {
		if *value == "" {
			continue
		}
		if strings.HasPrefix(*value, "~/") {
			home, e := os.UserHomeDir()
			if e != nil {
				return e
			}
			*value = filepath.Join(home, (*value)[2:])
		}
		*value, err = filepath.Abs(*value)
		if err != nil {
			return err
		}
	}
	source, err := app.Profile(p)
	if err != nil {
		return err
	}
	if closer, ok := source.(interface{ Close() }); ok {
		closer.Close()
	}
	found := false
	for i, profile := range current.Profiles {
		if profile.Name == p.Name {
			if !*replace {
				return fmt.Errorf("perfil %s já existe; use outro nome ou --replace", p.Name)
			}
			current.Profiles[i] = p
			found = true
		}
	}
	if !found {
		current.Profiles = append(current.Profiles, p)
	}
	if err = config.SavePersonal(path, current); err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Perfil %s salvo em %s\nUse: ranjit doctor --live\nDepois: ranjit sync %s\nSe a API estiver rodando, reinicie-a para carregar o perfil.\n", p.Name, path, p.Name)
	return nil
}
