# Ranjit

Um programa em Go para importar extratos, conectar suas próprias contas e consultar dados financeiros no terminal ou por API.

O Ranjit cuida da integração dentro da aplicação: estado da conexão, fila de sincronização, contas, saldos e transações. Inclui um sandbox local e conectores diretos para a própria conta Efí, Inter Empresas, Sicoob, C6 Empresas e Santander Empresas. A cobertura de dados depende do banco.

As integrações diretas usam as credenciais da aplicação criada pelo titular no banco. Para compartilhamento entre bancos pelo Open Finance, ainda é necessária uma operação habilitada e outro adaptador. Os [limites de acesso](docs/open-finance.md) estão documentados com as referências oficiais.

## Instalar

macOS e Linux, sem Go e sem Docker:

```sh
curl -fsSL https://raw.githubusercontent.com/getcortexlabs/ranjit/main/install.sh -o /tmp/ranjit-install.sh
sh /tmp/ranjit-install.sh
```

O instalador baixa o binário da release, verifica o SHA-256 e instala em `~/.local/bin`. Se essa pasta ainda não estiver no `PATH`, ele mostra o comando para executar. Não usa `sudo` nem altera arquivos do shell. Para escolher outra pasta, use `RANJIT_INSTALL_DIR`; para fixar uma versão, `RANJIT_VERSION`.

