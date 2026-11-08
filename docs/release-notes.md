Integrações de backend com recuperação de pedidos, notificações assinadas e troca de chaves.

- `Idempotency-Key` e `client.ConnectWithKey`: pedidos repetidos recuperam a mesma conexão por 24 horas, inclusive depois de reiniciar.
- Webhooks `sync.succeeded` e `sync.failed` com HMAC-SHA256, fila persistente, oito tentativas automáticas e reenvio pela API/SDK.
- Rotação da chave com sobreposição: `ranjit key rotate --grace 24h` na configuração pessoal; chave anterior e expiração por ambiente em servidores.
- Receptor Go com verificação de assinatura e caixa de entrada SQLite que deduplica eventos.
- Contrato HTTP, exemplos e documentação de instalação atualizados.

As novas tabelas são criadas automaticamente no SQLite ou PostgreSQL existente. Faça backup antes de atualizar; as migrações não têm downgrade automático. Webhooks são opcionais, sem eventos retroativos. Pedidos sem `Idempotency-Key` mantêm o comportamento anterior.

Os conectores bancários continuam experimentais. A validação com uma conta real depende de acesso habilitado à API do banco. A integração com dados simulados e importação de extratos funciona sem essas credenciais.

Configuração e exemplos: https://github.com/getcortexlabs/ranjit/blob/main/docs/backend-reliability.md
