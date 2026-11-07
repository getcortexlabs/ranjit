# Arquitetura

O processo HTTP e o worker compartilham um SQLite. Esse recorte mantém o projeto fácil de instalar e permite testar as garantias de consistência sem depender de serviços externos. A instalação é de um único operador, com um servidor por arquivo de banco. Os comandos locais também podem abrir esse SQLite por períodos curtos, usando as mesmas transações e concessões de execução.

```text
aplicação → API HTTP → serviço / armazenamento → SQLite
terminal → CLI ──────→ importação / consultas ──────↑
                               ↑                  ↑
                        consentimento           worker → perfis → provedores
```

| Local | Responsabilidade |
| --- | --- |
| `model.go` | Tipos públicos, valores monetários e identificação |
| `client.go` | Cliente HTTP Go; não conhece armazenamento nem provedores |
| `cmd/ranjit` | Configuração, montagem do processo e encerramento |
| `internal/app` | Montagem dos conectores e registro de perfis |
| `internal/config` | Configuração pessoal em JSON e compatibilidade com ambiente |
| `internal/statement` | Leitura limitada de OFX/CSV e identidade dos registros |
| `internal/httpapi` | Rotas, autenticação, limites de entrada e respostas |
| `internal/service` | Criação da conexão e autorização do sandbox |
| `internal/store` | Migrações, consultas e transações de consistência |
| `internal/provider` | Contrato de ingestão e validação do lote |
| `internal/provider/sandbox` | Instituições e dados fictícios reproduzíveis |
| `internal/provider/efi` | Consulta direta de saldo, recebimentos Pix e devoluções na Efí |
| `internal/provider/inter` | Consulta de saldo disponível e extrato da conta Inter Empresas PJ |
| `internal/provider/sicoob` | Consulta mensal de extrato e saldo da conta do cooperado |
| `internal/provider/c6` | Consulta de saldo e extrato C6 Empresas em intervalos de até 30 dias |
| `internal/provider/santander` | Consulta de saldo e extrato Santander Empresas com offset |
| `internal/worker` | Agendamento, tentativas e execução da sincronização |

As consultas simples vão do handler ao armazenamento. O fluxo de consentimento passa pelo serviço. Não existe uma interface de repositório que apenas repete os métodos de um único banco. A interface de provedor existe porque essa é uma fronteira concreta de integração.

## Consentimento

A criação salva uma conexão `pending` e o SHA-256 de um token aleatório. O token original só aparece no link devolvido ao solicitante. Abrir o link exibe os dados e os botões de decisão; somente um POST pode aprovar ou recusar.

A decisão é um UPDATE condicional ao estado, ao token e aos prazos. A aprovação consome o token e cria o primeiro job na mesma transação. Duas requisições com o mesmo token não aprovam duas vezes. Recusa, expiração e revogação também inutilizam o link.

Os prazos de dez minutos e 90 dias pertencem ao sandbox. Não são configurações de OAuth nem afirmações sobre um consentimento oficial.

No modo `api_credentials`, a criação verifica a aplicação bancária instalada pelo operador e grava a conexão ativa junto do primeiro job. Não passa pelo formulário de sandbox. O prazo local de 90 dias também limita essa coleta e fica no campo existente `consent_expires_at`; a validade bancária das credenciais é independente. `authorization_mode` distingue os dois fluxos.

O hash do Client ID e do ambiente vincula a conexão à aplicação Efí. No Inter, o vínculo também inclui a conta selecionada. O conector rejeita outro vínculo antes de fazer chamadas bancárias. Secret, chave privada e certificado são lidos de arquivos locais; tokens OAuth ficam em memória. Revogar no Ranjit encerra o acesso local; desabilitar a aplicação no banco é uma operação separada.

## Sincronização

Um índice parcial permite no máximo um job `queued` ou `running` por conexão. Pedidos simultâneos de sincronização devolvem esse mesmo job. Criar conexões não é idempotente: uma repetição do POST cria outra conexão.

O worker verifica vencimentos, agenda conexões elegíveis e reivindica um job a cada passo. A reivindicação gera um token de execução e uma concessão de um minuto. A consulta ao provedor tem timeout de 20 segundos. Se o processo morrer, uma nova execução pode recuperar o job após esse minuto. Cada processo seleciona apenas conexões das instituições que conhece; adicionar um perfil pelo terminal exige reiniciar o servidor para disponibilizá-lo na API.

