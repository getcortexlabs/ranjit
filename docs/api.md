# API

O contrato está em [openapi.json](openapi.json). As respostas usam JSON, exceto a página HTML de autorização e o DELETE bem-sucedido, que não tem corpo.

## Chave e cliente

Use `Authorization: Bearer <chave>` em todas as rotas `/v1`. A instalação representa um único operador. A chave não distingue clientes finais; o backend que chama o Ranjit deve validar qual usuário pode acessar cada conexão. Não envie a chave para JavaScript no navegador, aplicativos distribuídos ou URLs.

`GET /healthz` e `GET /readyz` não exigem autenticação. O primeiro verifica que o processo responde; o segundo também consulta o banco.

## Conexões diretas

Com `RANJIT_PROVIDER=efi`, envie `institution_id: efi`. Com `RANJIT_PROVIDER=inter`, envie `institution_id: inter`. A criação verifica a aplicação configurada localmente, cria a conexão ativa e enfileira a primeira sincronização. A resposta traz apenas `connection`; não há URL de consentimento. `authorization_mode: api_credentials` identifica esse fluxo. O campo `consent_expires_at` representa o limite local de coleta de 90 dias, independente da validade bancária.

`GET /v1/institutions` informa `connection_mode`, `sandbox` e `data_coverage`. Para Efí, o histórico cobre recebimentos Pix e suas devoluções concluídas; não representa o extrato completo. Consulte o [guia Efí](efi.md). O Inter cobre saldo disponível e extrato da conta PJ; veja o [guia Inter](inter.md).

## Consentimento simulado

`POST /v1/connections` recebe `customer_id` e `institution_id`. O primeiro é o ID interno da aplicação, com até 128 bytes. Use um identificador opaco. O segundo deve vir de `GET /v1/institutions`.

No sandbox, a resposta 201 contém `connection`, `authorization_url` e `authorization_expires_at`. O link é uma credencial temporária. Apenas a aplicação que criou a conexão e a pessoa autorizando devem recebê-lo. A criação não é idempotente: não repita automaticamente um POST cujo resultado ficou incerto; consulte as conexões do cliente antes de tentar novamente.

`GET /sandbox/authorize?token=...` exibe a decisão. `POST /sandbox/authorize` recebe `token` e `decision=approve|deny` como `application/x-www-form-urlencoded`. O token dispensa a chave da API apenas nessa rota. Uma decisão bem-sucedida retorna a conexão em JSON. Reutilizar ou deixar vencer o link resulta em 410.

| Estado da conexão | Significado |
| --- | --- |
| `pending` | Aguardando decisão no sandbox |
| `active` | Consentimento válido; pode sincronizar e consultar |
| `denied` | Compartilhamento recusado |
| `expired` | Link ou consentimento vencido |
| `revoked` | Conexão revogada pela aplicação |

Não há renovação de consentimento nesta versão. Crie outra conexão. Uma conexão ativa pode estar temporariamente sem dados enquanto a primeira sincronização roda.

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

Leituras de contas de uma conexão inativa retornam 409. Uma conta já apagada retorna 404 ao consultar suas transações. Expiração também bloqueia acesso imediatamente; a limpeza física de linhas ocorre no próximo passo do worker.

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
| 410 | `invalid_authorization` | Token inválido, consumido ou vencido |
| 502 | `provider_credentials` | Credenciais, escopos ou vínculo da aplicação inválidos |
| 502 | `provider_unavailable` | Banco temporariamente indisponível |
| 500 | `internal_error` | Falha interna |

O corpo de entrada é limitado a 16 KiB; JSON com campos desconhecidos ou conteúdo adicional é rejeitado com 400. Respostas de erro geradas pelo roteador HTTP, como método não permitido, podem ser texto simples. `X-Request-ID` permite localizar a chamada nos logs sem incluir dados financeiros.
