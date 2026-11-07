# Segurança

O projeto está em desenvolvimento. O sandbox usa dados fictícios e o conector Efí pode consultar dados reais quando configurado com credenciais de produção. Falhas de autenticação, vazamento de tokens e acesso depois de revogação devem ser tratados como problemas de segurança.

Use o [relato privado de vulnerabilidade do GitHub](https://github.com/getcortexlabs/ranjit/security/advisories/new), quando habilitado. Se esse canal estiver indisponível, abra uma issue pedindo um contato privado, sem publicar detalhes de exploração ou dados sensíveis.

Inclua versão ou commit, passos mínimos para reproduzir, impacto observado e um exemplo com dados de teste. Não envie credenciais ou extratos reais. Não há prazo de resposta ou programa de recompensa estabelecido.

As limitações operacionais conhecidas estão em [docs/operations.md](docs/operations.md): chave única por instalação, ausência de criptografia do banco e controle de acesso dos usuários finais a cargo da aplicação integradora.
