# Ranjit no seu backend

O Ranjit é um serviço que sua aplicação chama por HTTP. O SDK Go cuida dessas chamadas. Você pode executar o binário junto do backend ou instalar o container na sua hospedagem. Docker é uma forma de instalação, não uma dependência do SDK.

O [guia do SDK](sdk.md) cobre paginação, retentativas e erros. `ranjit doctor --remote --json` verifica a instalação usando somente `RANJIT_URL` e `RANJIT_API_KEY`. Diagnósticos e métricas estão descritos em [operação](operations.md).

## Desenvolvimento local

O [quickstart completo](../examples/quickstart/) sobe Ranjit, backend Go e receptor de webhooks com `./scripts/quickstart.sh`. Ele gera as chaves e verifica importação e sandbox. Para trabalhar sem Docker, siga com o binário abaixo.

Depois de instalar o binário:

```sh
ranjit serve
```

Em outro terminal, `ranjit key` mostra a chave da instalação. Configure no ambiente do **seu backend**:

```sh
export RANJIT_URL=http://localhost:8080
# RANJIT_API_KEY: chave mostrada por ranjit key
go get github.com/getcortexlabs/ranjit@v0.4.0
```

```go
client, err := ranjit.NewClientFromEnv()
if err != nil {
    return err
}
result, err := client.Import(ctx, file, ranjit.ImportOptions{
    CustomerID: authenticatedUserID,
    Account:    "personal",
    Format:     "ofx",
})
```

O cliente pode ser criado uma vez na inicialização e reutilizado pelos handlers. Passe o contexto da requisição para propagar cancelamentos. Use `next_cursor` nas próximas consultas até ele ficar vazio. Valores monetários estão em centavos inteiros.

O [exemplo executável](../examples/backend/) mostra autenticação do backend, importação de CSV e consulta paginada. A chave do Ranjit dá acesso à instalação inteira: não a envie para o navegador ou aplicativo móvel. Antes de consultar uma conexão ou conta, seu backend precisa verificar que ela pertence ao usuário autenticado.

## Servidor com PostgreSQL

Configure estas variáveis no serviço Ranjit:

| Variável | Valor |
| --- | --- |
| `RANJIT_ADDR` | `0.0.0.0:8080` no container |
| `RANJIT_BASE_URL` | Origem HTTPS pública do serviço, sem caminho |
| `RANJIT_API_KEY` | Segredo aleatório de pelo menos 32 caracteres |
| `RANJIT_DATABASE_URL` | URL do PostgreSQL fornecida pela hospedagem |
| `RANJIT_PROFILES_JSON` | `[]` para começar com importação de extratos |
| `RANJIT_SYNC_INTERVAL` | `1h`, opcional |

Inicie com `ranjit serve`. A imagem já usa esse comando. `/readyz` verifica a conexão com o banco, e `/healthz` verifica o processo. Configure o proxy da hospedagem para HTTPS e mantenha uma réplica. Nenhum volume de aplicação é necessário nesse modo; PostgreSQL contém o histórico e a fila. Os certificados, chaves e a configuração pertencem ao armazenamento de segredos da plataforma.

`RANJIT_DATABASE_URL` prevalece sobre o caminho SQLite padrão da imagem. A configuração por ambiente exige `RANJIT_API_KEY` e não deve ser combinada com `RANJIT_CONFIG`. O SDK usa `RANJIT_URL`; o servidor usa `RANJIT_BASE_URL`. Em containers separados, `localhost` aponta para o próprio container: use a URL HTTPS do Ranjit ou seu endereço na rede privada confiável.

Configure TLS do PostgreSQL conforme a hospedagem (`sslmode=verify-full` com CA confiável quando disponível). Não desative TLS para um banco acessível pela internet. O serviço não imprime a URL de conexão em erros de inicialização.

Alterar de SQLite para PostgreSQL cria uma instalação vazia; não transfere o histórico. Mantenha a instalação original até concluir uma migração de dados planejada. Não restaure um arquivo `.db` em PostgreSQL.

## Configurar contas

Sem perfis, a API aceita OFX/CSV e retorna uma lista vazia de instituições. Para testar o fluxo simulado, use:

```json
[{"name":"demo","provider":"sandbox"}]
```

