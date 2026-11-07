# Operação

O servidor pode usar o sandbox local ou os conectores diretos Efí, Inter, Sicoob, C6 e Santander. Configure as credenciais da própria conta seguindo o [guia do provedor](providers.md); o deploy sozinho não habilita acesso bancário.

## Configuração

| Variável | Padrão | Função |
| --- | --- | --- |
| `RANJIT_API_KEY` | obrigatório | Chave do operador, com pelo menos 32 bytes sem espaços |
| `RANJIT_ADDR` | `127.0.0.1:8080` | Endereço de escuta HTTP |
| `RANJIT_BASE_URL` | `http://localhost:8080` | Origem pública dos links de consentimento |
| `RANJIT_DATABASE` | `./data/ranjit.db` | Caminho do arquivo SQLite |
| `RANJIT_PROVIDER` | `sandbox` | `sandbox`, `efi`, `inter`, `sicoob`, `c6` ou `santander` |
| `RANJIT_SYNC_INTERVAL` | `1h` | Intervalo do agendador; mínimo `1m` |

O binário usa `config.json` quando `RANJIT_CONFIG` está definido ou quando não há `RANJIT_API_KEY` no ambiente e existe configuração pessoal. O arquivo explicitamente selecionado tem precedência: não há mistura de credenciais entre arquivo e ambiente. Caminhos relativos no JSON são resolvidos a partir da pasta dele. No fluxo legado, o binário lê variáveis de ambiente. Quem carrega `.env` é o Makefile ou o Compose. Ao executar `bin/ranjit` diretamente, exporte as variáveis antes. `ranjit init` cria o arquivo sem sobrescrever outro existente; `ranjit version` imprime a versão; `ranjit healthcheck` verifica `/readyz` no endereço local.

Para expor a instalação, configure uma origem HTTPS sem caminho, query ou credenciais em `RANJIT_BASE_URL` e termine TLS em um proxy. O servidor não implementa TLS. Apenas origens de loopback podem usar HTTP. Configure limite de requisições e acesso na infraestrutura: não há rate limiter ou contas de operadores no binário.

Use exatamente a origem configurada ao abrir o consentimento. `localhost` e `127.0.0.1` são origens diferentes para o navegador. O POST verifica `Origin` quando o cabeçalho existe.

## Dados e backup

Use um servidor e um disco local por arquivo SQLite. Comandos locais do Ranjit podem abrir o mesmo arquivo; as transações e concessões coordenam essas operações. Não coloque o WAL em NFS e não tente escalar o Compose com várias réplicas compartilhando o mesmo banco.

O processo cria novos diretórios com permissão `0700` e o arquivo com `0600`. Permissões de diretórios ou arquivos já existentes não são alteradas. O banco não tem criptografia em repouso: use proteção de volume, acesso restrito e backups protegidos conforme a operação.

Use `ranjit backup copia.db`, inclusive com o servidor ativo. O comando usa `VACUUM INTO`, confere integridade e publica uma cópia consistente sem sobrescrever um destino existente. Configuração, chave da API local e credenciais bancárias não entram nessa cópia. Ela contém dados financeiros e não é criptografada. Não copie apenas o arquivo principal enquanto existe escrita no WAL.

Para restaurar: `ranjit service stop` (ou encerre `serve`/`ui`) e `ranjit restore --replace copia.db`. Um lock exclusivo impede a restauração enquanto outro processo Ranjit usa esse banco. O comando valida a cópia, guarda o banco anterior ao lado do original e cancela jobs e links antigos. Conexões bancárias ficam pausadas; arquivos importados continuam disponíveis. Retome cada conexão com `ranjit renew ID`. Sem `--replace`, o destino precisa ser novo. Use discos locais que suportem hard links, como APFS, ext4 e NTFS. Processos SQLite externos não participam do lock de aplicação; encerre-os antes de restaurar.

Revogação e expiração de consentimento simulado removem as linhas dos dados financeiros. Em conexões diretas, expiração e pausa preservam o histórico, mas não apagam retroativamente backups ou garantem eliminação de bytes antigos no disco. A política de retenção também deve considerar os metadados das conexões e o histórico de jobs, que crescem nesta versão.

## Saúde e encerramento

`/healthz` verifica o processo. `/readyz` verifica também a conexão SQL, sem afirmar que o provedor ou o worker estão saudáveis. Os logs são JSON e incluem IDs de requisição ou de job e códigos de erro. Não incluem URL, query, headers, corpos ou mensagens brutas do provedor. Proxies precisam de configuração equivalente para não registrar os tokens dos links.

SIGINT e SIGTERM encerram a aceitação de chamadas, aguardam as requisições por até dez segundos e cancelam o worker. Um job interrompido fica recuperável após a concessão de um minuto. Os adaptadores precisam respeitar cancelamento de contexto; um adaptador que ignora isso também prejudica o encerramento.

Para trocar a chave da API, atualize a variável e reinicie o processo. Atualize os consumidores junto. Não há período de convivência de duas chaves.

## Verificação

`make check` valida código e comportamento sem exigir portas de rede. `make smoke` precisa de uma instância com o provedor `sandbox` rodando e testa o caminho HTTP por socket. O workflow de CI executa ambos e faz uma verificação de vulnerabilidades. A imagem Docker é construída e exercitada em um job separado.

