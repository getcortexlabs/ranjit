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

A configuração seleciona `sandbox`, `efi`, `inter`, `sicoob`, `c6` ou `santander`. Os conectores [Efí](efi.md), [Inter](inter.md), [Sicoob](sicoob.md), [C6](c6.md) e [Santander](santander.md) implementam `CredentialSource`, que acrescenta `CredentialID()` e `Verify(ctx)` ao contrato de ingestão. Isso permite verificar o acesso instalado pelo titular e vincular cada conexão à aplicação correspondente. Cada conector cobre a própria conta. `data_coverage` descreve quais dados a integração entrega. A página `/sandbox/authorize` não deve ser reaproveitada como callback de um banco.

Um adaptador para compartilhamento pelo Open Finance precisa implementar e testar o fluxo oferecido pela operação habilitada: início da autorização, vínculo entre usuário e consentimento, callback autenticado, troca e renovação de credenciais, expiração, revogação remota e os estados de falha. O conjunto exato depende da integração contratada.

Os conectores bancários leem segredos e certificados de arquivos locais protegidos e mantêm tokens em memória. O carregamento desses arquivos e a configuração do transporte mTLS são compartilhados em `internal/provider/transport.go`; cada banco implementa seu contrato de autenticação e dados. Não há armazenamento de credenciais no banco. Para introduzir credenciais por usuário ou renovação persistida, defina criptografia, referência de chave, rotação e política de remoção antes de persisti-las. O mesmo vale para revogação: a operação local já impede novas leituras e importações, mas a revogação remota precisará de execução persistente e retentativas próprias.

Mantenha URLs remotas na configuração do operador, valide os destinos e use clientes HTTP com limites de resposta, timeouts e política explícita de redirects. Não transforme URLs enviadas por usuários em destinos de chamadas autenticadas.

`provider.RetryError` informa ao worker um intervalo de espera após uma limitação de consultas. O atraso é persistido na fila, limitado a cinco minutos, sem manter a concessão de execução aberta.

Para a primeira contribuição de provedor, abra uma issue com a documentação pública da integração, o escopo de dados, o modelo de consentimento e o mecanismo de credenciais. Isso permite decidir a fronteira com um contrato concreto.
