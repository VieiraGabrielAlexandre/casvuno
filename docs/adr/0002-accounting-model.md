# ADR 0002 — Modelo contábil: transações, obrigações, parcelas e movimentos

- Status: Proposto (itens I-04, I-08 e I-18 requerem aprovação)
- Data: 2026-11-10

## Contexto
O produto precisa mostrar três visões sem dupla contagem: consumo, compromissos e caixa (README §4.1). Do jeito que o README modela, parcelas de cartão ficam em `obligations`, o "rascunho" de despesa prevista não tem onde morar, há referências circulares com recorrências e os status são duplicados (ver `spec-review.md` A).

## Decisão
1. **Quatro fatos com tabelas separadas:**
   - `transactions`: fato econômico (consumo/receita). É a única fonte de consumo.
   - `obligations`: algo a pagar ou receber **diretamente** por uma conta. É a fonte de compromissos fora do cartão.
   - `card_installments` + `card_invoices`: compromissos do cartão. A fatura é a unidade de pagamento.
   - `account_movements`: caixa efetivo. É a única fonte de saldo e de fluxo de caixa.
2. **Caminho único de liquidação:**
   - obrigação → `obligation_payments` → movimento;
   - fatura → `invoice_payments` → movimento;
   - parcela nunca é paga individualmente (não existe FK que permita isso).
3. **Despesa/receita imediata** cria transação + obrigação `CONFIRMED` + pagamento + movimento em uma transação SQL. É o mesmo caminho da prevista, sem atalhos.
4. **Despesa/receita prevista** cria só a obrigação `PLANNED` com campos de rascunho. O **primeiro pagamento** confirma: cria a transação (`occurred_on = competence_on`) e vincula. Uma previsão nunca coexiste com uma transação paralela.
5. **Status mínimos persistidos.** O que pode ser calculado é derivado: situação de pagamento, atraso, estado de pagamento da fatura e "estornado". Os totais pagos vêm sempre de `SUM(...)` sob lock da linha-mãe. Não há colunas de cache que possam divergir.
6. **Correções são auditáveis:** void (transação), estorno (movimento compensatório), ajuste (fatura fechada). Nada financeiro é apagado fisicamente.
7. **Edição (V1):** `PATCH` só de campos não estruturais. Valor, data, meio e parcelas mudam via void + novo lançamento. O void de compra no cartão só é permitido se todas as faturas afetadas estiverem `OPEN` e nenhuma ficar com saldo negativo.
8. Transferência é imutável na V1.

## Consequências
- Cada visão financeira é uma consulta simples sobre uma única família de tabelas (INV-10/11/12).
- Uma despesa imediata grava 4 linhas. O volume familiar é irrelevante para isso, e o ganho é ter um caminho só.
- Consumo de despesa prevista só aparece quando paga. A UI mostra "previsto" em compromissos e projeção.