Para habilitar uma integração existente, coloque os metadados em `RANJIT_PROFILES_JSON`. Por exemplo, para Efí:

```json
[
  {
    "name": "main-account",
    "provider": "efi",
    "environment": "production",
    "client_id": "CLIENT_ID_DA_APLICACAO",
    "client_secret_file": "env:BANK_CLIENT_SECRET",
    "certificate_file": "env:BANK_CERT_PEM",
    "private_key_file": "env:BANK_KEY_PEM",
    "lookback_days": 7
  }
]
```

Cadastre `BANK_CLIENT_SECRET`, `BANK_CERT_PEM` e `BANK_KEY_PEM` como **segredos** na hospedagem. PEM precisa conter as quebras de linha reais, incluindo BEGIN/END. A referência `env:NOME` é explícita; um caminho continua lendo um arquivo. Rotacionar um segredo bancário exige reiniciar ou fazer um novo deploy do serviço. Para o Sicoob sandbox, `access_token_file` também aceita essa referência. Os campos exigidos por cada banco estão nos respectivos guias.

Não combine `RANJIT_PROFILES_JSON` com `RANJIT_PROVIDER`. Este último permanece disponível para a configuração antiga de um único conector. Perfis indisponíveis não impedem a consulta do histórico; `ranjit doctor` ajuda a identificar configuração ou certificados inválidos.

Após configurar um perfil, consulte `client.Institutions(ctx)`, selecione o ID retornado e chame `client.ConnectWithKey` com uma chave aleatória persistida para a operação. A conexão direta verifica as credenciais e agenda a primeira sincronização; acompanhe `client.Status` antes de assumir que os dados estão disponíveis. A autorização no navegador pertence ao sandbox simulado. Os conectores diretos acessam as contas cobertas pelas credenciais do operador; um `customer_id` diferente não concede acesso a outra conta bancária. Uma aplicação que recebe contas de terceiros precisa de uma operação de compartilhamento habilitada e do adaptador correspondente.

## Orbit

A receita do catálogo cria o serviço Ranjit, um PostgreSQL gerenciado e uma chave de API aleatória. A URL do banco entra por referência; a chave fica como segredo. O endereço HTTPS gerado é configurado em `RANJIT_BASE_URL` após o primeiro deploy.

Depois da instalação, configure a URL e a chave no seu backend. Comece importando um extrato; quando tiver acesso bancário habilitado, adicione os perfis e segredos nas variáveis do serviço e faça um novo deploy. O painel `/app/` permanece restrito ao uso local; a instalação hospedada é operada pela API, pelas variáveis e pelos logs do Orbit.

A receita usa a imagem pública `ghcr.io/getcortexlabs/ranjit:v0.3.0`, sem login no registry. A versão instalada depende da receita presente na sua versão do Orbit; atualizar o Ranjit exige alterar a tag e fazer um novo deploy.

## Backup e atualização

Use os backups do PostgreSQL na hospedagem ou `pg_dump`/`pg_restore`. Inclua os segredos em uma política separada de recuperação. `ranjit backup`, `restore` e a atualização automática com backup SQLite recusam esse modo de servidor.

Antes de atualizar a imagem, faça um backup do banco. As migrações são aplicadas no início e não têm downgrade automático; voltar apenas à imagem anterior pode não reverter uma mudança de schema. Para restaurar um backup antigo, pare o serviço e revise conexões e jobs antes de retomar a coleta. O restore gerenciado do PostgreSQL não aplica as pausas automáticas nem limpa a fila de webhooks como o comando SQLite. Ao restaurar uma cópia antiga, revise essa fila antes de habilitar o envio; notificações antigas podem voltar a ser entregues.

## Webhooks e continuidade da integração

Você pode receber eventos de sincronização e de estado da conexão no seu backend e trocar a chave da API com um prazo para atualizar os consumidores. Configure as variáveis pelo gerenciador de segredos da hospedagem e siga o [guia de integração confiável](backend-reliability.md). A fila de entrega usa o mesmo PostgreSQL/SQLite, sem Redis nem outro serviço. O receptor precisa estar acessível pelo Ranjit e salvar os eventos por ID antes de confirmar recebimento.
