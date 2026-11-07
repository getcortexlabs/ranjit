# Provedores

O contrato atual cobre ingestão:

```go
type Source interface {
    Institutions() []ranjit.Institution
    Fetch(context.Context, ranjit.Connection) (Snapshot, error)
}
```

`Fetch` devolve contas e transações normalizadas. Precisa respeitar cancelamento e timeout, manter identificadores estáveis e entregar todas as páginas necessárias para aquele lote. O worker só confirma a sincronização depois de gravar o lote inteiro. Não há checkpoint por página nesta versão.

`Snapshot.Validate` rejeita IDs vazios ou repetidos no lote, referências a contas ausentes, datas inválidas, moedas não suportadas e valores fora do intervalo aceito. O teste do provedor deve usar fixtures saneadas, nunca respostas contendo dados reais de clientes.

## Uma integração real exige mais que Fetch

O único provedor registrado é `sandbox`. A configuração, a criação de conexões e o schema rejeitam outros provedores intencionalmente. A página `/sandbox/authorize` não deve ser reaproveitada como callback de um banco.

Um adaptador real precisa de uma mudança que implemente e teste o fluxo oferecido pela operação habilitada: início da autorização, vínculo entre usuário e consentimento, callback autenticado, troca e renovação de credenciais, expiração, revogação remota e os estados de falha. O conjunto exato depende da integração contratada.

Credenciais não têm armazenamento nesta versão. Ao introduzi-las, defina criptografia, referência de chave, rotação e política de remoção antes de persisti-las. O mesmo vale para revogação: a operação local já impede novas leituras e importações, mas a revogação remota precisará de execução persistente e retentativas próprias.

Mantenha URLs remotas na configuração do operador, valide os destinos e use clientes HTTP com limites de resposta, timeouts e política explícita de redirects. Não transforme URLs enviadas por usuários em destinos de chamadas autenticadas.

Para a primeira contribuição de provedor, abra uma issue com a documentação pública da integração, o escopo de dados, o modelo de consentimento e o mecanismo de credenciais. Isso permite decidir a fronteira com um contrato concreto.
