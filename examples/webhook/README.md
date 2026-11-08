# Receber notificações no backend

Este exemplo verifica a assinatura e grava cada evento uma única vez em SQLite antes de responder `204`. Reiniciar o exemplo preserva a caixa de entrada em `webhook-inbox.db`. Adapte a tabela ao banco do seu backend e processe os registros de forma transacional. Efeitos externos, como enviar outro pedido HTTP, precisam de sua própria chave de idempotência.

Na raiz do repositório, com Go instalado:

```sh
make setup
# Adicione ao .env:
# RANJIT_WEBHOOK_URL=http://127.0.0.1:8081/webhooks/ranjit
# RANJIT_WEBHOOK_SECRET=<outro segredo aleatório, com pelo menos 32 caracteres>
set -a
. ./.env
set +a
go run ./examples/webhook
```

Em outro terminal, rode `make run`. Em um terceiro, `make example` cria uma conexão no sandbox. Abra o link exibido e aprove os dados fictícios. A primeira sincronização enviará `sync.succeeded` para o exemplo. Não é necessário habilitar um banco para esse teste.

O endpoint pode receber o mesmo ID várias vezes, inclusive depois de responder com sucesso se a resposta se perder. A assinatura muda a cada tentativa; o ID e o corpo do evento permanecem iguais. O exemplo só recebe eventos, não consulta nem altera dados financeiros. Use `client.Accounts` e `client.Transactions` ao consumir a caixa de entrada se sua aplicação precisar atualizar seu próprio histórico.

Em produção, sirva o endpoint por HTTPS e use o mesmo `RANJIT_WEBHOOK_SECRET` no Ranjit e no receptor. O segredo de assinatura é separado da chave da API. Veja [retries, inspeção e recuperação de entregas](../../docs/backend-reliability.md).
