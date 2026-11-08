Integração de backend com instalação demonstrável, recuperação de pedidos, notificações assinadas e diagnóstico remoto.

- `Idempotency-Key` e `client.ConnectWithKey`: pedidos repetidos recuperam a mesma conexão por 24 horas, inclusive depois de reiniciar.
- Webhooks `sync.succeeded` e `sync.failed` com HMAC-SHA256, fila persistente, oito tentativas automáticas e reenvio pela API/SDK.
- Rotação da chave com sobreposição: `ranjit key rotate --grace 24h` na configuração pessoal; chave anterior e expiração por ambiente em servidores.
- Receptor Go com verificação de assinatura e caixa de entrada SQLite que deduplica eventos.
- Quickstart em um comando: Ranjit, backend Go e receptor com chaves separadas, dados persistentes e verificação de CSV e sandbox.
- Eventos de ativação, recusa, pausa, expiração, renovação e revogação na mesma transação do estado. Revogar limpa eventos anteriores e deixa um aviso mínimo.
- `ranjit doctor --remote --json` verifica API, autenticação, banco e workers sem configuração local.
- Métricas autenticadas em `/metrics` e diagnóstico das filas, com distinção entre worker ocioso e sem progresso.
- Iteradores no SDK Go para buscar páginas sob demanda; retentativas opcionais de GET com limite total, cancelamento e `Retry-After`.
- Testes de atualização a partir do binário e da imagem publicados da v0.3.0, incluindo SQLite/PostgreSQL, recuperação de jobs, backup, restauração e rollback.
- Contrato HTTP, exemplos e documentação de instalação atualizados.

As novas tabelas e a migração dos eventos são aplicadas automaticamente no SQLite ou PostgreSQL existente. Faça backup antes de atualizar; as migrações não têm downgrade automático. Webhooks são opcionais, sem eventos retroativos. Pedidos sem `Idempotency-Key` mantêm o comportamento anterior. Retentativas do SDK continuam desabilitadas por padrão e nunca repetem mutações automaticamente.

Os conectores bancários continuam experimentais. A validação com uma conta real depende de acesso habilitado à API do banco. A integração com dados simulados e importação de extratos funciona sem essas credenciais.

Configuração e exemplos: https://github.com/getcortexlabs/ranjit/blob/main/docs/backend-reliability.md
