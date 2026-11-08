# Pedidos repetidos, notificações e troca de chaves

## Criar uma conexão sem duplicar

Crie uma chave aleatória por operação, salve-a no seu backend e reaproveite-a se a chamada perder a resposta:

```go
result, err := client.ConnectWithKey(ctx, ranjit.ConnectRequest{
    CustomerID:    authenticatedUserID,
    InstitutionID: institutionID,
}, operationKey) // UUID ou outro valor aleatório persistido pela aplicação
```

Na API HTTP, envie `Idempotency-Key`. São aceitos de 16 a 128 caracteres ASCII visíveis, sem espaços. A chave vale por 24 horas desde a criação e pertence à instalação inteira: use um valor diferente por operação, inclusive entre usuários. O Ranjit armazena hashes da chave e do pedido.

Mesmo pedido e chave retornam `201` com a mesma conexão e seu estado atual. Pedidos concorrentes só criam uma conexão e um primeiro job. Alterar `customer_id` ou `institution_id` usando a mesma chave retorna `409 idempotency_conflict`. Falhas antes do commit não reservam a chave. Depois de 24 horas, reutilizá-la pode criar outra conexão; guarde o ID recebido no seu backend.

No sandbox, o mesmo link é recuperável enquanto estiver pendente e dentro dos dez minutos originais. O token é derivado da chave enviada e do ID da conexão; somente hashes são salvos. Aprovar, negar, revogar ou deixar vencer o link impede sua recuperação. Repetir a criação não renova consentimento nem reativa uma conexão.

Sem o cabeçalho, `client.Connect` mantém o comportamento anterior: cada chamada cria uma conexão. O SDK não repete POSTs automaticamente. A idempotência descrita aqui se aplica apenas à criação de conexões; importações e solicitações de sincronização têm suas próprias regras de deduplicação.

## Receber notificações

Configure no serviço:

```dotenv
RANJIT_WEBHOOK_URL=https://seu-backend.example/webhooks/ranjit
RANJIT_WEBHOOK_SECRET=seu-segredo-aleatorio-com-pelo-menos-32-caracteres
```

Use um segredo novo e diferente da chave da API. Para configuração pessoal em `config.json`, os campos são `webhook_url` e `webhook_secret`. Reinicie o serviço ao alterar essas opções. HTTP só é aceito em loopback para desenvolvimento; a URL não aceita credenciais, query ou fragmento. Docker exige um endereço alcançável de dentro do container.

O Ranjit envia `sync.succeeded` após publicar o lote completo e `sync.failed` quando o job falha definitivamente. Tentativas intermediárias, cancelamento de job e importação de arquivo não geram eventos de sincronização. Habilitar webhooks não cria eventos retroativos. Os comandos locais também gravam os eventos quando configurados; `ranjit serve` precisa estar em execução para entregá-los.

Mudanças efetivas de conexão geram eventos na mesma transação do estado:

| Evento | Quando ocorre | `data.status` |
| --- | --- | --- |
| `connection.activated` | Criação com acesso direto ou aprovação do sandbox | `active` |
| `connection.denied` | Recusa no sandbox | `denied` |
| `connection.paused` | Pausa de uma conexão direta | `paused` |
| `connection.expired` | Prazo local vencido, detectado pelo worker | `expired` |
| `connection.renewed` | Renovação do acesso direto | `active` |
| `connection.revoked` | Revogação | `revoked` |

Esses eventos não têm `job_id`. Incluem `connection_id`, `customer_id` e `status`, exceto a revogação, que conserva apenas `connection_id` e `status`. Uma pausa ou revogação já aplicada não gera outro evento; repetir a criação com a mesma chave também não. Uma nova renovação que estende o prazo é outra mudança efetiva.

```json
{
  "id": "evt_...",
  "type": "sync.succeeded",
  "created_at": "2026-11-08T12:00:00Z",
  "data": {
    "job_id": "job_...",
    "connection_id": "con_...",
    "customer_id": "user-42"
  }
}
```

Falhas incluem `data.error_code`. Não enviamos saldos, transações ou credenciais no evento. Consulte a API usando a chave do seu backend quando precisar dos dados.

