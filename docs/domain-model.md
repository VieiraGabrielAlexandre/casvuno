# Modelo de domínio — Casvuno

> Complementa o README §4–§6. O que não estiver listado aqui segue o README. As diferenças em relação ao README estão marcadas com **Δ** e justificadas em `docs/spec-review.md` e nos ADRs.

## 1. Módulos e posse de dados

Cada tabela tem **um** módulo dono, e só ele escreve nela. O módulo `reporting` lê várias tabelas por consultas próprias, somente leitura.

| Módulo | Responsabilidade | Tabelas (escrita) | Depende de (via interface do consumidor) |
|---|---|---|---|
| `identity` | Cadastro, login, sessões, hash de senha | `users`, `sessions` | — |
| `households` | Família, membros, convites, checagem de membership | `households`, `household_members`, `household_invites` | `categories` (seed de padrões) |
| `categories` | Categorias e hierarquia de 2 níveis | `categories` | — |
| `accounts` | Contas, movimentos, saldo, extrato, transferências, estornos de movimento | `accounts`, `account_movements`, `transfers` | — |
| `cards` | Cartões, ciclo, faturas, parcelas, pagamentos e ajustes de fatura, limite estimado | `credit_cards`, `card_invoices`, `card_installments`, `invoice_payments`, `invoice_adjustments` | `accounts` (postar/estornar movimento) |
| `ledger` **Δ** | Receitas/despesas, obrigações, pagamentos de obrigação, confirmação de previsão, void, favoritos | `transactions`, `obligations`, `obligation_payments`, `quick_templates` | `accounts`, `cards`, `categories`, `households` |
| `recurrences` | Regras, agenda, geração idempotente, skip, edição de futuras | `recurring_rules`, `recurring_occurrences` | `ledger` |
| `reporting` | Dashboard, consumo, compromissos, caixa, projeção, CSV | — (somente leitura) | — |
| `notifications` | Preferências, centro de notificações, jobs de lembrete | `notification_preferences`, `notifications` | — (lê vencimentos por consulta própria) |
| `platform/*` | Config, pool/tx, HTTP, idempotência, auditoria, worker, clock | `idempotency_keys`, `audit_events` | — |

Grafo de dependência (acíclico): `accounts ← cards ← ledger ← recurrences`. `households → categories`. `reporting`, `notifications` e `identity` são independentes.

## 2. Tipos de kernel

- `money.Cents` (`int64`). Positivo no domínio; o sinal aparece só na direção do movimento, nos ajustes de fatura e no saldo inicial. Nunca `float`.
- `civil.Date` (data de calendário sem hora, `DATE`). Instantes em `time.Time` UTC (`TIMESTAMPTZ`). "Hoje" é sempre calculado no fuso da família via `Clock`: `kernel.Today(clock.Now(), membership.Timezone)` (o fuso vem na membership desde a F3.2).
- IDs: UUID v7 gerado na aplicação.
- Erros: `ErrValidation`, `ErrNotFound`, `ErrConflict`, `ErrForbidden`, `ErrInvariant` (→ 400/422, 404, 409, 403, 422).

## 3. Diferenças de esquema em relação ao README §6 (Δ)

