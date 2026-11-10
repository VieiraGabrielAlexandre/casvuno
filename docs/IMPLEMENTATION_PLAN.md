# Plano de implementação — Casvuno V1

> Único backlog do projeto (o README não tem mais backlog por sprint). Fonte de verdade de *produto*: `README.md`. Desvios e esclarecimentos: `docs/spec-review.md` + `docs/adr/`. Modelo e invariantes: `docs/domain-model.md`. Andamento: `docs/implementation-status.md`.

## 1. Como usar este plano

- Cada tarefa (`Fx.y`) é **pequena, vertical e verificável**: cabe em uma sessão/PR e termina com comandos que provam o aceite.
- Siga a ordem dentro de cada fase. Fases só começam quando as dependências listadas estão concluídas.
- Ao terminar uma tarefa: rode os comandos de verificação, atualize `docs/implementation-status.md` (feito, comandos executados e resultado real, decisões) e o mapeamento de testes (§4) quando for o caso.
- **Definição de pronto:** README §14 + `AGENTS.md` §8.

## 2. Estrutura de diretórios alvo

```text
casvuno/
├── AGENTS.md                      # instruções permanentes para LLMs
├── CLAUDE.md                      # aponta para AGENTS.md
├── README.md                      # especificação de produto
├── Makefile                       # interface única de comandos
├── .env.example
├── .editorconfig  .gitignore
├── package.json  pnpm-workspace.yaml  pnpm-lock.yaml   # workspace: apps/*, packages/*
├── .github/workflows/ci.yml
├── apps/
│   ├── api/
│   │   ├── cmd/casvuno/main.go    # serve | migrate | seed | admin
│   │   ├── internal/
│   │   │   ├── app/               # composition root: fx.New, fx.Module por módulo, router, lifecycle
│   │   │   ├── kernel/            # money, civil, ids, errors (sem dependências externas)
│   │   │   ├── platform/
│   │   │   │   ├── config/        # env → struct, validação, fail-fast
│   │   │   │   ├── logging/       # slog
│   │   │   │   ├── clock/         # Clock + fuso da família
│   │   │   │   ├── postgres/      # pool pgx, TxManager, migrations embutidas
│   │   │   │   ├── httpx/         # JSON, erros, paginação, middlewares (request id, log, recover, CSRF, headers)
│   │   │   │   ├── idempotency/
│   │   │   │   ├── audit/
│   │   │   │   └── worker/        # ticker, advisory lock, retry
│   │   │   ├── identity/
│   │   │   ├── households/
│   │   │   ├── categories/
│   │   │   ├── accounts/
│   │   │   ├── cards/             # domain/ (ciclo, split) · postgres/ · httpapi/
│   │   │   ├── ledger/            # domain/ · postgres/ · httpapi/
│   │   │   ├── recurrences/       # domain/ (agenda) · postgres/ · httpapi/
│   │   │   ├── reporting/
│   │   │   └── notifications/
│   │   ├── db/migrations/         # NNNNNN_*.up.sql / .down.sql
│   │   ├── db/queries/            # <módulo>.sql (sqlc)
│   │   ├── sqlc.yaml  .golangci.yml  go.mod  Dockerfile
│   └── web/                       # AGENTS.md próprio (gerado pelo Next 16)
│       ├── src/app/               # rotas App Router (finas)
│       ├── src/features/          # auth, households, accounts, cards, transactions, recurrences, reports, notifications, settings
│       ├── src/components/ui/     # shadcn/ui
│       ├── src/lib/api/           # schema.d.ts gerado + client.ts
│       ├── src/lib/               # money.ts, dates.ts, idempotency.ts
│       ├── public/                # manifest.webmanifest, sw.js, ícones
│       ├── e2e/                   # Playwright
│       └── next.config.ts  package.json  vitest.config.ts  playwright.config.ts
├── packages/                      # reservado; vazio até haver um 2º consumidor TS
├── docs/
│   ├── openapi.yaml
│   ├── domain-model.md  spec-review.md  IMPLEMENTATION_PLAN.md  implementation-status.md
│   ├── deployment.md              # (F8)
│   └── adr/
└── infra/
    ├── compose.yaml               # dev: db, migrate, api (web roda no host: make web)
    ├── compose.prod.yaml          # caddy(+web estático), api, db
    ├── Caddyfile
    ├── sql/check-invariants.sql
    └── scripts/backup.sh  restore-test.sh
```

