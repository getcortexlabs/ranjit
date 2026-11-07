# Sicoob

Consulta saldo e extrato da própria conta pela API Conta Corrente v4. O acesso precisa ser criado pelo cooperado no [Portal Developers](https://developers.sicoob.com.br/portal/). O portal descreve certificados A1 e-CNPJ ou e-CPF; a disponibilidade para a conta deve ser confirmada ao criar a aplicação. O Ranjit não cria esse acesso nem recebe dados de outros bancos por Open Finance.

## Configuração

Para o sandbox, obtenha Client ID e Access Token na área Sandbox do portal. Grave apenas o valor do token em `secrets/sicoob-sandbox-token`, sem o prefixo `Bearer`:

```dotenv
RANJIT_PROVIDER=sicoob
RANJIT_DATABASE=./data/sicoob-sandbox.db
RANJIT_SICOOB_ENV=sandbox
RANJIT_SICOOB_CLIENT_ID=client-id-do-sandbox
RANJIT_SICOOB_ACCESS_TOKEN_FILE=./secrets/sicoob-sandbox-token
RANJIT_SICOOB_ACCOUNT=123456
RANJIT_SICOOB_LOOKBACK_DAYS=7
```

O sandbox do banco entrega dados simulados e usa token estático. Não reproduz o fluxo de certificado de produção. Se o token deixar de funcionar, substitua o arquivo e reinicie.

Em produção, crie a aplicação com a permissão de consulta `cco_consulta`. Configure o certificado A1 do cooperado e sua chave em PEM:

```dotenv
RANJIT_SICOOB_ENV=production
RANJIT_DATABASE=./data/sicoob-production.db
RANJIT_SICOOB_CLIENT_ID=client-id-de-producao
RANJIT_SICOOB_ACCESS_TOKEN_FILE=
RANJIT_SICOOB_CERT_FILE=./secrets/sicoob-cert.pem
RANJIT_SICOOB_KEY_FILE=./secrets/sicoob-key.pem
```

Informe a conta incluindo o dígito, somente com números. Zeros à esquerda são normalizados. O número segue em `numeroContaCorrente` em todas as consultas. A conexão fica vinculada à aplicação, conta e ambiente; mudar um deles exige outra conexão.

Use `0700` em `secrets/` e `0600` nos arquivos. Execute `make run` e, em outro terminal, `make example`. O exemplo seleciona a instituição configurada. Para conectar pela API, envie `{"customer_id":"local-user","institution_id":"sicoob"}` a `POST /v1/connections`. A consulta de saldo verifica o acesso antes de criar uma conexão ativa.

## Contrato implementado

- OAuth em `auth.sicoob.com.br`, com mTLS, `client_credentials` e `cco_consulta`; sem Client Secret. O token fica em memória e uma resposta 401 permite uma renovação.
- `GET /conta-corrente/v4/saldo`: importa `saldo`, sem somar limite.
- `GET /conta-corrente/v4/extrato/{mes}/{ano}`: divide a janela em meses, envia os dias inicial e final e `agruparCNAB=false`.
- Deduplicação por `transactionId`. A data de lançamento usa `dataLote`, quando presente, ou `data`. Crédito positivo e débito negativo.

A janela vai de 1 a 90 dias, incluindo o dia atual em São Paulo. O limite local é de 10 mil lançamentos por tentativa e 2 MiB por resposta. O cliente espaça consultas por 550 ms para respeitar o limite publicado de duas requisições por segundo. HTTP 429 devolve o trabalho à fila com `Retry-After` limitado a cinco minutos.

O Swagger descreve respostas diretas; a coleção Postman também usa o envelope `resultado`. Ambos são aceitos. Respostas sem saldo, sem lista de transações ou sem identificador estável são rejeitadas. A falha de um mês descarta todo o lote daquela tentativa.

Referências: [documentação e autenticação](https://developers.sicoob.com.br/portal/documentacao), [Swagger Conta Corrente](https://developers.sicoob.com.br/portal/apis) e [coleção Postman oficial](https://d1zet1fng6kfaw.cloudfront.net/postman-collections/c13e59f30aaf6c95d7b88d8e18c7c1d7-API-Conta-Corrente-Sicoob.postman_collection), consultados em 07/11/2026. O catálogo atual usa `cco_consulta`; exemplos antigos de resposta do token ainda citam `cco_saldo` e `cco_extrato`.

## Validação e operação

Os testes exercitam OAuth, sandbox com token estático, consulta por mês, valores, falhas intermediárias e o fluxo SDK → HTTP → worker → SQLite. Não foi realizada homologação com credenciais do Sicoob.

O prazo de 90 dias da conexão é local. Revogar no Ranjit interrompe a coleta e remove os dados locais; para cancelar o acesso no banco, desabilite a aplicação no portal. Para Docker, use os cuidados de montagem de arquivos descritos em [operação](operations.md).
