# ADR 0012 — Observabilidade, auditoria e operação

- Status: Aceito em 2026-11-12 (aprovado pelo usuário)
- Data: 2026-11-10

## Decisão
- **Logs:** `log/slog`, JSON em produção e texto em dev. Middleware de acesso registra `request_id` (aceita `X-Request-ID` válido ou gera UUID; devolvido no header e no corpo de erro), método, **padrão da rota** (sem path cru), status, duração, `user_id` e `household_id`. Nunca registra corpo, senha, token, cookie, valores, descrições nem e-mail. Panics viram 500 com stack só no log.
- **Health:** `GET /health` na raiz da API (sem versão e sem autenticação): 200 `{"status":"ok","checks":{"database":"ok"}}` quando o banco responde ao ping em até 1 s, e 503 caso contrário, sem detalhes do erro. Um endpoint de liveness separado só será criado quando houver um orquestrador que o use. *(Revisado na entrega F0: o plano original previa `/health/live` + `/health/ready`.)*
- **Worker:** um log estruturado por execução de job (nome, duração, itens, erro).
- **Auditoria (`audit_events`, append-only; UPDATE/DELETE bloqueados por trigger no banco):** gravada na **mesma transação** da operação via `platform/audit.Recorder`. Cobre criação, edição, void, pagamentos, fechamento, ajustes, transferências, convites, membros e configurações. `metadata` contém só IDs e campos alterados (antes/depois de campos não sensíveis), sem segredos. Leitura por endpoint fica fora da V1 (consulta via SQL pelo operador).
- **Sem stack de métricas na V1** (sem Prometheus/Grafana/Sentry). Se necessário, `pg_stat_statements` para consultas lentas. Revisitar com uso real.
- **Backup:** `make backup` = `pg_dump -Fc` → cifrado com `age` (chave pública em env) → diretório local com rotação (7 diários + 4 semanais). Cópia off-site documentada com `rclone` para o destino escolhido pelo operador. `make restore-test` restaura num container temporário, roda `check-invariants.sql` e uma contagem básica. Recomenda-se agendar mensalmente via cron do host.
- **Produção:** `infra/compose.prod.yaml` com `caddy` (TLS automático, HSTS, CSP, web estático), `api` e `db` (sem porta publicada). Runbook em `docs/deployment.md`. Exclusão de família é um procedimento de operador documentado, sem endpoint.
