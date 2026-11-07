# Acesso ao Open Finance

O Ranjit é software de integração. Instalar o projeto não habilita uma empresa ou uma pessoa a receber dados financeiros pelas APIs oficiais.

A Resolução Conjunta nº 21, de 22 de outubro de 2026, alterou a regulamentação do Open Finance e foi publicada em 26 de outubro. Ela trata, entre outros pontos, das parcerias entre instituições e entidades não autorizadas. Isso não equivale a liberar credenciais públicas para qualquer aplicação independente. [Texto oficial no Banco Central](https://www.bcb.gov.br/estabilidadefinanceira/exibenormativo?numero=21&tipo=Resolu%C3%A7%C3%A3o%2520Conjunta).

O registro da aplicação e a identidade da organização continuam fazendo parte da integração técnica. A documentação oficial descreve o Diretório, a declaração de software e certificados de produção. O consentimento de um titular não substitui esses requisitos da operação receptora. [Guia para instituições receptoras](https://openfinancebrasil.atlassian.net/wiki/spaces/OF/pages/240648607) e [padrão de certificados](https://openfinancebrasil.atlassian.net/wiki/spaces/OF/pages/245694518).

Para uma aplicação que contrata uma integração, o caminho de implementação é obter o acesso aceito para aquela operação, entender sua finalidade e seus consentimentos e então desenvolver o adaptador da API contratada. O papel de cada parte, as permissões de uso e uma eventual redistribuição precisam estar definidos nessa operação. A licença do código não concede direitos sobre os dados recebidos.

O sandbox local simula estados para desenvolvimento e não registra consentimento no ecossistema oficial. Os conectores [Efí](efi.md), [Inter Empresas](inter.md), [Sicoob](sicoob.md), [C6 Empresas](c6.md) e [Santander Empresas](santander.md) acessam a própria conta por credenciais emitidas pelo banco. Usam APIs proprietárias dos bancos; não implementam recepção de dados entre instituições pelo Open Finance. `sandbox` indica se os dados são simulados ou de homologação. Nenhum endpoint solicita senha bancária, realiza scraping ou tenta contornar a identificação da aplicação.

Referências conferidas em 7 de novembro de 2026. A implementação de uma integração real deve conferir novamente as regras e a documentação vigentes.
