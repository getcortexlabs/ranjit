# Backend completo em um comando

Na raiz do repositório, com Docker e Compose v2 instalados:

```sh
./scripts/quickstart.sh
```

O comando compila e sobe o Ranjit, um backend Go e um receptor de webhooks. Cria três segredos diferentes em `examples/quickstart/.env`, com permissão `0600`, e verifica o fluxo inteiro: CSV pelo backend, autorização no sandbox, sincronização, paginação pelo SDK e recebimento das notificações assinadas. Não precisa de Go instalado nem de credenciais bancárias. A primeira execução demora mais por causa da compilação.

| Serviço | Endereço | Autenticação |
| --- | --- | --- |
| Backend de exemplo | `http://localhost:3000` | `APP_TOKEN` |
| API do Ranjit | `http://localhost:8080` | `RANJIT_API_KEY` |
| Receptor | loopback do container, porta 8081 | Assinatura com `RANJIT_WEBHOOK_SECRET` |

As duas portas publicadas escutam apenas no computador local. Os serviços compartilham a rede do container Ranjit para que o webhook de desenvolvimento use HTTP em loopback. Em produção, configure o endpoint HTTPS do seu backend e a rede da hospedagem conforme o [guia de servidor](../../docs/server.md).

Para consultar o backend:

```sh
set -a
. examples/quickstart/.env
set +a
curl -fsS http://localhost:3000/transactions \
  -H "Authorization: Bearer $APP_TOKEN"
```

O backend usa um cliente fixo, `example-customer`. Adapte a autenticação ao seu aplicativo e derive o cliente da sessão no servidor. A chave do Ranjit não deve chegar ao navegador. O receptor confirma a entrega somente depois de gravar o evento no SQLite, deduplicando pelo ID.

```sh
./scripts/quickstart.sh verify  # repete a verificação na instalação rodando
./scripts/quickstart.sh stop    # encerra os containers e mantém os volumes
./scripts/quickstart.sh         # recria os containers usando os mesmos dados
./scripts/quickstart.sh reset   # apaga os dois volumes de demonstração
```

Os segredos são preservados ao parar, reiniciar ou resetar. Os volumes `ranjit-quickstart_data` e `ranjit-quickstart_inbox` guardam, respectivamente, os dados do Ranjit e os eventos recebidos. Repetir o exemplo não duplica o CSV nem uma conexão de sandbox ainda ativa. Se modificar ou revogar manualmente a conexão da demonstração, use `reset` antes de repetir o teste.

Se as portas 3000 ou 8080 já estiverem ocupadas, encerre a outra instalação antes de iniciar. Para trabalhar sem Docker, use o [mesmo backend Go](../backend/) com o binário do Ranjit e o [receptor Go](../webhook/).
