package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/getcortexlabs/ranjit/internal/config"
)

func (c cli) rotateKey(args []string) error {
	f := c.flags("key rotate")
	grace := f.Duration("grace", 24*time.Hour, "tempo para a chave anterior continuar válida (0 a 168h)")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 || *grace < 0 || *grace > 7*24*time.Hour {
		return errors.New("use ranjit key rotate [--grace 24h]; máximo 168h")
	}
	cfg, err := localConfig(false)
	if err != nil {
		return err
	}
	if cfg.PersonalFile == "" {
		return errors.New("configuração por ambiente: defina RANJIT_API_KEY_PREVIOUS e RANJIT_API_KEY_PREVIOUS_EXPIRES_AT, troque RANJIT_API_KEY e recrie o serviço")
	}
	p, err := config.ReadPersonal(cfg.PersonalFile)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if p.PreviousAPIKeyExpiresAt.After(now) && *grace > 0 {
		return errors.New("há uma troca em andamento; aguarde a expiração ou use --grace 0 para invalidar as chaves anteriores")
	}
	p.PreviousAPIKey, p.PreviousAPIKeyExpiresAt = "", time.Time{}
	if *grace > 0 {
		p.PreviousAPIKey, p.PreviousAPIKeyExpiresAt = p.APIKey, now.Add(*grace)
	}
	p.APIKey = rand.Text() + rand.Text()
	if err = config.SavePersonal(cfg.PersonalFile, p); err != nil {
		return err
	}
	// stdout contains only the new key, so it can be redirected to a secret file.
	_, err = fmt.Fprintln(c.out, p.APIKey)
	return err
}
