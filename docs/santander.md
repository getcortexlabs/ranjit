# Santander Empresas

Consulta saldo e lançamentos da própria conta pelo produto Bank Account Information. Requer aplicação Santander Empresas habilitada e certificado mTLS. O conector fixa o banco Santander e uma conta; não consulta contas de outras instituições por Open Finance.

## Configuração

Cadastre a aplicação e o certificado no [Portal Developers](https://developer.santander.com.br/). Guarde o Client Secret em um arquivo contendo somente seu valor, além do certificado e da chave privada em PEM. Use `0700` em `secrets/` e `0600` nos arquivos.

```dotenv
RANJIT_PROVIDER=santander
RANJIT_DATABASE=./data/santander-sandbox.db
RANJIT_SANTANDER_ENV=sandbox
RANJIT_SANTANDER_CLIENT_ID=client-id-da-integracao
RANJIT_SANTANDER_CLIENT_SECRET_FILE=./secrets/santander-client-secret
RANJIT_SANTANDER_CERT_FILE=./secrets/santander-cert.pem
RANJIT_SANTANDER_KEY_FILE=./secrets/santander-key.pem
RANJIT_SANTANDER_ACCOUNT=0001.000000123456
RANJIT_SANTANDER_LOOKBACK_DAYS=7
```

O formato da conta é agência com quatro dígitos, ponto e conta com doze dígitos, incluindo o verificador. Preencha os zeros à esquerda. O exemplo é fictício. O vínculo da conexão inclui aplicação, ambiente e conta; `customer_id` não seleciona outra conta.

Execute `make run` e depois `make example`. Pela API, envie `{"customer_id":"local-user","institution_id":"santander"}` a `POST /v1/connections`. A consulta de saldo verifica o acesso antes da criação da conexão ativa. Para produção, troque o ambiente por `production`, use as credenciais correspondentes e outro SQLite.

## Contrato implementado

OAuth por mTLS em `/auth/oauth/v2/token`, formulário `client_credentials`; as consultas enviam Bearer e `X-Application-Key` com o Client ID. Os destinos são `trust-sandbox.api.santander.com.br` e `trust-open.api.santander.com.br`.

O saldo usa `availableAmount` e `availableAmountCurrency`. O extrato usa `transactionId`, `creditDebitType`, `amount`, `transactionCurrency` e `transactionDate`. Débitos já negativos preservam o sinal; débitos positivos são convertidos. Créditos negativos são rejeitados. Só são aceitos lançamentos identificados como `TRANSACAO_EFETIVADA`; estados desconhecidos interrompem o lote.

Há uma divergência nos documentos publicados pelo banco: o Swagger mostra a rota de extrato com agência e conta na query; a coleção Postman de produção e o guia mostram `/banks/{bank_id}/statements/{agencia.conta}`. O Ranjit segue a coleção Postman para as URLs e os parâmetros `_offset`, `_limit`, `initialDate` e `finalDate`, e o Swagger para os campos da resposta. **Essa combinação ainda precisa de homologação com uma aplicação habilitada.** Não há fallback automático entre rotas.

O offset começa em 1 e avança pelo número de registros recebidos. A resposta precisa trazer `_content` e `_pageable._moreElements`. Os limites locais são 100 páginas, 50 registros por página e 2 MiB por resposta. Página vazia com continuação, ID repetido ou metadados ausentes impedem a importação parcial. Os endpoints de grandes volumes não são usados: o modelo publicado para eles não fornece `transactionId`.

A janela local aceita de 1 a 90 dias, com padrão de sete dias em São Paulo. Tokens ficam em memória; 401 permite uma renovação e 429 adia o job. Cada tentativa tem o limite de execução de 20 segundos do worker.

Referências consultadas em 07/11/2026: [documentação atual, Swagger e coleções Postman](https://developer.santander.com.br/api/documentation/balance-and-statement), [visão geral do produto](https://developer.santander.com.br/api/overview/balance-and-statement). O guia antigo tem um [endereço histórico](https://developer.santander.com.br/sites/default/files/2024-02/user_guide_contas_saldo_extrato_api_v6.pdf), atualmente substituído pelo portal; para novos downloads use a página atual.

## Validação e operação

Os testes locais cobrem OAuth, cabeçalhos, offset, créditos e débitos, resposta incompleta, deduplicação e o fluxo até o SQLite. Não foram usadas credenciais reais nem foi concluída homologação bancária.

O prazo de 90 dias da conexão é uma restrição local. Revogar no Ranjit remove os dados coletados e interrompe a sincronização. Desabilite a aplicação no Santander para revogar o acesso bancário. Veja [operação](operations.md) para Docker e proteção dos arquivos.
