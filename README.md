# Ranjit

Uma API em Go para conectar contas, acompanhar consentimentos e consultar dados financeiros no mesmo formato.

O Ranjit cuida da integração dentro da aplicação: estado da conexão, fila de sincronização, contas, saldos e transações. Inclui um sandbox local e um conector direto para a própria conta Efí, com saldo, Pix recebidos e devoluções concluídas desses recebimentos.

A integração Efí usa as credenciais da aplicação criada pelo titular no banco. Para compartilhamento entre bancos pelo Open Finance, ainda é necessária uma operação habilitada e outro adaptador. Os [limites de acesso](docs/open-finance.md) estão documentados com as referências oficiais.

## Rodar

Go 1.26 ou superior. Use o patch mais recente de uma versão suportada.

```sh
git clone https://github.com/getcortexlabs/ranjit.git
cd ranjit
make setup
make run
```

`make setup` gera uma chave aleatória e salva em `.env`, com permissão `0600`. Não sobrescreve uma configuração existente. `make run` lê esse arquivo e sobe a API em `http://localhost:8080`.

Em outro terminal:

```sh
make example
```

Abra o link impresso, autorize a simulação e volte ao terminal. O exemplo usa o SDK Go para consultar duas contas e quatro transações. As instituições e os dados são fictícios.

Para testar sem abrir o navegador, `make smoke` executa a autorização simulada, sincroniza duas vezes, confere a deduplicação e revoga a conexão. Requer Python 3 e o servidor rodando.

### Docker

Depois de criar o `.env`:

```sh
docker compose up --build
```

A imagem roda sem root. O Compose publica a porta apenas em loopback e guarda o SQLite no volume `ranjit_data`. Não rode `make run` e Compose na mesma porta ao mesmo tempo. Quem preferir preparar a configuração sem Go pode copiar `.env.example` para `.env` e preencher `RANJIT_API_KEY` com a saída de `openssl rand -hex 32`.

## Conectar a própria conta Efí

O [guia Efí](docs/efi.md) mostra como configurar Client ID, Client Secret e certificado para homologação ou produção. A modalidade Pro admite empreendedores sem CNPJ. O conector usa apenas leitura, e cada instalação mantém suas credenciais localmente.

O histórico inicial cobre Pix recebidos e suas devoluções concluídas. Compras, Pix enviados e o extrato completo não estão incluídos. O conector foi testado com respostas simuladas da API; a validação com uma conta habilitada ainda está pendente.

## Integrar

Toda chamada em `/v1` usa `Authorization: Bearer <RANJIT_API_KEY>`. Essa chave pertence ao backend da aplicação. Ela permite acessar todos os clientes daquela instalação; `customer_id` é um identificador de agrupamento, não uma barreira de autorização entre usuários.

```sh
set -a
. ./.env
set +a

curl http://localhost:8080/v1/connections \
  -H "Authorization: Bearer $RANJIT_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"customer_id":"user-42","institution_id":"sandbox-bank"}'
```

No sandbox, a resposta traz a conexão e `authorization_url`. O link vence em dez minutos e só pode ser usado uma vez. A aprovação enfileira a primeira sincronização. O consentimento simulado dura 90 dias, contados da criação da conexão; esse prazo não pretende reproduzir a regra de um banco. Para Efí, a conexão usa `api_credentials`, é criada ativa após verificar o acesso e não retorna link de autorização.

O [exemplo Go](examples/connect/main.go) mostra o fluxo completo. O contrato HTTP está em [docs/openapi.json](docs/openapi.json), e os detalhes de paginação, erros e estados em [docs/api.md](docs/api.md).

| Método | Caminho | Uso |
| --- | --- | --- |
| `GET` | `/v1/institutions` | Instituições configuradas e cobertura de dados |
| `POST` | `/v1/connections` | Criar conexão; link de autorização no sandbox |
| `GET` | `/v1/connections?customer_id=...` | Listar conexões do cliente |
| `GET` | `/v1/connections/{id}` | Consultar estado e última sincronização |
| `POST` | `/v1/connections/{id}/sync` | Enfileirar sincronização |
| `GET` | `/v1/jobs/{id}` | Acompanhar execução e tentativas |
| `GET` | `/v1/connections/{id}/accounts` | Consultar contas e saldos |
| `GET` | `/v1/accounts/{id}/transactions` | Consultar transações |
| `DELETE` | `/v1/connections/{id}` | Revogar e remover dados normalizados |

## Decisões do projeto

Um binário, um banco e um worker. `net/http` faz o transporte; SQLite guarda os dados e a fila; o provedor só entrega lotes normalizados. Não há Redis, ORM ou framework web. O driver SQLite funciona sem CGO.

Os valores monetários usam centavos inteiros. As importações são atômicas e fazem upsert por identificador estável do provedor. Um job interrompido pode ser retomado quando sua concessão de execução vence. O resultado de uma tentativa antiga é rejeitado, mesmo se chegar depois da nova tentativa.

Revogar uma conexão cancela seus jobs e apaga contas e transações na mesma transação. Os endpoints também conferem o vencimento do consentimento antes de liberar dados, sem depender da próxima execução do worker.

A [arquitetura](docs/architecture.md) explica os limites dos pacotes e os motivos dessas escolhas. O guia de [provedores](docs/providers.md) descreve o contrato que uma futura integração precisa cumprir.

## Desenvolvimento

```sh
make check     # testes com detector de corrida, go vet e formatação
make build     # bin/ranjit, sem CGO
```

Os testes usam SQLite em diretórios temporários e exercitam os handlers HTTP reais com transporte em memória. Cobrem consentimento, autenticação, paginação, isolamento dos IDs entre conexões, rollback, repetição de sincronização, concorrência, expiração e retomada após reinício.

Antes de operar fora da máquina local, leia [operação](docs/operations.md). A versão atual tem uma chave por instalação e banco sem criptografia em repouso. A configuração escolhe um provedor por processo: sandbox ou uma aplicação Efí. Webhooks, múltiplos operadores, extratos completos e PostgreSQL ficam para mudanças com casos de uso definidos.

Contribuições: [CONTRIBUTING.md](CONTRIBUTING.md). Licença: [MIT](LICENSE).
