# ADR 0007 — Idempotência e concorrência

- Status: Proposto; mecânica de idempotência implementada na F2.1 (2026-11-10)
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

**Implementação (F2.1, `internal/platform/idempotency`):**
- Middleware `(*idempotency.Middleware).Require`, aplicado por rota (`r.With(idem.Require).Post(...)`) em rotas montadas sob `/households/{householdId}` (exige a membership no contexto). O handler roda **dentro** da transação da chave: os serviços usam `TxManager.WithinTx`, que reaproveita essa transação.
- Refinamentos em relação ao item 2–4 acima:
  - `request_hash = SHA-256(método + caminho concreto + corpo JSON canônico)`: o caminho concreto (com IDs) faz a mesma chave em outro recurso (outra fatura) dar 409; o corpo é canonizado (ordem de chaves e espaços não importam; números preservados sem arredondamento). `endpoint` (parte da unicidade) = método + rota-padrão.
  - Só respostas **2xx** são memorizadas. Qualquer outra resposta faz rollback de tudo (operação e chave): o cliente pode repetir com a mesma chave.
  - A resposta fica em buffer e só é enviada **depois do commit**: nunca há sucesso respondido e não gravado.
  - Replay devolve os **mesmos bytes** (coluna `bytea`, não `jsonb`, I-41) com o cabeçalho `Idempotent-Replayed: true`.
  - Chave expirada (> 7 dias) é substituída e a requisição roda de novo. A limpeza periódica das expiradas fica para o worker (F5.2).
  - Erros: 400 `IDEMPOTENCY_KEY_REQUIRED`, 400 `IDEMPOTENCY_KEY_INVALID` (1–128 caracteres ASCII visíveis), 409 `IDEMPOTENCY_KEY_REUSED`.

**Concorrência financeira** (READ COMMITTED + locks explícitos):
- pagamento de fatura: `SELECT … FROM card_invoices WHERE id=$ AND household_id=$ FOR UPDATE`, depois soma e verifica o teto;
- pagamento de obrigação e confirmação: `FOR UPDATE` na obrigação;
- inserção de parcela e fechamento: `FOR UPDATE` na fatura;
- void: `FOR UPDATE` na transação;
- transferência (F2.3): `FOR UPDATE` nas duas contas **em ordem fixa de ID** (evita deadlock entre transferências em sentidos opostos) e exige contas ativas (evita a corrida com o arquivamento); sem verificação de saldo (ADR 0002); as duas pernas na mesma transação SQL. Testado com 20 transferências simultâneas em sentidos opostos;
- lançamento imediato (F3.1): `FOR SHARE` na categoria (impede arquivar no meio do lançamento sem serializar lançamentos) e depois `FOR UPDATE` na conta, **antes** de inserir as linhas que a referenciam. Inserir antes e travar depois causava deadlock entre lançamentos concorrentes: a FK pega `KEY SHARE` na conta e o `FOR UPDATE` posterior vira um *lock upgrade*. Testado com 20 lançamentos simultâneos na mesma conta;
- jobs do worker: `pg_try_advisory_xact_lock(job_id)` + `ON CONFLICT DO NOTHING`.

**Teste T17:** N goroutines pagam o saldo total ao mesmo tempo, com chaves diferentes. Exatamente uma é aceita e as demais recebem 422 `AMOUNT_EXCEEDS_OUTSTANDING`.