**Comandos (Makefile):** existentes: `setup`, `up`, `down`, `logs`, `ps`, `dev`, `web`, `api`, `migrate-up`, `migrate-down`, `test`, `test-unit`, `lint`, `fmt`, `build`. Também existentes: `gen`, `migrate-new`. Previstos: `seed` (F1.5), `e2e` (F8.5), `backup`, `restore-test` e `check-invariants` (F8.3).

**Dependências previstas (todas gratuitas/OSS).** Go: `fx`, `chi/v5`, `pgx/v5`, `golang-migrate/v4`, `validator/v10`, `google/uuid`, `x/crypto` (argon2), `x/time` (rate); em teste, `testcontainers-go` e `kin-openapi`. Ferramentas: `sqlc`, `golangci-lint`. Web: `next`, `react`, `typescript`, `tailwindcss`, shadcn/ui (copiado), `@tanstack/react-query`, `react-hook-form`, `zod`, `@hookform/resolvers`, `recharts`, `openapi-fetch`; dev: `openapi-typescript`, `vitest`, `@testing-library/react`, `@playwright/test`, `eslint`, `prettier`. **Qualquer dependência fora desta lista exige justificativa no PR.**

## 3. Backlog ordenado por dependência

Legenda: **Dep.** = dependências; **Aceite** = critérios verificáveis; **Verificar** = comandos.

### F0 — Fundação

**F0 concluída em 2026-11-10:** F0.1, F0.2, F0.3, F0.6, F0.8 e F0.9. Por decisão desta entrega ("não criar pacotes sem uso real"), os itens que ainda não têm consumidor foram movidos para a primeira tarefa que precisa deles (coluna "Movido para").

