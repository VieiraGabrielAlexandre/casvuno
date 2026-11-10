# ADR 0004 — Dinheiro, datas, fuso e formatos de exportação

- Status: Proposto
- Data: 2026-11-10

## Decisão
- **Dinheiro:** `int64` de centavos (`kernel/money.Cents`) no Go, `BIGINT` no PostgreSQL com `CHECK`, `number` inteiro em `amountCents` no JSON. Não existe `float` em nenhuma camada. No frontend, o input é convertido de dígitos para centavos sem aritmética de ponto flutuante, e a formatação usa `Intl.NumberFormat('pt-BR', {style:'currency', currency:'BRL'})` só para exibição. Moeda única BRL (coluna `currency` mantida com CHECK `= 'BRL'`).
- **Parcelamento:** `Split(total, n)` conforme RN-010, com `MaxInstallments = 60` como constante.
- **Datas:** `DATE` para competência, vencimento, pagamento e movimento; `TIMESTAMPTZ` para instantes. No JSON, `YYYY-MM-DD` e RFC 3339.
- **Fuso:** `households.timezone` (padrão `America/Sao_Paulo`). "Hoje", limites de mês e horários de lembrete são calculados no fuso da família via `platform/clock.Clock`, que é injetável em testes.
- **CSV (RN-026):** UTF-8 com BOM, separador `;`, `CRLF`, valor em `12,50`, datas ISO. Campos de texto que começam com `= + - @ \t \r` recebem o prefixo `'`. Respeita autorização e filtros, e o streaming é paginado no servidor.
