# Contribuir

Para correções pequenas, envie um PR com o problema, a mudança e como você verificou o resultado. Para conectores ou mudanças no modelo de dados, abra uma issue antes: consentimento e identidade dos registros afetam o restante do sistema.

```sh
make check
make build
```

Use nomes de código em inglês e `gofmt`. Comentários devem explicar restrições ou decisões que não são óbvias lendo a função. Uma dependência nova precisa resolver um problema concreto.

Mudanças em comportamento financeiro, consentimento ou fila precisam de testes que reproduzam o caso. Os testes de armazenamento usam SQLite real; não substitua as transações por mocks. Novas migrações são arquivos novos em `internal/store/migrations`.

Ao alterar a API, atualize `docs/openapi.json`, o SDK e os exemplos afetados. Use dados fictícios em fixtures e relatos de erro. Não envie `.env`, chaves, arquivos de banco ou respostas com dados pessoais.

PRs são distribuídos sob a [licença MIT](LICENSE) do projeto.