| ID | Tarefa | Dep. | Aceite | Verificar | Status |
|---|---|---|---|---|---|
| F0.1 | `git init`, `.gitignore`, `CLAUDE.md`, Makefile (`help`, `setup`, `up`, `down`, `dev`, `web`, `api`, `migrate-*`, `test*`, `lint*`, `fmt`, `build`), `.env.example`, workspace pnpm (`apps/*`, `packages/*`) | — | `make help` lista os alvos; `make setup` funciona num clone limpo | `make help && make setup` | ✅ |
| F0.2 | `platform/config` (env, fail-fast com todos os erros), `platform/logging` (slog texto/JSON), `cmd/casvuno` (`serve`, `migrate up\|down [N]`) com Fx só em `internal/app`; servidor HTTP com timeouts e shutdown gracioso; router chi com request ID, log de acesso (padrão da rota) e recover | F0.1 | Config inválida encerra com exit 1 e lista os erros; SIGTERM encerra em menos de 10 s com exit 0 | `make test-unit`; `docker compose stop api` | ✅ |
| F0.3 | `platform/postgres`: pool pgx no lifecycle (ping na inicialização, falha rápida), migrations embutidas (golang-migrate, driver pgx/v5), migration `000001_extensions`; **`GET /health`** (200 com banco, 503 sem, sem detalhes do erro); `infra/compose.yaml` (`db`, `migrate` one-shot, `api`) | F0.2 | 503 sem banco e 200 com banco; ciclo `up/down/up` sem erro | `make migrate-down && make migrate-up`; teste de integração `TestMigrationsUpDownUp` | ✅ |
| F0.4 | `sqlc.yaml`, `TxManager` e testes de TxManager | F0.3 | stores gerados por módulo; transação via contexto | `make gen` | ✅ entregue na F1 (`internal/<módulo>/store`, `platform/postgres/tx.go`); coberto pelos testes de integração |
| F0.5 | Envelope de erro, decode JSON estrito, headers de segurança, `kernel/errors`, `platform/clock` | F0.2 | mapeamento de erros e decode testados | `go test ./internal/platform/httpx/...` | ✅ entregue na F1. Paginação keyset ↪ **F2.2**; `money`/`civil` ↪ **F2.2** |
| F0.6 | `apps/web`: Next.js 16.4 (App Router, TS), export estático só no build e rewrites só em dev (`next.config.ts` por fase), Tailwind 4, shadcn/ui (`button`), ESLint + Prettier + `tsc`, Vitest; página inicial pt-BR com status da API | F0.1 | `pnpm build` gera `out/`; testes do componente de status passam | `make lint-web test-web build` | ✅ (`lib/money.ts` ↪ **F2.4**, primeiro formulário com valor) |
| F0.7 | `docs/openapi.yaml` + `make gen` (sqlc + `openapi-typescript`) | F1.1 | `make gen` idempotente; CI falha se o código gerado divergir | `make gen` | ✅ entregue na F1; respostas dos testes de API validadas contra o contrato (kin-openapi) |
| F0.8 | Dockerfile da API (multi-stage, distroless estático, não-root, 15 MB) + `make up` com `--wait` | F0.3 | Clone limpo + `make setup && make up`: `curl 127.0.0.1:8090/health` 200; portas só em 127.0.0.1 | `make up` | ✅ O web roda no host (`make web`). No Compose ele entra com o Caddy de produção em **F8.2**, porque um container Node com bind mount gravaria arquivos como root no repositório |
| F0.9 | CI (GitHub Actions, `.github/workflows/ci.yml`): job **api** (`make lint-go`, `go mod tidy -diff`, `make test-go` com Testcontainers, `docker build`) e job **web** (`make lint-web`, `make test-web`, build estático), rodando os mesmos alvos do Makefile | F0.8 | Pipeline verde em push/PR para `main` | push | ✅ verde no GitHub (PR #1, run 42815181348) |

### F1 — Identidade e família

**F1 concluída (2026-11-10).** Entrega 2: F1.1–F1.4, F1.6–F1.8 (CI verde no PR #2). Entrega 3: F1.5. Os testes E2E de todas as fases ficam concentrados na F8.5 (decisão do usuário). F1.5 (categorias) ficou para a próxima entrega: é catálogo das funcionalidades financeiras e não fazia parte do escopo pedido.

| ID | Tarefa | Dep. | Aceite | Verificar | Status |
|---|---|---|---|---|---|
| F1.1 | Itens movidos da F0 (`kernel/errors`, envelope de erro + decode estrito, `platform/clock`, `sqlc.yaml` + `TxManager`, `openapi.yaml` + `make gen`, `make migrate-new`). Migration `users`/`sessions`; hasher Argon2id (PHC, rehash); `register`, `login`, `logout`, `me`; cookie de sessão; rate limit de login; `REGISTRATION_ENABLED` | F0.1–F0.3, F0.6, F0.8 | Sessão inválida, revogada ou expirada → 401; cookie com flags corretas por ambiente; e-mail inexistente e senha errada dão a mesma resposta; senha nunca aparece em log | `make test` | ✅ |
| F1.2 | Middleware CSRF (Origin/Sec-Fetch-Site + content-type) | F1.1 | POST com Origin estranho → 403; GET não é afetado | `make test` | ✅ |
| F1.3 | Migration `audit_events` + `platform/audit.Recorder` (na transação) | F0.4 | Evento gravado só em caso de commit (teste de rollback) | `make test` | ✅ |
| F1.4 | Migration `households`/`members`/`invites`; criar e listar famílias; `GET/PATCH /households/:id` (PATCH só OWNER); `RequireMember`/`RequireOwner` | F1.1, F1.3 | Não-membro → 404; MEMBER no PATCH → 403; criador vira OWNER | `make test` | ✅ |
| F1.5 | Migration `categories` + módulo `categories` (CRUD, arquivar, 2 níveis, `kind` coerente) + seed pt-BR na criação da família | F1.4 | RN-022: arquivar categoria referenciada preserva histórico; pai de outra família → 422; família nova já vem com categorias padrão | `make test` | ✅ (entrega 3: 49 categorias pt-BR na criação da família; DELETE = arquivar; página `/categories`) |
| F1.6 | Convites (criar, aceitar, revogar), listar membros, desativar membro (≥ 1 OWNER), aceite de convite no cadastro com registro fechado | F1.4 | Token só em hash; expirado/usado/revogado → 422; T16 (membro desativado perde acesso e o histórico é preservado) | `make test` | ✅ (+ `casvuno admin reset-password`, I-12) |
| F1.7 | Suíte de isolamento table-driven (ADR 0006) com rotas F1; cresce a cada fase | F1.4 | Teste falha se uma rota `/households/{id}` não estiver na suíte | `make test` | ✅ (`internal/app/households_integration_test.go`) |
| F1.8 | Web: layout (bottom nav/sidebar), login, cadastro, onboarding, seleção de família, aceitar convite, membros e categorias em Configurações | F1.6, F0.7 | E2E: A cadastra → cria família → gera convite → B aceita → B vê a família | `make e2e` | ✅ telas entregues e testadas com Vitest; E2E ↪ **F8.5** (decisão de 2026-11-10) |

### F2 — Contas, idempotência e transferências

**Entrega 4 (2026-11-10):** F2.1 concluída. Gerador de `Idempotency-Key` no web (`lib/idempotency.ts`) entra na F2.4, com o primeiro formulário financeiro.

| ID | Tarefa | Dep. | Aceite | Verificar |
|---|---|---|---|---|
| F2.1 | Migration `idempotency_keys` + `platform/idempotency` (ADR 0007) | F1.4 | T09, T10 e duas requisições concorrentes com a mesma chave → uma operação e um replay | `make test` | ✅ (entrega 4; middleware pronto, primeiro uso real em F2.3) |
| F2.2 | Migration `accounts`/`account_movements`/`transfers`; CRUD de contas (owner obrigatório se INDIVIDUAL), saldo (INV-09), extrato paginado; arquivar só com saldo 0 | F1.5 | T21; RN-017; ordenação estável no extrato | `make test` |
| F2.3 | Transferência atômica + listagem; estorno de movimento interno (usado depois pelo void) | F2.1, F2.2 | T04; origem = destino → 422; conta de outra família → 422 (T15); `check-invariants` INV-08 | `make test` |
| F2.4 | Web: contas (saldos, extrato), formulário de transferência, componente `MoneyInput`, padrão de mutação com idempotência e estado offline | F2.3 | Vitest em `MoneyInput`; E2E de transferência com consolidado inalterado | `pnpm -C apps/web test && make e2e` |

### F3 — Lançamentos (ledger)

| ID | Tarefa | Dep. | Aceite | Verificar |
|---|---|---|---|---|
| F3.1 | Migration `transactions`/`obligations`/`obligation_payments`; criar despesa/receita **imediata** (conta) | F2.3 | T01, T02, T03; categoria de `kind` errado → 422; INV-03/04/05 | `make test` |
| F3.2 | Despesa/receita **prevista**; `GET /obligations` (pendentes, vencidas, período); `POST /obligations/:id/payments` (parcial, confirma na 1ª, lock); `PATCH /obligations/:id` (PLANNED) | F3.1 | T22; consumo só aparece depois da confirmação; pagamentos concorrentes nunca excedem o valor | `make test` |
| F3.3 | Listar/filtrar transações (período, categoria, conta, pessoa, escopo, meio, texto) com keyset; detalhe; `PATCH` não estrutural; void de não-cartão (estorna movimentos) | F3.2 | RN-025; INV-19; void idempotente | `make test` |
| F3.4 | Migration `quick_templates`; CRUD de favoritos; "repetir último" (`GET transactions?createdBy=me&limit=1`) | F3.1 | Favorito de outra família/usuário inacessível | `make test` |
| F3.5 | Web: lançamento rápido (FAB `+`, valor em destaque, hoje por padrão, imediato/previsto), favoritos, lista de transações com filtros, pendências com pagamento, "Corrigir" (void + novo) | F3.4 | E2E: favorito completo confirmado em ≤ 3 interações; offline sem falso sucesso (T19 parcial) | `make e2e` |

### F4 — Cartões e faturas

| ID | Tarefa | Dep. | Aceite | Verificar |
|---|---|---|---|---|
| F4.1 | `cards/domain`: `Split` (RN-010, fuzz) e `cardcycle` (ADR 0003) puros | F0.2 | T06, T13, T14 e toda a tabela do ADR 0003 | `go test ./internal/cards/domain/...` |
| F4.2 | Migration `credit_cards` + CRUD (sem PAN/CVV, `last_four` opcional); arquivar sem saldo em aberto | F1.5 | Validação 1..31; outra família → 404 | `make test` |
| F4.3 | Migration `card_invoices`/`card_installments` (+ ALTER em `transactions`); compra no crédito no `ledger` (find-or-create de faturas, lock, deslocamento se CLOSED) | F4.1, F4.2, F3.1 | T05; INV-02/03; compra retroativa com `allocationShifted` | `make test` |
| F4.4 | Listar faturas por cartão/período; detalhe (parcelas, ajustes, pagamentos, saldo); fechar (manual, congela base) | F4.3 | INV-15; fechar antes de `closing_on` → 422 | `make test` |
| F4.5 | Migration `invoice_payments`/`invoice_adjustments`; pagamento parcial/total; ajustes | F4.4, F2.1 | T07, T08, T17; INV-06/07 | `make test` |
| F4.6 | Void de compra no cartão (política RN-019) + limite disponível estimado (RN-018) | F4.5, F3.3 | Void bloqueado com fatura CLOSED; limite = limite − saldo das faturas | `make test` |
| F4.7 | Web: cartões, faturas por mês (referência = vencimento), parcelas futuras, pagar fatura, fechar, ajustar, limite "estimado" | F4.6 | E2E: compra 6× → fatura → pagamento parcial → saldo correto | `make e2e` |

### F5 — Recorrências e worker

| ID | Tarefa | Dep. | Aceite | Verificar |
|---|---|---|---|---|
| F5.1 | `recurrences/domain.Occurrences` (mensal/semanal/anual, normalização, intervalo, fim) | F0.2 | Tabela de datas extremas (31, 29/02, virada do ano) | `go test ./internal/recurrences/domain/...` |
| F5.2 | `platform/worker` (ticker, advisory lock, timeout, retry, log) + job `idempotency.cleanup` | F2.1 | Duas instâncias não rodam o mesmo job em paralelo; falha não derruba a API | `make test` |
| F5.3 | Migration `recurring_rules`/`recurring_occurrences` (+ ALTER em `obligations`/`transactions`); CRUD de regras; job `recurrences.generate` | F5.1, F5.2, F3.2 | T11; horizonte de 12 meses; regra exige no máximo uma fonte | `make test` |
| F5.4 | `PATCH` de regra (futuras), `skip`, job `recurrences.confirm_card` | F5.3, F4.3 | T12; ocorrência de cartão vira compra só na data | `make test` |
| F5.5 | Web: recorrências (lista, próximas ocorrências, editar, pular) | F5.4 | E2E básico | `make e2e` |

### F6 — Relatórios, dashboard e exportação

| ID | Tarefa | Dep. | Aceite | Verificar |
|---|---|---|---|---|
| F6.1 | `reporting`: consultas separadas de consumo, compromissos e caixa, com um fixture cobrindo todos os comandos de `domain-model.md` §6 | F4.6, F5.4 | INV-10/11/12; T18 | `make test` |
| F6.2 | `GET /dashboard?month=` (formato do README §7.5 + `projectedBalanceCents`, I-34) | F6.1 | Cada bloco rotulado pelo tipo de métrica | `make test` |
| F6.3 | `GET /reports/forecast?months=12` (obrigações + faturas + ocorrências de cartão; previsto × confirmado) | F6.1 | RN-016 sem dupla contagem: compra 6× + pagamento parcial + recorrência | `make test` |
| F6.4 | `GET /reports/expenses?groupBy=category\|account\|card\|person\|scope` e `/reports/cashflow` | F6.1 | Totais de agrupamentos diferentes batem com o consumo total | `make test` |
| F6.5 | `GET /export/transactions.csv` (ADR 0004) | F6.4 | T20; filtros respeitados; BOM e `;` | `make test` |
| F6.6 | Web: dashboard, projeção, relatórios (Recharts) e exportação | F6.5 | Nenhum card soma consumo com caixa; E2E T18 | `make e2e` |

### F7 — Notificações

| ID | Tarefa | Dep. | Aceite | Verificar |
|---|---|---|---|---|
| F7.1 | Migration `notification_preferences`/`notifications`; GET/PATCH de preferências; listar e marcar como lida | F1.4 | Preferências por usuário e família; opt-in (padrão desligado) | `make test` |
| F7.2 | Jobs `notifications.due` e `notifications.daily` | F7.1, F5.2, F4.5 | Deduplicação (rodar 2× → 1 notificação); respeita o fuso | `make test` |
| F7.3 | Web: centro de notificações + preferências | F7.2 | Contador de não lidas atualiza | `make e2e` |

### F8 — PWA, segurança e operação

| ID | Tarefa | Dep. | Aceite | Verificar |
|---|---|---|---|---|
| F8.1 | Manifest, `sw.js` (só assets versionados e shell, **nunca** `/api`), banner offline, aviso de atualização | F3.5 | T19; teste garante que o SW ignora `/api/*` | `make e2e` |
| F8.2 | `compose.prod.yaml`, Caddyfile (TLS, HSTS, CSP, proxy), `docs/deployment.md` (runbook, variáveis, exclusão de família, rotação de segredos) | F0.8 | Stack prod sobe localmente com TLS interno do Caddy | `docker compose -f infra/compose.prod.yaml up` |
| F8.3 | `infra/scripts/backup.sh` + `restore-test.sh` + `check-invariants.sql` completo | F6.1 | `make backup && make restore-test` passa | idem |
| F8.4 | Revisão de segurança: suíte de isolamento cobrindo 100% das rotas, rate limit de convites, auditoria em todas as mutações, revisão de logs sem dados sensíveis | todas | Checklist do `AGENTS.md` §7 marcado | `make test` |
| F8.5 | **Suíte E2E (Playwright) de todo o produto** + `make e2e` + job no CI: cadastro → família → convite → aceite; gasto rápido por favorito (≤ 3 interações); compra parcelada → fatura → pagamento; dashboard separando métricas (T18); offline (T19); viewport mobile. Acessibilidade (contraste AA, teclado, labels) | F6.6, F7.3, F8.1 | E2E verde em desktop e mobile; os E2E adiados das fases F1–F7 cobertos | `make e2e` |

**Fora da V1 (não implementar sem nova aprovação):** tudo listado no README §0 "Fora da V1", Web Push, recuperação de senha por e-mail, troca de papel, exclusão de família por endpoint, edição estrutural de lançamentos, auto-close de fatura.

## 4. Mapeamento dos testes obrigatórios (README §13)

| ID | Tarefa | Nível |
|---|---|---|
| T01 | F3.1 | integração |
| T02, T03 | F3.1 + F6.1 | integração |
| T04 | F2.3 | integração |
| T05 | F4.3 | integração |
| T06 | F4.1 | unitário (+ fuzz) |
| T07, T08 | F4.5 | integração |
| T09, T10 | F2.1 | integração/API |
| T11 | F5.3 | integração |
| T12 | F5.4 | integração |
| T13, T14 | F4.1 | unitário |
| T15 | F1.7 (suíte crescente) | API |
| T16 | F1.6 | API |
| T17 | F4.5 | integração (concorrência) |
| T18 | F6.1 / F6.6 | integração + E2E |
| T19 | F3.5 / F8.1 | E2E |
| T20 | F6.5 | unitário + API |
| T21 | F2.2 | integração |
| T22 | F3.2 | integração |

## 5. Ordem das migrations

| # | Migration | Tarefa |
|---|---|---|
| 000001 | extensions (`citext`) | F0.3 ✅ |
| 000002 | users, sessions | F1.1 ✅ |
| 000003 | households, household_members, household_invites | F1.4 ✅ |
| 000004 | audit_events (append-only por trigger; FK para households) | F1.3 ✅ |
| 000005 | categories | F1.5 ✅ |
| 000006 | idempotency_keys (`response_body bytea`, I-41) | F2.1 ✅ |
| 000007 | accounts, account_movements, transfers | F2.2 |
| 000008 | transactions, obligations, obligation_payments | F3.1 |
| 000009 | quick_templates | F3.4 |
| 000010 | credit_cards | F4.2 |
| 000011 | card_invoices, card_installments, ALTER transactions (credit_card_id) | F4.3 |
| 000012 | invoice_payments, invoice_adjustments | F4.5 |
| 000013 | recurring_rules, recurring_occurrences, ALTER obligations/transactions | F5.3 |
| 000014 | notification_preferences, notifications | F7.1 |

A numeração é indicativa: se a ordem de entrega mudar, a sequência real segue a ordem de criação.

## 6. Riscos e mitigação

| Risco | Mitigação |
|---|---|
| Dupla contagem entre as três visões | Tabelas separadas por visão (ADR 0002), fixture único em F6.1, `check-invariants.sql` |
| Vazamento entre famílias | Três camadas (ADR 0006) + suíte que falha para rota não coberta |
| Erros de data em ciclo de cartão | Função pura + tabela exaustiva (ADR 0003) antes de qualquer persistência |
| Escopo crescendo além da V1 | Lista "Fora da V1" e regra no `AGENTS.md` |
| Next.js pesado | Export estático (ADR 0009) |
| Perda de dados | Backup cifrado + `restore-test` mensal com verificação de invariantes |
