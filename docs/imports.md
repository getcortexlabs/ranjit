# Importar extratos

`ranjit import extrato.ofx` abre o arquivo, valida tudo e grava na mesma transação. Um erro em uma linha impede a importação inteira. Não é necessário iniciar a API. O limite é 8 MiB e 10 mil lançamentos por arquivo.

## OFX

O leitor aceita OFX 1.x (SGML, inclusive campos sem fechamento) e OFX 2.x (XML), em UTF-8, Windows-1252 ou ISO-8859-1 declarado no cabeçalho. São aceitos extratos de conta corrente, poupança e cartão em BRL. Arquivos de investimentos, conversão de moedas, DTD e registros de correção `CORRECTFITID` não são aceitos; exporte o extrato consolidado.

Cada lançamento precisa de `FITID`, `DTPOSTED` e `TRNAMT`. O FITID identifica o lançamento dentro da conta. Importações sobrepostas atualizam o mesmo ID, sem comparar apenas descrição, valor e data. O saldo vem de `LEDGERBAL/BALAMT`, com a data `DTASOF`; um extrato antigo não substitui um saldo mais recente. Sem saldo informado, ele fica desconhecido.

A identificação da conta usa banco, agência, número e tipo, transformados em hash. O nome padrão também deriva desse hash. Para escolher um nome:

```sh
ranjit import --account pessoal extrato.ofx
```

Use o mesmo nome nos próximos arquivos dessa conta. O Ranjit recusa usar um nome existente para outra identidade de conta. Um arquivo com várias contas mantém cada uma separada; exportações seguintes com esse mesmo nome precisam conter o mesmo conjunto de contas.

## CSV

Exemplo UTF-8:

```csv
id,date,description,amount
tx-001,2026-11-06,Salário,3500.00
tx-002,2026-11-07,Supermercado,-125.40
```

```sh
ranjit import --account pessoal extrato.csv
```

Cabeçalhos reconhecidos:

| Campo | Nomes aceitos |
| --- | --- |
| Data | `date`, `data`, `posted_on` |
| Descrição | `description`, `descrição`, `descricao`, `histórico`, `historico`, `title` |
| Valor | `amount`, `valor` |
| Identificador, opcional | `id`, `fitid`, `transaction_id` |

O separador de colunas é detectado entre vírgula, ponto e vírgula e tabulação. Valores usam ponto decimal por padrão; `--decimal ,` seleciona vírgula. Remova separadores de milhares. Débitos têm sinal negativo. Não há arredondamento de valores com mais de duas casas.

Datas padrão: `YYYY-MM-DD` ou `DD/MM/YYYY`. Para mês/dia/ano, use `--date-format 01/02/2006`; a opção usa layouts Go. A data de lançamento é civil, sem conversão de fuso.

Sem uma coluna de IDs, o identificador usa o conteúdo do arquivo e o número da linha. Assim, dois pagamentos idênticos dentro de um arquivo continuam sendo dois pagamentos. Repetir o mesmo arquivo não duplica nada. Outro arquivo que se sobreponha ao período já importado será recusado: inclua IDs estáveis ou exporte períodos separados.

CSV não informa saldo. A soma dos lançamentos não é tratada como saldo. OFX, CSV e API bancária têm identidades distintas e não são mesclados automaticamente; importe cada origem em um nome diferente para evitar misturar formatos.

## HTTP e SDK Go

Envie o arquivo como corpo bruto, com autenticação normal da API:

```sh
curl 'http://localhost:8080/v1/imports?customer_id=personal&format=ofx&account=pessoal' \
  -H "Authorization: Bearer $RANJIT_API_KEY" \
  -H 'Content-Type: application/octet-stream' \
  --data-binary @extrato.ofx
```

O SDK oferece `client.Import(ctx, reader, ranjit.ImportOptions{CustomerID: "personal", Format: "ofx", Account: "pessoal"})`. A resposta contém `connection_id`, contagens, `duplicate` e `deduplication`. As contagens descrevem o arquivo, incluindo lançamentos já existentes que foram atualizados; não são contagens de inserções novas.

Os comandos locais listam o agrupamento `personal`. Aplicações podem usar outros `customer_id` pela API, sob a mesma chave de operador. Arquivos importados não expiram. Revogar a conexão remove seus lançamentos e o registro de importação, sem apagar cópias externas do arquivo.


## Conferir antes de importar

No painel (`ranjit ui`), escolha o arquivo, ajuste as colunas do CSV e clique em **Conferir extrato**. A prévia mostra os primeiros 20 lançamentos e conta novos, atualizados e já existentes. Só **Confirmar importação** grava os dados. Arquivos já importados ficam identificados.

No terminal:

```sh
ranjit import --preview --account pessoal --delimiter ';' --decimal , \
  --date-column Quando --description-column Texto --amount-column Quantia \
  --id-column Código extrato.csv
```

Remova `--preview` para gravar com as mesmas opções. Se os nomes já são reconhecidos, o mapeamento é opcional. A prévia também verifica conflitos de conta e períodos sobrepostos, sem deixar registros no banco.
