# ADR 0003 — Ciclo de cartão, alocação de parcelas e fechamento de fatura

- Status: Aceito em 2026-11-11 (I-15 aprovado pelo usuário)
- Data: 2026-11-10

## Contexto
A RN-011 é ambígua quanto ao vencimento e não trata mudança de `closing_day`/`due_day`, nem o caso `closing_day == due_day`. O README não define se o fechamento é manual ou automático, nem o que fazer com compra retroativa.

## Decisão
**Normalização:** `norm(dia, ano, mês) = min(dia, últimoDia(ano, mês))`.

**Datas de uma fatura que fecha no mês M:**
- `closing_on = norm(closing_day, M)`;
- `due_on` = a primeira data entre `norm(due_day, M)` e `norm(due_day, M+1)` que seja **estritamente posterior** a `closing_on`;
- `cycle_start_on` = `closing_on` da fatura anterior (na primeira fatura, o fechamento calculado para M−1).

**Pertinência:** uma compra na data P pertence à fatura com o menor `closing_on > P`. Logo, o intervalo é `[cycle_start_on, closing_on)` e uma compra no dia do fechamento vai para a próxima fatura (RN-011). A parcela k vai para a (k−1)-ésima fatura seguinte à da 1ª parcela.

**Datas concretas persistidas.** As faturas são criadas sob demanda (find-or-create com `UNIQUE(credit_card_id, cycle_start_on)` e `ON CONFLICT DO NOTHING`). Alterar os dias do cartão afeta só faturas ainda não criadas: a próxima continua encadeada no último `closing_on` existente. Uma data anterior à primeira fatura existente é calculada pela regra atual, e se houver sobreposição o lançamento é rejeitado (422).

**Mês de referência** exibido = mês de `due_on`.

**Fechamento:** manual, via `POST …/close`, permitido quando `hoje(fuso da família) ≥ closing_on`. Congela `closed_base_amount_cents` sob `SELECT … FOR UPDATE` da fatura. Não existe auto-close; a UI sinaliza "fechamento pendente".

**Compra retroativa em fatura `CLOSED`:** a 1ª parcela vai para a próxima fatura não fechada e as demais são deslocadas. A resposta traz `allocationShifted: true`. A inserção de parcela trava a fatura (`FOR UPDATE`) para não competir com o fechamento.

**Pagamento:** permitido em fatura `OPEN` (antecipação) ou `CLOSED`, com teto no saldo em aberto no momento, sob lock. Um ajuste negativo que deixaria o saldo < 0 é rejeitado na V1.

**Limite disponível estimado** = `limit_cents − Σ (total − pago)` de todas as faturas do cartão.

Sem ajuste de vencimento para fim de semana/feriado na V1.

## Testes obrigatórios (tabela)
- fechamento dia 31 em fevereiro (bissexto e não bissexto), abril e dezembro→janeiro;
- `closing_day == due_day`; `due_day < closing_day`; `due_day > closing_day`;
- compra em `closing_on − 1`, em `closing_on` e em `closing_on + 1`;
- 28/02 e 29/02 de ano bissexto (T13); compra no dia do fechamento (T14);
- 12× atravessando a virada do ano;
- mudança de `closing_day` com faturas existentes.
