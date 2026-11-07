# Efí

O primeiro conector direto consulta a própria conta Efí por uma aplicação criada pelo titular. A modalidade Efí Pro atende empreendedores sem CNPJ e oferece integração por API; a disponibilidade da conta e das permissões é definida pela Efí. [Conta Pro](https://sejaefi.com.br/efi-bank/efi-pro).

O conector lê saldo, Pix recebidos e devoluções concluídas desses recebimentos. Usa a API Pix da Efí, sem agregadora. Não implementa compartilhamento de dados entre bancos pelo Open Finance.

## Preparar o acesso

Na conta Efí, crie uma aplicação com apenas `gn.balance.read` e `pix.read`. Guarde o Client ID, o Client Secret e o certificado do ambiente escolhido. A [documentação de credenciais](https://dev.efipay.com.br/docs/api-pix/credenciais/) explica a emissão e a conversão de P12 para PEM.

O Ranjit recebe certificado e chave privada em PEM. Salve-os junto de um arquivo contendo somente o Client Secret:

```text
secrets/
  efi-client-secret
  efi-cert.pem
  efi-key.pem
```

Restrinja o diretório e os arquivos ao usuário do processo (`0700` e `0600`). Esse diretório e arquivos PEM/P12 estão ignorados pelo Git e pelo build Docker. Credenciais não são enviadas ao projeto, gravadas no SQLite ou retornadas pela API. O token OAuth fica em memória. Os arquivos locais não são criptografados automaticamente; use armazenamento protegido.

No `.env`, mantenha a chave do Ranjit e configure:

```dotenv
RANJIT_PROVIDER=efi
RANJIT_DATABASE=./data/efi-homologation.db
RANJIT_EFI_ENV=homologation
RANJIT_EFI_CLIENT_ID=client-id-da-aplicacao
RANJIT_EFI_CLIENT_SECRET_FILE=./secrets/efi-client-secret
RANJIT_EFI_CERT_FILE=./secrets/efi-cert.pem
RANJIT_EFI_KEY_FILE=./secrets/efi-key.pem
RANJIT_EFI_LOOKBACK_DAYS=90
```

Use as credenciais de homologação para esse ambiente. Para dados reais, use `RANJIT_EFI_ENV=production`, os arquivos e Client ID de produção e um banco local separado, como `./data/efi-production.db`.

Execute `make run` e, em outro terminal na mesma pasta, `make example`. O exemplo seleciona a instituição configurada, cria a conexão e imprime os dados depois da sincronização. Com Efí, não existe link de consentimento simulado. O acesso bancário vem da aplicação e das permissões configuradas pelo titular na Efí.

Em contêiner, monte os três arquivos somente para leitura, garanta que o usuário `65532` possa lê-los e ajuste as variáveis para os caminhos internos. O Compose padrão serve ao sandbox local e não monta credenciais bancárias.

## Pela API

`GET /v1/institutions` retorna `efi`, `connection_mode: api_credentials`, a indicação de homologação e a cobertura de dados. Envie para `POST /v1/connections`:

```json
{"customer_id":"local-user","institution_id":"efi"}
```

Antes de criar o registro, o servidor verifica as credenciais, os dois escopos e a consulta de saldo. A resposta 201 contém a conexão `active` e `authorization_mode: api_credentials`. Os campos `authorization_url` e `authorization_expires_at` ficam ausentes. A primeira sincronização entra na fila automaticamente.

O POST autoriza a coleta local por 90 dias, em `consent_expires_at`. Esse é um limite do Ranjit, não a validade das credenciais ou um consentimento registrado no Open Finance.

Esta versão usa uma aplicação Efí por instalação. O operador da chave Ranjit controla o acesso à conta configurada; `customer_id` não escolhe outra conta Efí. Uma conexão fica vinculada ao Client ID e ao ambiente. Alterar qualquer um exige criar outra conexão, evitando misturar contas ou homologação com produção. Renovar certificado ou Client Secret da mesma aplicação exige reiniciar o processo.

## Cobertura e limites

O saldo vem do [endpoint de saldo](https://dev.efipay.com.br/docs/api-pix/endpoints-exclusivos-efi/). Os lançamentos vêm da [listagem de Pix recebidos](https://dev.efipay.com.br/docs/api-pix/gestao-de-pix/), incluindo as devoluções liquidadas informadas nessa resposta. A API Extratos CNAB é outro produto e não está implementada.

Cada sincronização relê a janela configurada, de 1 a 90 dias, com fim fixo para todas as páginas. O limite local é de 100 páginas e 10 mil recebimentos por sincronização. Se a paginação mudar, houver falha ou o limite for excedido, nenhum dado daquele lote é gravado. A execução tem o timeout de 20 segundos do worker.

O ID EndToEnd identifica cada recebimento. Devoluções concluídas entram como débitos com IDs próprios. Devoluções pendentes ou não realizadas não entram no histórico. Só são atualizadas devoluções de Pix cujo recebimento original ainda aparece na janela consultada. `posted_on` usa a data UTC informada pela Efí.

Esse histórico é parcial: não inclui Pix enviados, compras no cartão, boletos pagos, TED ou todos os encargos da conta. Não reconstrua o saldo pela soma dessas transações. Consulte `data_coverage` e use o saldo informado pelo banco.

## Desconectar e testar

`DELETE /v1/connections/{id}` interrompe a coleta daquela conexão e apaga seus dados locais. Para revogar a credencial bancária, desabilite a aplicação na Efí. O DELETE do Ranjit não altera a configuração da conta bancária.

Os testes locais exercitam autenticação, configuração de mTLS, renovação e cache de token, paginação, precisão monetária, devoluções, falhas e o fluxo API → worker → SQLite. As respostas do banco são simuladas. A integração ainda precisa ser validada contra uma conta Efí habilitada; não houve consulta a uma conta real durante a implementação.
