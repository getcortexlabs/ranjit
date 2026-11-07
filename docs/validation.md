# Validar um conector com uma conta habilitada

Os conectores Efí, Inter, Sicoob, C6 e Santander têm testes automatizados locais. Nenhum deles é anunciado como homologado pelo banco. `/v1/institutions` informa `validation: experimental`; o sandbox informa `simulated`.

Para testar sua própria aplicação habilitada:

```sh
ranjit configure
ranjit doctor
ranjit doctor --live
ranjit sync meu-perfil
ranjit status
ranjit accounts
ranjit transactions
```

O primeiro diagnóstico valida arquivos, certificado e configuração. `--live` verifica autenticação e leitura do saldo. A sincronização também exercita o extrato e sua paginação. Confira valores e datas no aplicativo ou extrato do banco para o mesmo período, respeitando a cobertura do conector; Efí inclui Pix recebidos e devoluções, não o extrato completo.

Depois de concluir a primeira sincronização, repita `ranjit sync meu-perfil`. Lançamentos com o mesmo identificador devem ser atualizados, sem duplicação. Valide um período com mais de uma página e outro sem movimento. Observe `ranjit status` e confirme que a última sincronização só avança ao concluir o lote. Não compartilhe saída de `transactions`, banco SQLite, arquivos de credenciais ou respostas brutas em issues.

Para reportar uma validação, informe apenas: versão do Ranjit, provedor, ambiente, tipo de conta, cobertura conferida, presença de múltiplas páginas e códigos de erro. Não publique números de conta, valores, IDs de transações, certificados, tokens ou Client Secret. Um teste em sandbox não comprova o contrato de produção.

No Santander há divergência entre o caminho de extrato do Swagger e o da coleção oficial Postman. O conector segue a coleção Postman. Confirme esse contrato antes de usar o resultado na sua aplicação; o [guia Santander](santander.md) registra as referências.