Respostas GET bem-sucedidas ficam temporariamente em `sync_responses`, limitadas por job e protegidas pelo token de execução. São dados financeiros, sujeitos à mesma política de backup do SQLite. Tokens OAuth e headers não são persistidos. A chave da requisição é um hash, e o período é fixado na criação do job. Uma tentativa pode continuar de onde parou sem repetir as leituras concluídas. Ao atingir o timeout com progresso, o worker devolve o job à fila sem consumir uma tentativa de falha. Conclusão, falha definitiva, expiração ou revogação removem as respostas temporárias.

Antes de importar, o armazenamento valida o lote e verifica novamente estado do consentimento, prazo e token de execução. Contas, transações, conclusão do job e data da última sincronização são gravados na mesma transação. Uma falha em qualquer escrita desfaz todas elas.

Falhas transitórias usam esperas de 2, 4, 8 e 16 segundos entre até cinco tentativas. Uma limitação de consultas pode adiar a tentativa conforme `Retry-After`, até cinco minutos. Esse prazo é persistido e sobrevive ao reinício. Dados inválidos ou falhas de credenciais encerram o job imediatamente. Um job terminal só volta a ser elegível pelo agendador depois do intervalo de sincronização, ou por um novo pedido explícito. Não há retry automático da criação de conexões no SDK.

## Dados

Os IDs internos derivam da conexão, do ID da conta no provedor e do ID da transação no provedor. O mesmo lançamento recebido novamente atualiza o registro. IDs iguais de contas em conexões diferentes não se misturam.

Cada lote inclui as contas às quais seus lançamentos pertencem. A ingestão acumula transações por upsert; ausência em um lote não é uma instrução de exclusão. Exclusões individuais, alterações de ID no provedor e a passagem de uma transação pendente para um ID definitivo precisam de um contrato adicional antes de um adaptador real.

Importações locais são atômicas e não expiram. Arquivos já importados são reconhecidos por hash; OFX e CSV com IDs também reconciliam lançamentos por conta. Sem IDs, períodos sobrepostos são recusados. A identidade de uma conta em arquivo não é mesclada com a de uma API bancária.

Saldos são informados pelo provedor. CSV sem saldo aparece como `null` na API. `balance_at` distingue o instante de leitura ou a data do saldo informado no OFX; um saldo antigo não substitui outro mais recente. Não são recalculados a partir do histórico importado. Valores usam inteiros no intervalo exato de JSON/JavaScript e moeda BRL. Datas de lançamento usam `YYYY-MM-DD`; os instantes do sistema usam UTC.

## SQLite

O driver abre uma única conexão SQL, habilita chaves estrangeiras, WAL, `synchronous=FULL` e espera de cinco segundos por locks. Transações usam lock imediato. Isso simplifica a disputa por jobs e a ordenação entre revogar e importar, mas serializa consultas e escritas. Não é uma arquitetura de réplicas horizontais.

Migrações SQL ficam embutidas no binário e são registradas por nome. Novas mudanças de schema devem ganhar outro arquivo; migrações aplicadas não devem ser editadas. O início aplica as pendentes em uma transação. Reconstruções de tabela usam uma conexão SQL reservada, suspendem a execução das chaves estrangeiras durante a migração, verificam a integridade antes do commit e restauram sua execução antes de liberar a conexão. O teste de upgrade verifica que contas, transações e jobs existentes são preservados.

A retenção atual mantém metadados das conexões e o histórico dos jobs. A remoção dos dados financeiros ocorre ao revogar ou após o worker identificar um consentimento simulado vencido. A coleta direta pode expirar ou ser pausada sem apagar o histórico. Renovar verifica as credenciais vinculadas à conexão e invalida jobs anteriores antes de enfileirar uma nova coleta. DELETE lógico de linhas não garante eliminação de vestígios no arquivo, WAL ou backups; essa política precisa acompanhar a operação de armazenamento.


## Interface e manutenção

O painel é servido por `internal/httpapi` com `go:embed`, sem CDN ou dependências de frontend em produção. A API continua sendo a fronteira de dados. Sessões locais são mantidas em memória; mutações por cookie exigem origem correspondente. O comando `ui` usa um link descartável em fragmento para não colocar a chave do operador no navegador.

`internal/autostart` renderiza serviços por usuário para os três sistemas suportados. `internal/updater` verifica assets estáveis do repositório e troca o executável mantendo a versão anterior. A camada de armazenamento coordena manutenção com locks compartilhados no uso normal e exclusivos em restauração/atualização. `VACUUM INTO` produz backups independentes do WAL; restauração valida, migra em arquivo temporário e publica apenas o resultado completo. Os testes de navegador ficam separados em `tests/browser`; Node e Playwright são dependências de desenvolvimento.