| Tabela | Mudança |
|---|---|
| `users` | `email citext UNIQUE` (normalizado em minúsculas); sem `user_agent_hash` em `sessions` (sem uso na V1). |
| `sessions` | `token_hash bytea` (SHA-256), `expires_at` deslizante + `absolute_expires_at` (ADR 0005). |
| `household_members` | `status ACTIVE \| INACTIVE` (sem `INVITED`); `deactivated_at`; `UNIQUE(household_id, id)` para FKs compostas; reativação volta como `MEMBER`. |
| `household_invites` | `email NULL` (informativo); uso único (`accepted_at`/`accepted_by_user_id`); `revoked_at`; expira em 7 dias; `invited_by_member_id` com FK composta. |
| `audit_events` | `household_id NOT NULL`; append-only garantido por trigger. |
| `categories` | `UNIQUE(household_id, id, kind)` (alvo da FK de `transactions`, F3.1). `parent_id` só pode apontar para uma categoria sem pai, ativa, do mesmo `kind` e da mesma família (FK composta + regra no serviço). Nome único entre irmãos ativos (índice parcial, `lower(name)`). Nunca apagada: `is_active=false`. No máximo 200 por família. Criadas com a família (49 padrões pt-BR, `internal/categories/defaults.go`). |
| `accounts` | `opening_balance_cents` pode ser negativo (limite ±10^15); `UNIQUE(household_id, id)`; titular via FK composta para `household_members`; nome único entre contas ativas; arquivar exige saldo 0 e congela a conta (I-42). |
| `account_movements` | Sem coluna de status. `kind ∈ {DIRECT_PAYMENT, DIRECT_RECEIPT, INVOICE_PAYMENT, TRANSFER, REVERSAL}` (sem `OPENING_ADJUSTMENT`). `reversal_of_id UNIQUE NULL` (FK composta; `kind = REVERSAL` ⇔ `reversal_of_id` preenchido). **Append-only por trigger** (`forbid_update_delete`). `transfer_id` (F2.3) liga as duas pernas de uma transferência. |
| `transfers` | Coluna `description` (padrão "Transferência"); `CHECK` origem ≠ destino; FKs compostas para as duas contas e para o membro que criou; **append-only por trigger** (imutável na V1, I-37). `account_movements.transfer_id` com `CHECK (kind = 'TRANSFER') = (transfer_id IS NOT NULL)`. |
| `transactions` | Novas colunas: `payment_method`, `account_id`, `credit_card_id NULL`. `recurring_occurrence_id UNIQUE NULL` (só regra com cartão). CHECK: `credit_card_id IS NOT NULL ⇔ payment_method='CREDIT_CARD'`, e `kind='INCOME' ⇒ credit_card_id IS NULL`. **F3.1 (000009):** FK composta `(household_id, category_id, kind)` → `categories (household_id, id, kind)` (o banco recusa categoria de outro tipo); `CHECK (scope='FAMILY' OR person_member_id IS NOT NULL)` (I-43); `account_id NOT NULL` e `payment_method` sem `CREDIT_CARD` até a F4.3, que adiciona `credit_card_id` e torna `account_id` opcional; `recurring_occurrence_id` entra na F5.3. Sem `DELETE` (FK das obrigações); void na F3.3 (status `VOIDED`, sem coluna nova). |
| `obligations` | `source ∈ {DIRECT, RECURRING, MANUAL_FORECAST}` (sem `CARD_INSTALLMENT`). `status ∈ {PLANNED, CONFIRMED, CANCELLED}`. Saem `card_invoice_id`, `installment_*` e `paid_cents`. Novos campos de rascunho: `description`, `category_id`, `scope`, `person_member_id NULL`, `payment_method`, `account_id NULL`, `competence_on`. `transaction_id UNIQUE NULL`. `recurring_occurrence_id UNIQUE NULL`. **F3.1:** `CHECK (status='CANCELLED' OR (status='PLANNED') = (transaction_id IS NULL))` (INV-13, parte verificável por linha); `account_id NULL` (previsão pode não ter conta). |
| `obligation_payments` | `account_movement_id UNIQUE` com FK composta para o movimento; `created_by_member_id`; **append-only por trigger** (correção = estorno do movimento, F3.3). |
| `card_installments` **(nova)** | `id`, `household_id`, `transaction_id`, `credit_card_id`, `card_invoice_id`, `installment_number`, `installment_total`, `amount_cents > 0`, `status ACTIVE \| CANCELLED`, timestamps. `UNIQUE(transaction_id, installment_number)`. |
| `card_invoices` | `status OPEN \| CLOSED`; `UNIQUE(credit_card_id, cycle_start_on)` e `UNIQUE(credit_card_id, closing_on)`; `closed_at`, `closed_by`. |
| `invoice_adjustments` | `amount_cents ≠ 0` (com sinal); `reason` obrigatório. |
| `recurring_occurrences` | Sem `transaction_id`/`obligation_id`. `status PLANNED \| GENERATED \| SKIPPED \| CANCELLED`. |
| `idempotency_keys` | `response_body bytea` (não `jsonb`, I-41); `response_status` 2xx; `request_hash bytea(32)`; linha só persiste junto com uma operação bem-sucedida. |
| `notification_preferences` | Sem `push_enabled` até existir Web Push. |

