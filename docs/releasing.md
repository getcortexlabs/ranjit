# Publicar uma versão

A v0.4.0 reúne as tarefas do [milestone](https://github.com/getcortexlabs/ranjit/milestone/1). PRs intermediários não geram versões. A publicação começa ao enviar a tag, depois do merge e dos checks do commit final.

## Antes da tag

- CI completo: Go 1.26/1.27, race detector, vet, SQLite/PostgreSQL, macOS/Windows, navegador, container e quickstart.
- `scripts/upgrade_smoke.py`: baixa o binário real da v0.3.0, verifica checksum, importa dados sintéticos e valida migração, recuperação de job pendente, reinício e rollback com o backup anterior. PostgreSQL usa um banco temporário próprio; o banco informado em `RANJIT_TEST_POSTGRES_URL` não é modificado.
- `scripts/container_upgrade.sh IMAGEM`: cria dados com a imagem publicada da v0.3.0 e compara as transações depois de executar a candidata e reiniciá-la.
- `install.sh`, `compose.quick.yaml`, SDK nos exemplos e notas de versão apontam para a mesma versão.
- Faça backup da instalação que será atualizada. Não existe migração reversa automática.

O workflow repete os testes de compatibilidade e o quickstart antes de publicar. Compila seis arquivos, verifica seus checksums e instala o binário nativo em uma pasta vazia. Antes da publicação, somente o transporte HTTPS do instalador é substituído por arquivos locais; o arquivo e o executável testados são os da candidata.

## Publicação e conferência

1. Envie uma única tag `v0.4.0` no commit aprovado de `main`.
2. O workflow prepara uma release em rascunho, com binários, imagens para instalação offline, instalador, Compose e checksums.
3. Publica as imagens da versão e testa downloads anônimos em amd64 e arm64 usando configuração Docker vazia.
4. Publica a release e baixa os arquivos públicos para conferir os checksums, instalar o binário por HTTPS, testar o primeiro uso e resolver o SDK pelo módulo Go.
5. Somente depois dessas verificações atualiza `ghcr.io/getcortexlabs/ranjit:latest`.

Se uma etapa falhar, confira o workflow e os artefatos antes de retomar. Não recrie a tag nem substitua artefatos de uma release que já foi anunciada. Uma falha antes da publicação mantém o rascunho; uma falha na verificação posterior exige investigar a distribuição antes de promover o catálogo.

Para conferir uma publicação novamente:

```sh
python3 scripts/verify_release.py --version v0.4.0 --published
```

Registre na release o resultado do workflow, o commit da tag e os testes de distribuição. A receita do Orbit deve apontar para `ghcr.io/getcortexlabs/ranjit:v0.4.0` somente após a imagem estar pública e as duas arquiteturas passarem. O catálogo não precisa das credenciais bancárias nem de uma assinatura paga.

## Voltar à versão anterior

Pare o serviço. Guarde a instalação com problema para análise e recupere o binário **e o backup feito pela versão anterior**, além da configuração correspondente. Não execute a v0.3.0 sobre o banco já migrado. No PostgreSQL, restaure o dump em um banco novo e altere a conexão do serviço; mantenha a instalação atual isolada até conferir os dados.

`ranjit restore` na versão atual valida o SQLite, guarda uma cópia do banco substituído, cancela trabalhos antigos, limpa entregas de webhook e deixa conexões diretas pausadas. Não use esse comando como downgrade de schema. Retome conexões restauradas explicitamente, depois de conferir os perfis e as credenciais.