Os cinco conectores usam respostas bancárias simuladas nos testes. A validação contra uma conta habilitada é uma etapa separada, descrita em [homologação](validation.md). Perfis distintos podem usar o mesmo SQLite: cada instituição contém o nome do perfil e cada conexão permanece vinculada ao hash da aplicação, ambiente e conta. Trocar credenciais que alterem essa identidade exige uma nova conexão. Não renomeie um perfil com conexões ativas sem planejar a reconexão.

Importações de arquivos não expiram e não entram no agendador bancário. O operador pode removê-las com `ranjit revoke ID`. Conexões bancárias diretas mantêm o prazo local de 90 dias; esse prazo é uma decisão do Ranjit, não um consentimento de Open Finance emitido pelo banco.

Cada job retém até 110 respostas GET e 32 MiB no SQLite. Os corpos contêm dados financeiros e recebem a mesma proteção e política de backup do restante do banco. Tokens OAuth, headers e URLs não são persistidos. Tentativas reutilizam respostas do mesmo job, com período fixo. O resultado só fica disponível após a validação completa. Paginação inconsistente falha sem publicar um extrato parcial. Um novo job começa uma leitura nova.

`requests_completed` inclui saldo e páginas de extrato; não é um percentual. `data_from` e `data_through` são as datas mínima e máxima dos lançamentos armazenados, sem afirmar cobertura de todos os dias entre elas. O saldo de um OFX usa a data informada em `DTASOF`. CSV não fornece saldo: a API retorna `balance: null`.

## Distribuição

As releases contêm seis binários (Linux, macOS e Windows em amd64/arm64), checksums e imagens Docker de Linux em amd64/arm64. `sh install.sh --docker` carrega a imagem da release e prepara um Compose que usa a cópia local, sem login em registry. O workflow testa essa imagem e o primeiro uso antes de publicar os arquivos.

O workflow também envia a imagem ao GHCR. A visibilidade desse pacote é independente da visibilidade do repositório; para oferecer `docker pull` anônimo, o mantenedor precisa marcar o pacote como público nas configurações do GitHub Packages. A instalação pela release não depende dessa opção.


## Painel e serviço pessoal

`ranjit ui` inicia o mesmo servidor de `serve` e abre o navegador. Se a instalação já está rodando, só abre uma sessão. `--no-browser` imprime o link. Sem serviço instalado, mantenha o terminal aberto enquanto usa o painel.

O link inicial vale um minuto, é consumido uma vez e fica no fragmento da URL até ser trocado por um cookie HttpOnly/SameSite=Strict, válido por oito horas. Reiniciar o servidor encerra as sessões. O painel exige hostname de loopback, rejeita origens externas e não armazena a chave em localStorage. Um usuário com acesso à sessão pode operar toda a instalação. O painel mostra o agrupamento `personal`; outros agrupamentos continuam na API.

```sh
ranjit service install --dry-run
ranjit service install
ranjit service status
ranjit service stop
ranjit service start
ranjit service uninstall
```

A instalação usa o `config.json` pessoal, o caminho absoluto do executável e um identificador derivado do caminho da configuração. `ranjit --config /caminho/config.json service install` escolhe outra instalação. No Linux, usa systemd do usuário; no macOS, LaunchAgent; no Windows, uma tarefa ao entrar na sessão, com o usuário atual e sem elevação. Não há inicialização antes do login. A máquina precisa ter o gerenciador correspondente disponível. Os comandos de serviço não convertem uma configuração legada de ambiente; configure os perfis pessoais primeiro. Desinstalar o serviço não apaga o banco nem as credenciais.

O servidor mantém a consulta ao histórico mesmo quando um perfil pessoal não pode ser carregado. Esse perfil fica fora do agendador até corrigir a configuração e reiniciar. O painel mostra o aviso. Certificados vencendo em até 30 dias geram alerta; em até 7 dias, alerta urgente. O diagnóstico também detecta certificados vencidos, ainda não válidos ou ilegíveis. Atualizar o arquivo exige reiniciar para recarregar o transporte bancário.

Referências dos serviços: [LaunchAgents](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html), [systemd.service](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html) e [Task Scheduler](https://learn.microsoft.com/en-us/windows/win32/taskschd/task-scheduler-schema).

## Atualizar

`ranjit update --check` consulta a última release estável no GitHub. `ranjit update` baixa o arquivo da plataforma, verifica SHA-256 e mantém uma cópia do executável anterior. O checksum protege a integridade do download; a confiança continua sendo no HTTPS e na release do repositório. Não há consulta automática de atualização ao iniciar.

Pare o serviço ou o processo aberto antes de atualizar. O comando exige acesso exclusivo ao banco selecionado, faz uma cópia antes da troca e mantém os dados em caso de erro de download. Não faz downgrade nem substitui builds `dev`. A pasta do executável precisa ser gravável pelo usuário. Depois, rode `ranjit ui` ou `ranjit service start`. No Docker, use a imagem da nova release em vez de executar o atualizador dentro do container. Preserve cópias anteriores até conferir os dados; voltar de uma migração exige o binário e o backup da versão correspondente.
