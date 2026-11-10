# ADR 0007 — Idempotência e concorrência

- Status: Proposto
- Data: 2026-11-10

## Decisão
**Rotas que exigem `Idempotency-Key`** (sem a chave: 400 `IDEMPOTENCY_KEY_REQUIRED`):
`POST transactions`, `POST transactions/{id}/void`, `POST obligations/{id}/payments`, `POST transfers`, `POST invoices/{id}/payments`, `POST invoices/{id}/close`, `POST invoices/{id}/adjustments`.

**Mecânica** (`platform/idempotency`, sem Redis):
1. A chave é uma string de 1 a 128 caracteres; o web gera um UUID por *submissão de formulário* e o reaproveita em retentativas.
2. `request_hash = SHA-256(método + rota-padrão + corpo canônico)`.
3. Dentro **da mesma transação SQL** da operação: `INSERT INTO idempotency_keys … ON CONFLICT (household_id, user_id, endpoint, key) DO NOTHING`.
   - Inseriu: executa o caso de uso, grava status e corpo da resposta na linha e faz commit.
   - Conflito: o PostgreSQL faz uma requisição concorrente com a mesma chave **esperar** o commit da primeira. Depois, `SELECT`: hash igual → *replay* com o mesmo status e corpo; hash diferente → **409** `IDEMPOTENCY_KEY_REUSED` (T09, T10).
4. Se a operação falhar, ocorre rollback e a chave não fica gravada, então o cliente pode tentar de novo.
5. Retenção de 7 dias. A limpeza diária fica a cargo do worker.

**Concorrência financeira** (READ COMMITTED + locks explícitos):
- pagamento de fatura: `SELECT … FROM card_invoices WHERE id=$ AND household_id=$ FOR UPDATE`, depois soma e verifica o teto;
- pagamento de obrigação e confirmação: `FOR UPDATE` na obrigação;
- inserção de parcela e fechamento: `FOR UPDATE` na fatura;
- void: `FOR UPDATE` na transação;
- transferência: sem verificação de saldo (ADR 0002); a atomicidade das duas pernas vem da transação SQL;
- jobs do worker: `pg_try_advisory_xact_lock(job_id)` + `ON CONFLICT DO NOTHING`.

**Teste T17:** N goroutines pagam o saldo total ao mesmo tempo, com chaves diferentes. Exatamente uma é aceita e as demais recebem 422 `AMOUNT_EXCEEDS_OUTSTANDING`.
