# Backend Go

Exemplo de uma aplicação que usa o Ranjit como serviço. Importa um CSV e consulta transações para um cliente fixo, sem credenciais bancárias.

Para subir este backend, o serviço e o receptor de webhooks juntos, execute `./scripts/quickstart.sh` na raiz. O [quickstart](../quickstart/) gera os segredos e testa o fluxo. Para rodar sem Docker, siga abaixo.

1. Instale o Ranjit e execute `ranjit serve`.
2. Em outro terminal, obtenha a chave com `ranjit key` e configure `RANJIT_API_KEY` no ambiente do exemplo.
3. Na raiz do repositório:

```sh
export RANJIT_URL=http://localhost:8080
export APP_TOKEN="$(openssl rand -hex 32)"
go run ./examples/backend
```

Use o mesmo `APP_TOKEN` em outro terminal:

```sh
curl http://localhost:3000/statements \
  -H "Authorization: Bearer $APP_TOKEN" \
  -H 'Content-Type: text/csv' \
  --data-binary @examples/backend/statement.csv

curl http://localhost:3000/transactions \
  -H "Authorization: Bearer $APP_TOKEN"
```

Repita a consulta com `?cursor=NEXT_CURSOR` enquanto a resposta tiver `next_cursor`.

O token do exemplo é diferente da chave do Ranjit. O handler determina `customer_id` no servidor; campos enviados pelo cliente não substituem essa identidade. Em uma aplicação com vários usuários, substitua o cliente fixo pelo usuário autenticado da sua sessão e confira a propriedade dos IDs antes de consultar contas ou conexões. O exemplo escuta apenas em loopback e não implementa cadastro, login ou distribuição de tokens.

Para hospedagem, o código do SDK continua igual: altere `RANJIT_URL` e configure a chave como segredo do backend. Consulte [instalação em servidor](../../docs/server.md).

`APP_ADDR` altera o endereço de escuta; o padrão é `127.0.0.1:3000`. `GET /healthz` verifica a prontidão do serviço sem retornar dados financeiros. O exemplo configura até três tentativas para consultas GET, com dez segundos de orçamento; importações não são repetidas automaticamente.