No Windows, baixe o ZIP correspondente ao processador em [Releases](https://github.com/getcortexlabs/ranjit/releases), extraia e execute `ranjit.exe`. Os arquivos `.tar.gz` também permitem instalação manual no macOS/Linux. `checksums.txt` acompanha cada release.

## Usar

Para conhecer o fluxo com dados fictícios:

```sh
ranjit demo
ranjit accounts
ranjit transactions
```

Para usar um extrato que você exportou do banco:

```sh
ranjit import extrato.ofx
ranjit accounts
ranjit transactions
ranjit status
```

Isso funciona localmente, sem credenciais de API ou servidor rodando. CSV também funciona:

```sh
ranjit import --account pessoal extrato.csv
# CSV com valores como -12,34 e campos separados por ponto e vírgula:
ranjit import --account pessoal --decimal , extrato.csv
```

O CSV precisa de `date,description,amount` ou `data,descrição,valor`. A coluna `id` é recomendada. Sem IDs, repetir o mesmo arquivo é seguro; novos arquivos com períodos sobrepostos são recusados para evitar confundir lançamentos iguais. Datas aceitas: `YYYY-MM-DD` e `DD/MM/YYYY`. Veja o [formato dos extratos](docs/imports.md).

Para conexão automática com a API de um banco:

```sh
ranjit configure
ranjit doctor --live
ranjit sync meu-perfil
```

O assistente pede o banco, o nome do perfil e os caminhos dos arquivos de credenciais. Rode novamente para adicionar outra conta. Se `ranjit serve` estiver rodando, reinicie-o depois de alterar os perfis. Os requisitos de acesso continuam sendo os de cada banco; o assistente configura uma integração já habilitada.

`ranjit status` mostra a última sincronização, os lançamentos armazenados e o progresso do job. `ranjit doctor` verifica a configuração local; `--live` consulta o saldo, sem imprimir valores ou credenciais. Essa consulta não equivale a homologar o extrato. Todos os conectores bancários estão marcados como `experimental`.

A configuração pessoal fica na pasta de configuração do sistema, dentro de `ranjit/config.json`. O SQLite fica em `data/` ao lado desse arquivo. `RANJIT_CONFIG=/outro/caminho/config.json` permite uma instalação separada. Arquivos novos recebem permissão `0600` nos sistemas Unix.

## API e sincronização periódica

```sh
ranjit serve
```

A API escuta em `http://localhost:8080`. `ranjit key` mostra a chave para integrar seu backend; guarde-a como segredo. Para consulta no terminal, os comandos acima dispensam a API. `ranjit revoke ID` revoga uma conexão e remove seus dados financeiros locais.

### Docker

O instalador também baixa uma imagem pronta, verifica o checksum e carrega no Docker. Não exige conta em registry:

```sh
curl -fsSL https://raw.githubusercontent.com/getcortexlabs/ranjit/main/install.sh -o /tmp/ranjit-install.sh
sh /tmp/ranjit-install.sh --docker
cd ranjit-docker
docker compose up -d
docker compose exec ranjit /ranjit demo
docker compose exec ranjit /ranjit accounts
```

O servidor gera a configuração e a chave no primeiro uso. O volume mantém os dados; a porta é publicada apenas em loopback. Para obter a chave: `docker compose exec ranjit /ranjit key`. Para importar, monte a pasta dos extratos como volume de leitura e execute `/ranjit import /caminho/extrato.ofx`. A imagem usa um filesystem somente leitura; credenciais também devem ser montadas como volumes de leitura. O container roda sem root.

### Compilar

Go 1.26 ou superior, com o patch mais recente da versão:

```sh
git clone https://github.com/getcortexlabs/ranjit.git
cd ranjit
make build
./bin/ranjit demo
```

O fluxo por variáveis de ambiente continua disponível: `make setup` gera `.env`, `make run` inicia a API e `make example` demonstra o SDK. O Compose principal do repositório usa esse `.env` e compila a imagem com `docker compose up --build`. `make smoke` testa a API já em execução.

## Conectar a própria conta Efí

O [guia Efí](docs/efi.md) mostra como configurar Client ID, Client Secret e certificado para homologação ou produção. A modalidade Pro admite empreendedores sem CNPJ. O conector usa apenas leitura, e cada instalação mantém suas credenciais localmente.

O histórico inicial cobre Pix recebidos e suas devoluções concluídas. Compras, Pix enviados e o extrato completo não estão incluídos. O conector foi testado com respostas simuladas da API; a validação com uma conta habilitada ainda está pendente.

## Conectar a própria conta Inter Empresas

O [guia Inter](docs/inter.md) explica como configurar a API Banking para consultar saldo disponível e lançamentos do extrato. A integração atende contas PJ; o banco não disponibiliza essas APIs para PF ou MEI. A instalação seleciona explicitamente a conta e solicita apenas `extrato.read`.

Os testes locais usam respostas simuladas. A validação com uma conta Inter habilitada ainda está pendente.

## Sicoob, C6 Empresas e Santander Empresas

Os guias de [Sicoob](docs/sicoob.md), [C6 Empresas](docs/c6.md) e [Santander Empresas](docs/santander.md) explicam como configurar cada aplicação para consultar saldo e extrato da própria conta. Cada banco tem seus requisitos de habilitação e seu ambiente de testes.

Os três conectores têm testes locais com respostas simuladas. A homologação com credenciais bancárias está pendente. No Santander, o guia registra uma divergência entre o Swagger e a coleção Postman que precisa ser conferida nessa etapa.

| Provedor | Acesso | Cobertura |
| --- | --- | --- |
| `file` | Arquivo OFX/CSV exportado pelo titular | Lançamentos e saldo quando informado no OFX |
| `sandbox` | Local, sem credenciais bancárias | Contas e transações fictícias |
| `efi` | Aplicação Efí habilitada | Saldo, Pix recebidos e suas devoluções |
| `inter` | Aplicação Inter Empresas PJ habilitada | Saldo disponível e extrato da conta |
| `sicoob` | Aplicação do cooperado habilitada | Saldo e extrato mensal |
| `c6` | Aplicação C6 Empresas PJ habilitada | Saldo disponível e extrato |
| `santander` | Aplicação Santander Empresas habilitada | Saldo disponível e lançamentos efetivados |

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

No sandbox, a resposta traz a conexão e `authorization_url`. O link vence em dez minutos e só pode ser usado uma vez. A aprovação enfileira a primeira sincronização. O consentimento simulado dura 90 dias, contados da criação da conexão; esse prazo não pretende reproduzir a regra de um banco. Nos conectores diretos, a conexão usa `api_credentials`, é criada ativa após verificar o acesso e não retorna link de autorização.

O [exemplo Go](examples/connect/main.go) mostra o fluxo completo. O contrato HTTP está em [docs/openapi.json](docs/openapi.json), e os detalhes de paginação, erros e estados em [docs/api.md](docs/api.md).

| Método | Caminho | Uso |
| --- | --- | --- |
| `POST` | `/v1/imports?customer_id=...&format=ofx` | Importar extrato local |
| `GET` | `/v1/connections/{id}/status` | Ver progresso, contagens e datas dos lançamentos |
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

Os valores monetários usam centavos inteiros. As importações são atômicas e fazem upsert por identificador estável do provedor. As respostas de leitura já recebidas ficam temporariamente no SQLite, vinculadas ao job. A retomada mantém o período solicitado e reaproveita essas respostas; tokens e headers não entram nesse armazenamento. Os registros temporários são removidos ao concluir, falhar definitivamente, expirar ou revogar. O resultado de uma tentativa antiga é rejeitado, mesmo se chegar depois da nova tentativa.

Revogar uma conexão cancela seus jobs e apaga contas e transações na mesma transação. Os endpoints também conferem o vencimento do consentimento antes de liberar dados, sem depender da próxima execução do worker.

A [arquitetura](docs/architecture.md) explica os limites dos pacotes e os motivos dessas escolhas. O guia de [provedores](docs/providers.md) descreve o contrato que uma futura integração precisa cumprir.

## Desenvolvimento

```sh
make check     # testes com detector de corrida, go vet e formatação
make build     # bin/ranjit, sem CGO
```

Os testes usam SQLite em diretórios temporários e exercitam os handlers HTTP reais com transporte em memória. Cobrem consentimento, autenticação, paginação, isolamento dos IDs entre conexões, rollback, repetição de sincronização, concorrência, expiração e retomada após reinício.

Antes de operar fora da máquina local, leia [operação](docs/operations.md). A versão atual tem uma chave por instalação e banco sem criptografia em repouso. A configuração escolhe um provedor por processo: sandbox, Efí, Inter, Sicoob, C6 ou Santander. Cada conector direto acessa uma conta por instalação. Webhooks, múltiplos operadores, outros produtos bancários e PostgreSQL ficam para mudanças com casos de uso definidos.

Contribuições: [CONTRIBUTING.md](CONTRIBUTING.md). Licença: [MIT](LICENSE).
