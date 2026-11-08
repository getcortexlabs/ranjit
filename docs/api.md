# API

O contrato está em [openapi.json](openapi.json). As respostas usam JSON, exceto a página HTML de autorização e o DELETE bem-sucedido, que não tem corpo.

## Chave e cliente

Use `Authorization: Bearer <chave>` em todas as rotas `/v1`. A instalação representa um único operador. A chave não distingue clientes finais; o backend que chama o Ranjit deve validar qual usuário pode acessar cada conexão. Use a chave apenas no backend de integrações. O painel embutido troca um link temporário por uma sessão local; o cookie também autentica `/v1`, com verificação de origem para mutações. A chave nunca vai na URL.

`GET /healthz` e `GET /readyz` não exigem autenticação. O primeiro verifica que o processo responde; o segundo também consulta o banco.

## Conexões diretas

Nos conectores diretos, `institution_id` é o nome do provedor selecionado: `efi`, `inter`, `sicoob`, `c6` ou `santander`. A criação verifica a aplicação configurada localmente, cria a conexão ativa e enfileira a primeira sincronização. A resposta traz apenas `connection`; não há URL de consentimento. `authorization_mode: api_credentials` identifica esse fluxo. O campo `consent_expires_at` representa o limite local de coleta de 90 dias, independente da validade bancária.

`GET /v1/institutions` informa `connection_mode`, `sandbox` e `data_coverage`. Para Efí, o histórico cobre recebimentos Pix e suas devoluções concluídas; não representa o extrato completo. Consulte o [guia Efí](efi.md). O Inter cobre saldo disponível e extrato da conta PJ; veja o [guia Inter](inter.md). Os contratos e limites de [Sicoob](sicoob.md), [C6](c6.md) e [Santander](santander.md) estão nos respectivos guias.

## Consentimento simulado

`POST /v1/connections` recebe `customer_id` e `institution_id`. O primeiro é o ID interno da aplicação, com até 128 bytes. Use um identificador opaco. O segundo deve vir de `GET /v1/institutions`.

No sandbox, a resposta 201 contém `connection`, `authorization_url` e `authorization_expires_at`. O link é uma credencial temporária. Apenas a aplicação que criou a conexão e a pessoa autorizando devem recebê-lo. Envie `Idempotency-Key` (16–128 caracteres ASCII visíveis, sem espaços) para recuperar a mesma conexão em pedidos repetidos durante 24 horas. Outro conteúdo com a mesma chave retorna `409 idempotency_conflict`. Sem a chave, cada POST cria outra conexão. Veja o [contrato de repetição e recuperação do link](backend-reliability.md).

`GET /sandbox/authorize?token=...` exibe a decisão. `POST /sandbox/authorize` recebe `token` e `decision=approve|deny` como `application/x-www-form-urlencoded`. O token dispensa a chave da API apenas nessa rota. Uma decisão bem-sucedida retorna a conexão em JSON. Reutilizar ou deixar vencer o link resulta em 410.

| Estado da conexão | Significado |
| --- | --- |
| `pending` | Aguardando decisão no sandbox |
| `active` | Consentimento válido; pode sincronizar e consultar |
| `denied` | Compartilhamento recusado |
| `expired` | Link ou consentimento vencido |
| `revoked` | Conexão revogada pela aplicação |
| `paused` | Coleta direta pausada, histórico disponível |

Para sandbox vencido, crie outra conexão. Conexões diretas aceitam `POST /v1/connections/{id}/pause` (204) e `/renew` (200, conexão). Renovar verifica a mesma identidade de credenciais, reinicia o prazo local de 90 dias e enfileira uma sincronização; não altera o consentimento ou a habilitação no banco. Uma conexão ativa pode estar temporariamente sem dados enquanto a primeira sincronização roda.

## Jobs

`POST /v1/connections/{id}/sync` retorna 202 com o job e seu endereço no cabeçalho `Location`. Se já existe execução pendente ou em andamento, retorna o mesmo job. Acompanhe `GET /v1/jobs/{id}`.

Os estados são `queued`, `running`, `succeeded`, `failed` e `cancelled`. `attempts` conta reivindicações da execução, incluindo retomadas após queda. `error_code` pode ser `provider_unavailable`, `invalid_provider_data`, `storage_unavailable`, `lease_exhausted` ou `provider_credentials`. Não contém mensagens brutas do provedor.

O agendador verifica conexões periodicamente. O intervalo padrão é uma hora desde a última atualização de um job terminal. Pedidos explícitos podem antecipar a próxima sincronização.

## Listas

Conexões, contas e transações usam `limit` de 1 a 100, com padrão 50. A resposta tem `data` e, quando houver outra página, `next_cursor`. Envie esse cursor na chamada seguinte com os mesmos filtros. O cursor é opaco para o consumidor.

A ordenação é por ID, não por data. A paginação não representa um snapshot congelado: se uma sincronização acrescentar registros durante a leitura, refaça a listagem para obter uma visão atualizada. `GET /v1/connections` exige `customer_id` e não oferece listagem global.

## Dinheiro e datas

```json
{
  "amount_minor": -4590,
  "currency": "BRL"
}
```

Isso representa um débito de R$ 45,90. Não use `float` para armazenar esse valor. A API aceita inteiros de `-9007199254740991` a `9007199254740991`. Apenas BRL está implementado. `posted_on` é uma data civil; os demais instantes usam RFC 3339 em UTC.

## Revogação

