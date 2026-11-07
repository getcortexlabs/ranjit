# Inter Empresas

O conector lê saldo disponível e lançamentos da própria conta pela API Banking do Inter. O banco disponibiliza essas integrações apenas para contas PJ; contas PF e MEI não têm acesso. [Elegibilidade e funcionalidades](https://ajuda.inter.co/conta-digital-pessoa-juridica/o-inter-disponibiliza-alguma-api-para-minha-conta-digital-pj).

Esta é uma integração direta com o banco. Não habilita compartilhamento entre instituições pelo Open Finance. Cada instalação configura uma aplicação e uma conta Inter.

## Preparar o acesso

Crie uma integração no Internet Banking Inter Empresas e habilite somente a consulta de extrato e saldo da API Banking, correspondente ao escopo `extrato.read`. Ative a integração e obtenha Client ID, Client Secret, certificado e chave privada. Consulte o [portal do desenvolvedor](https://developers.inter.co/) para emissão e renovação das credenciais.

Salve o Secret em um arquivo contendo apenas seu valor. Certificado e chave devem estar em PEM, inclusive quando o arquivo fornecido pelo banco usa a extensão `.crt`:

```text
secrets/
  inter-client-secret
  inter-cert.pem
  inter-key.pem
```

Restrinja o diretório a `0700` e os arquivos a `0600`. `secrets/` está ignorado pelo Git e pelo build Docker. Os arquivos não são criptografados automaticamente. Tokens ficam em memória; o SQLite guarda apenas o vínculo da conexão, sem credenciais.

No `.env`, mantenha a chave da API Ranjit e configure:

```dotenv
RANJIT_PROVIDER=inter
RANJIT_DATABASE=./data/inter-sandbox.db
RANJIT_INTER_ENV=sandbox
RANJIT_INTER_CLIENT_ID=client-id-da-integracao
RANJIT_INTER_CLIENT_SECRET_FILE=./secrets/inter-client-secret
RANJIT_INTER_CERT_FILE=./secrets/inter-cert.pem
RANJIT_INTER_KEY_FILE=./secrets/inter-key.pem
RANJIT_INTER_ACCOUNT=123456789
RANJIT_INTER_LOOKBACK_DAYS=7
```

Informe a conta corrente da integração, incluindo o dígito, somente com números. O Ranjit sempre envia `x-conta-corrente`, mesmo para uma integração com uma única conta, para tornar a seleção explícita. `customer_id` agrupa registros locais e não escolhe outra conta bancária.

Use as credenciais do ambiente correspondente. Para consultar dados reais, selecione `RANJIT_INTER_ENV=production`, configure a aplicação de produção e use outro SQLite, como `./data/inter-production.db`. Os destinos são fixos: `cdpj-sandbox.partners.uatinter.co` e `cdpj.partners.bancointer.com.br`.

Execute `make run` e depois `make example` em outro terminal. O exemplo seleciona o banco configurado, cria a conexão e aguarda a sincronização. Para usar Docker, monte os três arquivos somente para leitura, com acesso para o usuário `65532`, e configure os caminhos dentro do contêiner. O Compose padrão roda o sandbox local e não configura esse conector.

## Conectar pela API

`GET /v1/institutions` retorna `inter`, `connection_mode: api_credentials` e a cobertura `available_balance` e `account_statement`. Crie a conexão:

```json
{"customer_id":"local-user","institution_id":"inter"}
```

`POST /v1/connections` verifica o acesso consultando o saldo. Se aceito, retorna a conexão ativa e agenda a primeira sincronização. Não há URL de autorização; o acesso foi habilitado na integração bancária. O campo `consent_expires_at` limita a coleta local a 90 dias. Esse prazo não altera as credenciais do Inter nem registra consentimento no Open Finance.

O vínculo inclui Client ID, ambiente e conta. Alterar qualquer um exige criar outra conexão. Para renovar Secret ou certificado da mesma integração, substitua os arquivos e reinicie o processo.

## Dados e sincronização

O saldo corresponde a `disponivel` em `/banking/v2/saldo`. Valores bloqueados e limite de crédito não são somados. Os lançamentos vêm de `/banking/v2/extrato/completo`, com crédito positivo e débito negativo. O identificador `idTransacao` permite repetir a coleta sem duplicar registros. A implementação segue o contrato publicado no [SDK oficial do Inter](https://github.com/inter-co/pj-sdk-java/tree/master/src/main/java/inter/sdk/banking).

A janela padrão cobre sete dias, incluindo o dia atual no fuso de São Paulo. Pode ser configurada entre 1 e 90 dias. Todas as páginas usam as mesmas datas. A data civil enviada pelo banco é preservada em `posted_on`. O histórico não inclui produtos que esse endpoint não retorna, como posições de investimento ou os itens de uma fatura de cartão.

Cada sincronização relê a janela inteira e atualiza os lançamentos conhecidos. O lote é atômico: paginação incompleta, IDs duplicados, valores inválidos ou falha intermediária impedem qualquer gravação daquela tentativa. O limite local é de 100 páginas com até 100 lançamentos por página e o worker permite 20 segundos por tentativa. Não há importação incremental por página; em contas com alto volume, reduza a janela.

Ao receber HTTP 429, a execução volta para a fila. O intervalo considera `Retry-After`, limitado a cinco minutos, e usa um minuto quando esse cabeçalho está ausente ou inválido. O agendamento fica no SQLite e sobrevive ao reinício. As demais falhas transitórias seguem as tentativas descritas na [arquitetura](architecture.md).

`DELETE /v1/connections/{id}` interrompe a coleta local e remove contas e transações daquela conexão. Para revogar o acesso bancário, desabilite a integração no Inter.

## Validação

Os testes usam um banco simulado e exercitam o fluxo HTTP → serviço → worker → SQLite, autenticação, renovação do token, paginação, créditos e débitos, precisão monetária, vínculo da conta e limites de consultas. A migração preserva dados de instalações com sandbox e Efí.

Ainda falta validar o conector com credenciais de uma conta Inter habilitada. Os testes locais não substituem essa homologação.