Todas as FKs entre recursos de negócio são compostas `(household_id, x_id)` → `UNIQUE(household_id, id)` no destino. `ON DELETE RESTRICT` em tudo que é financeiro.

## 4. Máquinas de estado

- **Transaction:** `ACTIVE → VOIDED` (terminal).
- **Obligation:** `PLANNED → CONFIRMED` (primeiro pagamento, que cria a transação); `PLANNED|CONFIRMED → CANCELLED` (void, skip, edição de futuras). Derivados: `paymentStatus = UNPAID | PARTIALLY_PAID | PAID`; `isOverdue = due_on < hoje ∧ saldo > 0`.
- **CardInvoice:** `OPEN → CLOSED` (manual, `hoje ≥ closing_on`, congela `closed_base_amount_cents`). Derivado: `paymentStatus`.
- **CardInstallment:** `ACTIVE → CANCELLED` (só com fatura `OPEN`).
- **RecurringOccurrence:** `PLANNED → GENERATED | SKIPPED | CANCELLED`.
- **AccountMovement:** imutável. Estornado ⇔ existe um movimento `REVERSAL` com `reversal_of_id` = id dele.

## 5. Invariantes financeiras

Cada invariante tem teste. As marcadas com ✔︎SQL também são verificadas por `infra/sql/check-invariants.sql`, que roda nos testes de integração e no `make restore-test`.

| ID | Invariante |
|---|---|
| INV-01 | Valores de transação, obrigação, parcela, pagamento, movimento e transferência são `> 0`. Têm sinal apenas `invoice_adjustments.amount_cents` (≠ 0) e `opening_balance_cents`. |
| INV-02 ✔︎SQL | Para compra no cartão: `Σ card_installments.amount_cents = transactions.amount_cents`, `count = installment_total`, `1 ≤ N ≤ 60`. A diferença entre parcelas é ≤ 1 centavo, e as primeiras `total % N` recebem o centavo extra. |
| INV-03 ✔︎SQL | Uma transação no cartão tem N parcelas e nenhuma obrigação. Uma transação fora do cartão tem exatamente uma obrigação (`obligations.transaction_id UNIQUE`). |
| INV-04 ✔︎SQL | `Σ obligation_payments(obligation) ≤ obligation.amount_cents`. |
| INV-05 ✔︎SQL | Cada `obligation_payment` aponta para exatamente um movimento de mesmo valor: `PAYABLE → OUT/DIRECT_PAYMENT`, `RECEIVABLE → IN/DIRECT_RECEIPT`. |
| INV-06 ✔︎SQL | `invoice_total = Σ parcelas ACTIVE + Σ ajustes` e `0 ≤ Σ invoice_payments ≤ invoice_total`. |
| INV-07 ✔︎SQL | Cada `invoice_payment` aponta para um movimento `OUT/INVOICE_PAYMENT` de mesmo valor (`account_movement_id UNIQUE`). |
| INV-08 ✔︎SQL | Cada transferência tem exatamente 2 movimentos `TRANSFER` (OUT na origem, IN no destino), mesmo valor, contas distintas e da mesma família. |
| INV-09 | Saldo da conta = `opening_balance_cents + Σ IN − Σ OUT` (estornos incluídos). Nunca é calculado a partir de transações. |
| INV-10 | **Consumo** = Σ `transactions` `ACTIVE` por `occurred_on`. Nunca vem de movimentos, obrigações ou faturas. |
| INV-11 | **Compromissos** = saldo em aberto de obrigações não canceladas + saldo em aberto de faturas, por vencimento. Parcelas nunca são somadas separadamente. |
| INV-12 | **Caixa** = Σ movimentos. Transferências internas têm efeito líquido zero no consolidado. |
| INV-13 | Obrigação `PLANNED` ⇔ `transaction_id IS NULL` ⇔ sem pagamentos. |
| INV-14 | Uma ocorrência por `(rule_id, scheduled_on)`; no máximo uma obrigação ou transação por ocorrência. |
| INV-15 ✔︎SQL | Fatura `CLOSED`: `closed_base_amount_cents = Σ parcelas ACTIVE` no momento do fechamento. Depois disso não entram nem saem parcelas; só ajustes. |
| INV-16 | Toda referência cruzada aponta para a mesma família (FK composta + `household_id` em todo `WHERE`). |
| INV-17 | Toda família tem ≥ 1 membro `OWNER` `ACTIVE`. |
| INV-18 ✔︎SQL | Um movimento é estornado no máximo uma vez; o estorno tem direção oposta, mesmo valor e mesma conta. |
| INV-19 ✔︎SQL | Transação `VOIDED`: obrigação `CANCELLED`, todo pagamento com movimento estornado, parcelas `CANCELLED` (F4.6). Converso: pagamento direto só é estornado com a obrigação cancelada. |
| INV-20 | Nenhuma tabela, coluna ou rota representa divisão, cota ou dívida entre membros (RN-003). `person_member_id` e `scope` servem só para filtro. |