O cabeçalho `Ranjit-Event-ID` contém o ID. `Ranjit-Signature` usa `t=<segundos Unix>,v1=<HMAC-SHA256 hexadecimal>`, calculado sobre `timestamp + "." + corpo original`. Use `ranjit.VerifyWebhook(body, signature, secret, time.Now())`; não reconstrua o JSON antes de verificar. O verificador aceita até cinco minutos de diferença no relógio. Mantenha o relógio do servidor sincronizado e rejeite cabeçalhos de assinatura duplicados.

O [receptor Go executável](../examples/webhook/) mostra assinatura, limite de corpo e deduplicação persistente. Responda com `2xx` depois de salvar o evento. Deduplicate pelo ID em uma restrição única no seu banco; a assinatura válida não impede uma segunda entrega legítima do mesmo evento.

## Entregas e recuperação

A fila fica no mesmo SQLite/PostgreSQL, gravada na mesma transação dos dados. O envio ocorre em um worker separado, com timeout de dez segundos. Redirecionamentos não são seguidos. Qualquer resposta fora de `2xx` ou erro de transporte agenda outra tentativa: até oito, com intervalos de 10, 20, 40, 80, 160, 320 e 640 segundos. Reinícios recuperam tentativas interrompidas após o lease de 30 segundos. Tentativas reclamadas depois de uma queda também contam no limite.

A entrega pode se repetir e não tem garantia de ordem entre eventos. ID e corpo permanecem iguais; cada tentativa recebe uma assinatura com horário novo. Consulte o estado atual da conexão pela API antes de tomar uma decisão que dependa dele. A fila conserva registros por sete dias desde a última alteração. Trocar a URL não redireciona eventos antigos para o novo destinatário; eles ficam associados à URL original até expirar. Desabilitar webhooks pausa as entregas. Revogar uma conexão apaga as entregas anteriores e deixa somente o aviso mínimo de revogação; uma requisição já enviada não pode ser desfeita. Restaurar um backup limpa a fila e não emite eventos de restauração.

```go
deliveries, err := client.WebhookDeliveries(ctx, "", 50)
// Depois de corrigir seu receptor, reenvie uma entrega com status failed:
err = client.RetryWebhook(ctx, eventID)
```

`GET /v1/webhooks/deliveries` usa `limit` e `cursor`, filtra pela URL configurada e mostra `queued`, `running`, `delivered` ou `failed`, tentativas, último status HTTP e próxima tentativa quando agendada. Não expõe URL nem segredo. `POST /v1/webhooks/deliveries/{id}/retry` retorna `202`, reinicia as tentativas e preserva o ID/corpo. Só aceita entregas `failed` da URL atual; caso contrário retorna `409 webhook_not_retryable`. Essas rotas exigem a chave da instalação.

## Trocar a chave da API

Na instalação pessoal ou no container instalado pelo script:

```sh
ranjit key rotate --grace 24h
# Docker: docker compose exec ranjit /ranjit key rotate --grace 24h
```

O comando imprime apenas a nova chave. Guarde-a como segredo no backend. O servidor lê a mudança sem reiniciar e aceita a chave anterior durante o prazo informado, de zero a 168 horas. O padrão é 24 horas. Outra rotação durante esse prazo é recusada para não derrubar clientes ainda em migração. `--grace 0` invalida imediatamente as duas chaves anteriores. Se o arquivo ficar inválido ou ilegível, a autenticação por chave é negada até corrigir a configuração. Sessões já abertas do painel local continuam com sua validade original; a rotação atua nas chaves da API.

Em instalações configuradas por ambiente, como o Orbit:

1. Gere uma nova chave aleatória com pelo menos 32 caracteres.
2. Coloque a chave atual em `RANJIT_API_KEY_PREVIOUS`, defina `RANJIT_API_KEY_PREVIOUS_EXPIRES_AT` em RFC3339 UTC e a nova em `RANJIT_API_KEY`. Faça o deploy do Ranjit com as três alterações juntas.
3. Atualize `RANJIT_API_KEY` no backend consumidor e recrie seu cliente Go ou reinicie o backend.
4. Depois da migração, remova as duas variáveis da chave anterior. Ela deixa de funcionar no prazo configurado mesmo sem novo deploy.

A configuração por ambiente é lida na inicialização. A janela de sobreposição permite migrar consumidores sem revogar a chave que eles ainda usam; não elimina a interrupção de um deploy com uma única réplica. O Ranjit continua representando um operador por instalação. A chave antiga e a nova têm as mesmas permissões.