`DELETE /v1/connections/{id}` retorna 204 depois de revogar localmente, invalidar o link, cancelar jobs ativos e apagar contas e transações. Repetir para uma conexão já revogada também retorna 204. O registro da conexão e os jobs permanecem para consulta. Para os conectores diretos, revogar a credencial bancária exige desabilitar a aplicação no respectivo banco; o DELETE encerra somente o acesso local.

Conexões diretas em `paused` ou `expired` permitem consultar o histórico, mas não sincronizar. No sandbox, a expiração bloqueia leitura imediatamente e o worker remove os dados. Conexões revogadas retornam 409 para contas; transações de contas já apagadas retornam 404.

## Erros

```json
{
  "error": {
    "code": "inactive_connection",
    "message": "connection is not active"
  }
}
```

| HTTP | Código | Causa |
| --- | --- | --- |
| 400 | `invalid_request` | JSON, campos, paginação ou formulário inválidos |
| 401 | `unauthorized` | Chave ausente ou incorreta |
| 403 | `invalid_origin` | Origem do POST de autorização difere da URL configurada |
| 404 | `not_found` | Recurso inexistente |
| 409 | `inactive_connection` | Conexão sem consentimento ativo |
| 409 | `idempotency_conflict` | Chave de repetição usada com outro pedido |
| 409 | `webhook_not_retryable` | Entrega não está em falha ou pertence a outro destino |
| 410 | `invalid_authorization` | Token inválido, consumido ou vencido |
| 502 | `provider_credentials` | Credenciais, escopos ou vínculo da aplicação inválidos |
| 502 | `provider_unavailable` | Banco temporariamente indisponível |
| 500 | `internal_error` | Falha interna |

O corpo de entrada é limitado a 16 KiB; JSON com campos desconhecidos ou conteúdo adicional é rejeitado com 400. Respostas de erro geradas pelo roteador HTTP, como método não permitido, podem ser texto simples. `X-Request-ID` permite localizar a chamada nos logs sem incluir dados financeiros.

## Importação e acompanhamento

`POST /v1/imports` recebe um corpo OFX/CSV de até 8 MiB. Parâmetros: `customer_id`, `format`, `account` (obrigatório em CSV), `decimal` e `date_format`. O [guia de importação](imports.md) descreve formatos e deduplicação. Essas conexões usam provedor `file`, modo `file_import`, não têm `consent_expires_at` e não aceitam sincronização bancária. A revogação funciona pelo mesmo endpoint.

`GET /v1/connections/{id}/status` retorna `connection`, `latest_job` quando houver, `accounts`, `transactions` e as datas mínima e máxima dos lançamentos presentes. `requests_completed` em um job conta respostas bancárias salvas, incluindo saldo. Não indica quantas requisições ainda faltam. A última sincronização só avança quando o lote completo é publicado.

`Account.balance` pode ser `null` quando o arquivo não informa saldo; não interprete como zero. `balance_at` indica quando o saldo foi lido ou a data do saldo no OFX. `type` também pode ser `credit_card`.

Com perfis, os IDs retornados por `/v1/institutions` têm o formato `perfil:instituição`; use o valor retornado, sem montá-lo por conta própria. Cada instituição informa `profile` e `validation` (`experimental` para conectores bancários; `simulated` no sandbox). Credenciais continuam fora da API.


## Prévia, mapeamento e busca

`POST /v1/imports/preview` recebe o mesmo arquivo e parâmetros de `/v1/imports`. Retorna `result`, `from`, `through` e `sample` (até 20 itens com `posted_on`, `description` e `amount`). Não persiste conexão nem lançamentos. Em ambos os endpoints, `new`, `updated` e `unchanged` contam o resultado por ID; a confirmação recalcula tudo em uma transação, pois o banco pode ter mudado desde a prévia.

CSV aceita `delimiter` (`,` / `;` / `tab`), `date_column`, `description_column`, `amount_column` e `id_column`, usando os nomes do cabeçalho. Colunas vazias mantêm a detecção automática. `POST /v1/imports/columns?delimiter=...` recebe o CSV e devolve `columns`, sem gravar. Reinterpretar um arquivo já importado com valores diferentes é recusado.

`GET /v1/transactions?customer_id=personal&q=mercado&from=2026-11-01&through=2026-11-30` busca em todas as contas desse agrupamento; `account_id` restringe uma conta. Datas são inclusivas. A busca textual trata `%` e `_` como caracteres comuns; a conversão de caixa do SQLite é limitada a ASCII. A ordenação é por `posted_on` e ID, decrescente, com `limit` e `cursor`.

`GET /v1/diagnostics` informa versão, perfis indisponíveis e validade de certificados. Não retorna caminhos, sujeitos dos certificados ou credenciais. A rota existe no servidor do binário; bibliotecas que montam seu próprio handler podem fornecê-la com `httpapi.Diagnostics`.

## Notificações e rotação

`GET /v1/webhooks/deliveries` lista eventos da URL configurada com a paginação padrão. `POST /v1/webhooks/deliveries/{id}/retry` reenfileira uma entrega em `failed` e responde `202`, sem corpo. O SDK oferece `WebhookDeliveries`, `RetryWebhook` e `VerifyWebhook`.

A assinatura, os prazos de repetição e a troca de chave com sobreposição estão no [guia de integração confiável](backend-reliability.md). Esses recursos usam o mesmo banco e não exigem serviços adicionais. A revogação também apaga as entregas da conexão, sem desfazer requisições já enviadas.
