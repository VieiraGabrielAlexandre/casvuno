# Status de implementação

> Atualize este arquivo ao fim de cada tarefa (ver `AGENTS.md` §8). Registre apenas comandos efetivamente executados e seus resultados reais.

## Fase atual
F0 — Fundação. Entrega 1 concluída em 2026-11-10 (F0.1, F0.2, F0.3, F0.6, F0.8). **Parado aguardando revisão.**

## Concluído

### 2026-11-10 — Planejamento
`AGENTS.md`, `CLAUDE.md`, `docs/spec-review.md`, `docs/domain-model.md`, `docs/IMPLEMENTATION_PLAN.md`, `docs/adr/0001`–`0012`. I-01 aprovado (nome Casvuno).

### 2026-11-10 — Entrega 1: fundação técnica
- **Monorepo:** git, workspace pnpm (`apps/*`, `packages/*`), `packages/` reservado, `.env.example`, `.gitignore`, Makefile.
- **API (`apps/api`, Go 1.27.3):** `cmd/casvuno` (`serve`, `migrate up|down [N]`); `internal/app` (Fx: pool pgx com ping de inicialização, router chi, servidor HTTP com timeouts e shutdown gracioso, log Fx em debug); `platform/config` (env, fail-fast agregando erros); `platform/logging` (slog texto/JSON); `platform/health` (`GET /health`: 200/503, sem vazar o erro); `platform/postgres` (pool + golang-migrate com migrations embutidas); `db/migrations/000001_extensions` (citext); Dockerfile distroless não-root (imagem de 15 MB); `.golangci.yml` com `depguard` (Fx só em `internal/app` e `cmd`).
- **Web (`apps/web`):** Next.js 16.4.6 + React 19.3 + TS 5, Tailwind 4.3, shadcn/ui (preset base-nova, `button`), export estático no build e rewrites em dev, fontes do sistema, página pt-BR com status da API, ESLint + Prettier + `tsc`, Vitest 5.
- **Infra:** `infra/compose.yaml` (`db` postgres:18.6-alpine, `migrate` one-shot, `api`), portas só em 127.0.0.1.
- **Docs:** README ganhou a seção "Como executar"; plano F0 com status e itens movidos; ADRs 0001, 0008, 0009 e 0012 e `spec-review` I-31 atualizados (`/health` único).

## Comandos executados (entrega 1) e resultados reais
| Comando | Resultado |
|---|---|
| `make help` | lista 17 alvos |
| `make setup` | ok (lockfile íntegro, `go mod download`) |
| `make up` | db healthy → migrate exit 0 → api healthy; `curl 127.0.0.1:8090/health` → 200 `{"status":"ok","checks":{"database":"ok"}}` |
| `make migrate-down` / `make migrate-up` | "reverted 1 migration(s)" (citext ausente) / "migrations applied" (`schema_migrations` = 1, dirty = false) |
| `docker compose stop db` → `curl /health` | 503 `unavailable`; depois de `start db` → 200 |
| `docker compose stop api` | encerrou em 0,33 s, exit 0, log "http server shutting down" |
| `make web` + `curl :3000/health` e `:3000/` | proxy para a API ok; página com `lang="pt-BR"` e "Casvuno" |
| `casvuno serve` com `APP_ENV=staging` e sem `DATABASE_URL` | exit 1 listando os dois erros |
| `make lint` | golangci-lint 2.15.0: 0 issues; gofmt/goimports sem diff; ESLint 0 warnings; Prettier ok; `tsc` ok |
| `make test` | Go (race + integração Testcontainers): `internal/app`, `config` e `health` ok, incluindo `TestMigrationsUpDownUp` e `TestAppStartsServesHealthAndStops`; Vitest 2/2 |
| `make test-unit` | Go 3 pacotes ok; Vitest 2/2 |
| `make build` | binário Go ok; imagem `casvuno-api:dev` 15 MB; `next build` gerou export estático (`/`, `/_not-found`) |
| `make down` | stack removida (volume `db-data` mantido) |

Correções feitas durante a verificação: o golangci-lint precisou ser compilado com Go 1.27 (o Makefile agora usa o toolchain do `go.mod`); 3 achados `noctx` em testes corrigidos; `gosec` G101 (credenciais falsas em fixtures) excluído só em `_test.go`.

### 2026-11-10 — Pós-entrega 1 (preparação da etapa 2)
- Remoto: `github.com/VieiraGabrielAlexandre/casvuno` (primeiro push feito pelo usuário, commit `71de13c`).
- Aprovados I-12, I-13, I-22 e I-24 → ADRs 0001, 0005 e 0009 com status **Aceito**.
- Caminho do módulo Go: `casvuno/api` → `github.com/VieiraGabrielAlexandre/casvuno/apps/api` (imports e `goimports.local-prefixes` atualizados).
- `.gitignore` único na raiz (removido `apps/web/.gitignore`).
- F0.9: `.github/workflows/ci.yml` criado. Passos executados localmente: `make lint` (0 issues), `go mod tidy -diff` (limpo), `make test` (3 pacotes Go ok, incluindo integração; Vitest 2/2), `docker build` ok, `pnpm --filter web build` ok. O YAML foi validado com `yaml.safe_load`; o `actionlint` não rodou (execução bloqueada pela política de permissões da sessão).

## Próxima tarefa
Confirmar o CI verde no GitHub depois do push; em seguida, F1.1 (que absorve os itens movidos da F0).

## Pendências reais
- **Decisões de produto em aberto:** I-04, I-08, I-15, I-18, I-19 (`docs/spec-review.md`; afetam F3–F5). ADRs 0002, 0003, 0004, 0006, 0007, 0008, 0010, 0011 e 0012 continuam "Proposto".
- **CI ainda não executado no GitHub:** as alterações desta preparação não foram commitadas.
- **Web fora do Compose em dev** (decisão registrada no plano/ADR 0009). O web no Compose entra com o Caddy em F8.2.
- **`apps/web/AGENTS.md`** é gerado e reinserido pelo `next dev`; mantido versionado.
- O pnpm 12 marcou `sharp` e `unrs-resolver` como build não permitido (`allowBuilds: false`). O build e o lint funcionam sem eles; reavaliar se o ESLint import resolver precisar.
- `@types/node` está em `^22` e o TypeScript em `^5` (o create-next-app 16.4 usa TS 5; o TS 7 existe, mas não foi testado com o Next 16.4).
