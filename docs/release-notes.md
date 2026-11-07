Primeira versão pública do Ranjit.

- Binários para Linux, macOS e Windows, em amd64 e arm64. Não exige Go nem servidor para importar e consultar extratos.
- `ranjit demo`, `configure`, `doctor`, `import`, `accounts`, `transactions`, `status` e `sync`.
- Importação OFX/CSV com valores inteiros, deduplicação e gravação atômica.
- Perfis para várias contas e retomada de leituras bancárias interrompidas.
- API HTTP, SDK Go, SQLite e imagem Docker sem root.

Os conectores Efí, Inter, Sicoob, C6 e Santander são experimentais: têm testes locais com respostas simuladas, mas ainda precisam de validação com contas habilitadas. A API de cada banco exige suas próprias credenciais. Importar um extrato local não exige habilitação bancária.

Instalação e primeiros comandos: https://github.com/getcortexlabs/ranjit#instalar