## 6. Efeitos de cada comando

v = valor da operação; p = valor pago.

| Comando | transactions | obligations | obl._payments | card_installments | account_movements | Consumo | Compromisso | Caixa |
|---|---|---|---|---|---|---|---|---|
| Despesa imediata (Pix/débito/dinheiro) | +1 ACTIVE | +1 CONFIRMED (paga) | +1 | — | +1 OUT | +v | 0 (já liquidado) | −v |
| Despesa prevista | — | +1 PLANNED (rascunho) | — | — | — | 0 | +v | 0 |
| Pagar prevista (total p=v) | +1 (confirma) | → CONFIRMED | +1 | — | +1 OUT | +v (na competência) | −v | −v |
| Pagamento parcial (p<v) | +1 se ainda PLANNED | CONFIRMED | +1 | — | +1 OUT | +v (uma vez) | −p | −p |
| Compra no cartão em N× | +1 | — | — | +N (em N faturas) | — | +v | +v (via faturas) | 0 |
| Pagar fatura | — | — | — | — | +1 OUT (+ `invoice_payment`) | 0 | −p | −p |
| Ajuste de fatura (±a) | — | — | — | — | — | 0 | ±a | 0 |
| Transferência | — | — | — | — | +2 (OUT/IN) | 0 | 0 | 0 consolidado |
| Receita imediata | +1 INCOME | +1 RECEIVABLE CONFIRMED | +1 | — | +1 IN | receita +v | 0 | +v |
| Receita prevista → recebida | +1 na confirmação | PLANNED → CONFIRMED | +1 | — | +1 IN | receita +v | −v recebível | +v |
| Void (não cartão) | → VOIDED | → CANCELLED | mantidos | — | +1 REVERSAL por pagamento | −v | −saldo | +Σp |
| Void (cartão, faturas OPEN) | → VOIDED | — | — | → CANCELLED | — | −v | −v | 0 |
| Gerar recorrência (conta) | — | +1 PLANNED (RECURRING) | — | — | — | 0 | +v | 0 |
| Confirmar recorrência (cartão) | +1 | — | — | +1 | — | +v | +v | 0 |

## 7. Regras de cálculo (funções puras)

- `installments.Split(total, n)`: `base = total/n`, as primeiras `total % n` parcelas recebem +1 (RN-010). Teste de fuzz garante que a soma é exata.
- `cardcycle.InvoiceFor(card, existingInvoices, purchaseDate)` e `cardcycle.Next(invoice, card)`: ver ADR 0003.
- `schedule.Occurrences(rule, from, to)`: mensal normaliza `day_of_month` para o último dia do mês; semanal usa `weekday` (0 = domingo); anual usa o dia/mês de `start_on` (29/02 vira 28/02 em ano não bissexto); respeita `interval_count` e `end_on`.
- `forecast.Month(...)`: compromissos por mês de vencimento = obrigações em aberto + saldo de faturas + ocorrências `PLANNED` de cartão projetadas na fatura calculada. Separa "previsto" (PLANNED, faturas `OPEN` futuras) de "confirmado".
