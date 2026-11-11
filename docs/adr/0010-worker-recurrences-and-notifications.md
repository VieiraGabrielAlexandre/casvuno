# ADR 0010 — Worker embutido, recorrências e lembretes

- Status: Aceito em 2026-11-11 (I-19 aprovado pelo usuário)
- Data: 2026-11-10

## Decisão
- **Worker embutido** no processo `serve` (`WORKER_ENABLED=true` por padrão), iniciado e parado pelo lifecycle do Fx. Um ticker a cada 5 min executa os jobs registrados. Cada job roda sob `pg_try_advisory_xact_lock`, é idempotente, tem timeout, faz até 3 retentativas com backoff e registra log de início, fim, duração e contagem. Falha de job não afeta a API, e `/health/ready` não depende do worker.
- **Jobs da V1:**
  1. `recurrences.generate`: gera ocorrências até `hoje + RECURRENCE_HORIZON_MONTHS` (padrão 12). Para regra com conta, cria a obrigação `PLANNED` (`source=RECURRING`). Para regra com cartão, cria só a ocorrência `PLANNED`.
  2. `recurrences.confirm_card`: para ocorrências de cartão `PLANNED` com `scheduled_on ≤ hoje`, cria transação + parcela única e marca `GENERATED`.
  3. `notifications.due`: avisos de vencimento (obrigações em aberto e faturas com saldo) `due_days_before` dias antes. `dedup_key = due:<tipo>:<id>:<due_on>`.
  4. `notifications.daily`: lembrete diário quando o horário local do usuário já passou de `daily_reminder_time`. `dedup_key = daily:<data local>`.
  5. `idempotency.cleanup`: remove chaves expiradas.
- **Editar regra "todas as futuras"**: em transação SQL, cancela as obrigações `PLANNED` sem pagamento com `scheduled_on ≥ hoje`, marca as ocorrências como `CANCELLED`/reaproveita a linha por `(rule_id, scheduled_on)` e regenera. O que já foi pago ou confirmado nunca é tocado (RN-015, T12).
- **Editar uma ocorrência:** `PATCH /obligations/{id}`, permitido só em `PLANNED` sem pagamentos.
- **Notificações:** apenas in-app na V1. Web Push/VAPID ficam para depois (I-25). Uma falha de canal nunca bloqueia uma operação financeira (RN-027).
