Ranjit como serviço para seu backend, com SDK Go e PostgreSQL.

- PostgreSQL para hospedagem, mantendo SQLite como padrão local.
- Configuração de vários perfis por `RANJIT_PROFILES_JSON` e credenciais por referências `env:NOME` aos segredos da plataforma.
- `ranjit.NewClientFromEnv()` e um exemplo de backend com importação e consulta de transações.
- Guia de instalação em servidor e receita preparada para o catálogo do Orbit.
- Testes com PostgreSQL real para concorrência, reinício, rollback, deduplicação e revogação; integração do SDK verificada contra o servidor em execução.

A troca de SQLite para PostgreSQL não transfere o histórico automaticamente. Backups e atualização de uma instalação PostgreSQL ficam sob a hospedagem. A entrada no catálogo do Orbit depende da publicação da receita e do download anônimo da imagem no GHCR.

Os conectores bancários continuam experimentais e ainda não foram validados com uma conta real habilitada. A instalação começa com importação de extratos; conectar uma conta exige configurar o acesso à API correspondente.

Instalação e integração: https://github.com/getcortexlabs/ranjit/blob/main/docs/server.md
