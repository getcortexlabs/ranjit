# Operação

O servidor pode usar o sandbox local ou os conectores diretos Efí e Inter. Configure as credenciais da própria conta seguindo o [guia Efí](efi.md) ou o [guia Inter](inter.md); o deploy sozinho não habilita acesso bancário.

## Configuração

| Variável | Padrão | Função |
| --- | --- | --- |
| `RANJIT_API_KEY` | obrigatório | Chave do operador, com pelo menos 32 bytes sem espaços |
| `RANJIT_ADDR` | `127.0.0.1:8080` | Endereço de escuta HTTP |
| `RANJIT_BASE_URL` | `http://localhost:8080` | Origem pública dos links de consentimento |
| `RANJIT_DATABASE` | `./data/ranjit.db` | Caminho do arquivo SQLite |
| `RANJIT_PROVIDER` | `sandbox` | `sandbox`, `efi` ou `inter` |
| `RANJIT_SYNC_INTERVAL` | `1h` | Intervalo do agendador; mínimo `1m` |

O binário lê variáveis de ambiente. Quem carrega `.env` é o Makefile ou o Compose. Ao executar `bin/ranjit` diretamente, exporte as variáveis antes. `ranjit init` cria o arquivo sem sobrescrever outro existente; `ranjit version` imprime a versão; `ranjit healthcheck` verifica `/readyz` no endereço local.

Para expor a instalação, configure uma origem HTTPS sem caminho, query ou credenciais em `RANJIT_BASE_URL` e termine TLS em um proxy. O servidor não implementa TLS. Apenas origens de loopback podem usar HTTP. Configure limite de requisições e acesso na infraestrutura: não há rate limiter ou contas de operadores no binário.

Use exatamente a origem configurada ao abrir o consentimento. `localhost` e `127.0.0.1` são origens diferentes para o navegador. O POST verifica `Origin` quando o cabeçalho existe.

## Dados e backup

Use um processo e um disco local por arquivo SQLite. Não coloque o WAL em NFS e não tente escalar o Compose com várias réplicas compartilhando o mesmo banco.

O processo cria novos diretórios com permissão `0700` e o arquivo com `0600`. Permissões de diretórios ou arquivos já existentes não são alteradas. O banco não tem criptografia em repouso: use proteção de volume, acesso restrito e backups protegidos conforme a operação.

Para backup com o servidor ativo, use a API de backup do SQLite, por exemplo `sqlite3 data/ranjit.db '.backup backup.db'`. Não copie apenas o arquivo principal enquanto existe escrita no WAL. Restrinja as permissões e a retenção da cópia. Teste restauração antes de depender dela.

Revogação e expiração removem as linhas dos dados financeiros, mas não apagam retroativamente backups ou garantem eliminação de bytes antigos no disco. A política de retenção também deve considerar os metadados das conexões e o histórico de jobs, que crescem nesta versão.

## Saúde e encerramento

`/healthz` verifica o processo. `/readyz` verifica também a conexão SQL, sem afirmar que o provedor ou o worker estão saudáveis. Os logs são JSON e incluem IDs de requisição ou de job e códigos de erro. Não incluem URL, query, headers, corpos ou mensagens brutas do provedor. Proxies precisam de configuração equivalente para não registrar os tokens dos links.

SIGINT e SIGTERM encerram a aceitação de chamadas, aguardam as requisições por até dez segundos e cancelam o worker. Um job interrompido fica recuperável após a concessão de um minuto. Os adaptadores precisam respeitar cancelamento de contexto; um adaptador que ignora isso também prejudica o encerramento.

Para trocar a chave da API, atualize a variável e reinicie o processo. Atualize os consumidores junto. Não há período de convivência de duas chaves.

## Verificação

`make check` valida código e comportamento sem exigir portas de rede. `make smoke` precisa de uma instância com o provedor `sandbox` rodando e testa o caminho HTTP por socket. O workflow de CI executa ambos e faz uma verificação de vulnerabilidades. A imagem Docker é construída e exercitada em um job separado.

Os testes dos conectores Efí e Inter usam respostas bancárias simuladas. A validação contra uma conta habilitada é uma etapa separada. Ao mudar de provedor ou ambiente, use outro arquivo de banco para manter os dados separados.
