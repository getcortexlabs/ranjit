# SDK Go

```sh
go get github.com/getcortexlabs/ranjit@v0.4.0
```

Configure `RANJIT_URL` e `RANJIT_API_KEY` no ambiente do backend e crie um cliente com `ranjit.NewClientFromEnv()`. Também é possível usar `ranjit.NewClient(url, key)`. O SDK chama o serviço HTTP; não abre o banco nem executa os workers dentro do seu aplicativo.

## Paginar

Os métodos `Connections`, `Accounts`, `Transactions` e `SearchTransactions` retornam uma página e `NextCursor`. Para percorrer todas as páginas sem guardar tudo em memória:

```go
for transaction, err := range client.IterSearchTransactions(ctx, ranjit.TransactionFilter{
    CustomerID: authenticatedUserID,
    Limit:      100,
}) {
    if err != nil {
        return err
    }
    if err := saveTransaction(transaction); err != nil {
        return err
    }
}
```

Há também `IterConnections(ctx, customerID, limit)`, `IterAccounts(ctx, connectionID, limit)` e `IterTransactions(ctx, accountID, limit)`. Eles só buscam páginas durante a iteração. `break` interrompe sem buscar a próxima; cancelamento do contexto interrompe chamadas e esperas. Um cursor repetido pelo servidor retorna `ErrInvalidResponse` para evitar um laço infinito. Os filtros da consulta permanecem iguais entre páginas.

A paginação não é uma fotografia imutável do banco: sincronizações concorrentes podem alterar o conjunto. Use os IDs estáveis ao gravar transações no seu aplicativo.

## Repetir consultas após falhas temporárias

Por padrão, cada chamada faz uma tentativa. Configure antes de compartilhar o cliente entre goroutines:

```go
client.ReadRetries = ranjit.RetryPolicy{
    MaxAttempts:    3,
    MaxElapsedTime: 10 * time.Second,
    BaseDelay:      200 * time.Millisecond,
    MaxDelay:       2 * time.Second,
}
```

A política vale apenas para GET. Repete erros transitórios de transporte e HTTP 408, 429, 500, 502, 503 e 504. O intervalo cresce com variação aleatória e respeita `Retry-After` em segundos ou data HTTP. Se a espera pedida pelo servidor não couber no prazo restante, retorna a última falha sem antecipar a chamada. O prazo do contexto, o orçamento total e `HTTPClient.Timeout` continuam valendo. São permitidas até dez tentativas e cinco minutos de orçamento total.

POST e DELETE nunca são repetidos automaticamente. Para recuperar a criação de uma conexão após perder a resposta, reaproveite sua chave de operação com `ConnectWithKey`, conforme o [guia de idempotência](backend-reliability.md).

## Tratar erros

```go
if errors.Is(err, ranjit.ErrUnauthorized) {
    // Atualize a chave configurada no backend.
}
var problem *ranjit.APIError
if errors.As(err, &problem) {
    log.Printf("Ranjit status=%d code=%s request_id=%s",
        problem.StatusCode, problem.Code, problem.RequestID)
}
```

`APIError` preserva status HTTP, código e ID da requisição. `errors.Is` reconhece também `ErrRateLimited`, `ErrNotFound`, `ErrConflict`, `ErrInvalid`, `ErrConsent`, os erros de provedor, idempotência e reenvio de webhook. Respostas maiores que 1 MiB retornam `ErrResponseTooLarge`; JSON inválido ou conteúdo extra retornam `ErrInvalidResponse`. Erros de transporte podem ser inspecionados com `errors.As` e `errors.Is`.

`client.Health`, `client.Ready` e `client.Diagnostics` permitem consultar a saúde da instalação sem acesso ao banco. Não use disponibilidade do processo como confirmação de que a API bancária está acessível.
