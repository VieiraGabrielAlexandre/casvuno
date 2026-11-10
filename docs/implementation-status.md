# Status de implementação

> Atualize este arquivo ao fim de cada tarefa (ver `AGENTS.md` §8). Registre apenas comandos efetivamente executados e seus resultados reais.

## Fase atual
F2 — Contas, idempotência e transferências: **F2.1 e F2.2 concluídas** (entregas 4 e 5, 2026-11-10; ainda não commitadas). Próxima: F2.3.

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

- CI verde no GitHub: PR #1 (`feature/add-ci`, run 42815181348, sucesso em 2m38s), mesclado em `main` (commit `172b7a0`). O usuário também validou a execução local.
- README sem backlog por sprint nem instruções para LLM (passaram para `AGENTS.md` e `docs/`).

### 2026-11-10 — Entrega 2: autenticação, usuários e famílias (F1)
- **Banco:** migrations `000002_identity` (users, sessions), `000003_households` (households, household_members, household_invites; FK composta do convidante), `000004_audit_events` (append-only por trigger).
- **Plataforma (itens movidos da F0):** `kernel` (erros tipados), `platform/clock` (+ `Fake`), `platform/postgres/tx.go` (TxManager via contexto), `platform/httpx` (decode JSON estrito, envelope de erro com `requestId`, `X-Request-Id`, recover, headers de API, CSRF `SameOrigin`, 404/405 em JSON), `platform/authctx`, `platform/audit`, `platform/ratelimit`, `platform/securetoken`; sqlc v1.31.1 (`internal/<módulo>/store`); `docs/openapi.yaml` 3.1; `make gen`; `make migrate-new`.
- **identity:** Argon2id em formato PHC com rehash no login; cadastro, login, logout e `/auth/me`; sessão opaca (hash SHA-256), cookie HttpOnly/SameSite=Lax (`__Host-` + Secure sob https), expiração deslizante de 30 dias com teto de 90 dias e renovação a cada 5 min; rate limit de login (5/min por IP e por e-mail); tempo igual para e-mail inexistente; `REGISTRATION_ENABLED` com exceção para convite válido; `casvuno admin reset-password` (revoga sessões).
- **households:** criar, listar, ver e editar família (OWNER); membros; convites de uso único (7 dias, só hash no banco, link `APP_ORIGIN/invite?token=`), revogar e aceitar; desativar membro preservando histórico, com a regra de ≥ 1 OWNER (`LAST_OWNER`); reativação volta como MEMBER; `RequireMember` (não membro → 404) e `RequireOwner` (→ 403); auditoria na mesma transação.
- **Web:** cliente tipado (`openapi-typescript` + `openapi-fetch`), TanStack Query, React Hook Form + Zod; páginas `/`, `/login`, `/register`, `/onboarding`, `/households`, `/household?id=`, `/invite?token=`; redirecionamento seguro `?next=`; erros da API nos campos; offline nunca vira sucesso. Removido o `ApiStatus` da entrega 1.
- **CI:** cache de golangci-lint + sqlc; falha se o código sqlc ou o cliente TS divergirem do que foi commitado.
- **Docs:** ADR 0005 (sessão deslizante, CSRF, `/auth/me`), ADR 0006 (implementado, suíte de isolamento), ADR 0008 (sqlc/TxManager), ADR 0012 (trigger de auditoria), `spec-review` I-39, `domain-model`, plano (F0.4/F0.5/F0.7/F1.x), README (primeiro acesso, senha esquecida), AGENTS.md.

## Comandos executados (entrega 2) e resultados reais
| Comando | Resultado |
|---|---|
| `go test -race -tags integration ./internal/app/` | 18/18 ok: cadastro/login/me/logout, validações, credenciais inválidas sem revelar conta, rate limit (6ª tentativa → 429), 401 em rotas protegidas e cookie forjado, CSRF (3 variações → 403), sessão deslizante, expiração ociosa e absoluta (relógio falso), cadastro fechado só com convite válido, reset de senha, família, convites e papéis (MEMBER → 403 `OWNER_REQUIRED`), convite de uso único e revogado, desativação + `LAST_OWNER` + reativação, **isolamento entre famílias** (todas as rotas → 404 e nenhuma linha alterada), cobertura de rotas via `chi.Walk`, trilha de auditoria + trigger, migrations up/down/up; toda resposta validada contra `docs/openapi.yaml` |
| 1ª execução da suíte acima | 2 falhas, ambas bugs dos testes (e-mail com maiúscula comparado sem normalizar; `MigrateDown(4)` em vez de 3), corrigidos |
| `make lint` | 1ª execução: 6 achados (gofmt ×2, errorlint, gosec G115, G124 ×2), corrigidos; G124 com `nolint` justificado (o `Secure` do cookie segue `APP_ORIGIN`); depois 0 issues, ESLint, Prettier e `tsc` ok |
| `make test` | 6 pacotes Go ok (app, identity, config, health, httpx, ratelimit); Vitest 14/14 (1ª execução: 2 falhas porque o `openapi-fetch` capturava `fetch` na importação; corrigido resolvendo `fetch` na chamada) |
| `go mod tidy -diff` | limpo |
| `make gen` ×2 (hash antes/depois) | idempotente |
| `make build` | binário ok; imagem `casvuno-api:dev` 18,7 MB; export estático com 8 rotas |
| `make up` + `make web` + curl pelo proxy :3000 | cadastro 201 (cookie HttpOnly, Path=/) → família 201 (OWNER) → convite 201 → 2º usuário aceita 200 (MEMBER) → MEMBER cria convite 403 → membros 200 → 3º usuário lê família alheia 404 → POST com Origin estranho 403 → sem cookie 401 → páginas `/ /login /register /onboarding /household /invite` 200 |
| `printf … \| compose run --rm -T api admin reset-password --email …` | senha antiga → 401, nova → 200 |

### 2026-11-10 — Entrega 3: categorias (F1.5) e mensagem de API indisponível
- **Banco:** `000005_categories` (2 níveis via FK composta, nome único entre irmãos ativos por índice parcial, `CHECK` de cor `#RRGGBB`, `UNIQUE(household_id, id)` para FKs futuras).
- **Módulo `categories`:** listar (filtro `kind`, `includeArchived`), criar (principal ou subcategoria: mesmo tipo, principal ativa, sem 3º nível, pai de outra família → 422 `PARENT_NOT_FOUND`), editar (nome, ícone, cor, ordem; tipo e principal imutáveis), arquivar e restaurar (arquivar a principal arquiva as filhas; restaurar filha exige principal ativa); `DELETE` = arquivar (idempotente); limite de 200; auditoria `category.created/updated/archived/restored`; qualquer membro ativo gerencia. 49 categorias padrão pt-BR criadas na mesma transação da família, via interface `households.CategorySeeder` (sem auditoria por item).
- **Router:** `/households/{householdId}` montado uma vez em `internal/app/router.go` (`households.ScopedRoutes` + `categories.Routes`).
- **Web:** página `/categories?household=…` (árvore por tipo, nova categoria com seleção de principal, arquivar/restaurar, mostrar arquivadas) + link na página da família. Cliente: 5xx sem envelope (proxy sem API) → `SERVER_UNAVAILABLE` "O servidor está indisponível no momento…". `make web` avisa quando `/health` não responde.
- **Lint:** `misspell` passou a verificar só comentários (as strings são pt-BR).
- **Docs:** `spec-review` I-40, plano (F1.5 ✅), `domain-model`, ADR 0001 (montagem das rotas por família), AGENTS.md, OpenAPI 0.3.0.

## Comandos executados (entrega 3) e resultados reais
| Comando | Resultado |
|---|---|
| `go test -race -tags integration ./internal/app/` | 1ª execução: 1 falha **real** (PATCH com `"color": ""` para remover a cor era barrado pela tag `len=7`); corrigida. Depois 23/23 ok, incluindo 4 testes novos de categorias e a suíte de isolamento com as 4 rotas novas (+ pai de outra família → 422, nenhuma linha alterada) |
| `make lint` | 1ª execução: 1 warning ESLint (`form.watch` × React Compiler → `useWatch`) e 4 falsos positivos do `misspell` em palavras pt-BR; corrigidos. Depois 0 issues |
| `make test` | 6 pacotes Go ok; Vitest 18/18 (novos: árvore de categorias, 5xx sem envelope → indisponível, 5xx com envelope mantém a mensagem) |
| `go mod tidy -diff` | limpo |
| `make gen` ×2 | idempotente |
| `make build` | ok; export estático com 9 rotas (nova `/categories`) |
| `make up` + `make web` + curl pelo proxy | migration 5 aplicada; família nova com 49 categorias (12 principais de despesa, 5 de receita); subcategoria 201; arquivar 204; `/categories` 200 |
| API parada (`compose stop api`) | proxy do Next responde `500 Internal Server Error` em texto puro (caso que o cliente agora traduz); `make web` imprime o AVISO; API religada e saudável |

### 2026-11-10 — Entrega 4: idempotência (F2.1)
- **Banco:** `000006_idempotency_keys` (unicidade por família + usuário + endpoint + chave; `response_status` só 2xx; `response_body bytea`, I-41; índice em `expires_at` para a limpeza futura).
- **`internal/platform/idempotency`:** middleware `Require` (ADR 0007): chave gravada na mesma transação da operação; handler executado dentro dela com resposta em buffer, enviada só após o commit; 2xx memorizado, qualquer outro status desfaz tudo; replay byte a byte com `Idempotent-Replayed: true`; 409 para a mesma chave com outra requisição (corpo canônico + caminho concreto); chave expirada é substituída. Registrado no Fx; nenhuma rota o usa ainda (primeira: F2.3 transferências).
- **Testes:** `internal/platform/postgres/pgtest` (PostgreSQL efêmero e migrado, agora compartilhado por `internal/app` e `idempotency`).
- **Docs:** ADR 0007 (implementação e refinamentos), `spec-review` I-41, `domain-model`, plano, OpenAPI 0.4.0 (parâmetro `Idempotency-Key` e cabeçalho `Idempotent-Replayed` em `components`), AGENTS.md (como aplicar o middleware).

## Comandos executados (entrega 4) e resultados reais
| Comando | Resultado |
|---|---|
| `go test -race -tags integration ./internal/platform/idempotency/` | 1ª execução: 2 falhas **reais**: o replay vinha de `jsonb`, que reordena chaves, então não era byte a byte igual ao original. Coluna trocada para `bytea` (I-41). Depois 7/7 ok: T09 (replay, inclusive com JSON em outra ordem e espaçamento), T10 (outro corpo e outro recurso → 409, sem novo efeito), chave ausente/inválida → 400, falha 422 não memorizada e repetível, **10 requisições concorrentes com a mesma chave → 1 efeito e 9 replays**, escopo por usuário/família, chave expirada roda de novo |
| mesmo pacote com `-count=10` nos testes de concorrência e T09 | ok (estável) |
| `go test ./internal/platform/idempotency/` (unitário) | ok: hash canônico (ordem/espaço ignorados; valor, ordem de array, caminho, método e inteiros grandes mudam o hash) e validação da chave |
| `make lint` | 0 issues; ESLint, Prettier e `tsc` ok |
| `make test` | 7 pacotes Go ok (incluindo `internal/app` com a migration 6 no ciclo up/down/up); Vitest 18/18 |
| `go mod tidy -diff` / `make gen` ×2 | limpo / idempotente |
| `make build` | ok |
| `make up` → `make migrate-down` → `make migrate-up` (stack real) | "reverted 1 migration(s)" → "migrations applied"; `schema_migrations` = 6, dirty = false; tabela presente; `/health` 200 |

### 2026-11-10 — Entrega 5: contas, saldo e extrato (F2.2, API)
- **Banco:** `000007_accounts` (`accounts` com titular por FK composta, `CHECK` INDIVIDUAL ⇔ titular, nome único entre ativas, saldo inicial com sinal; `account_movements` com FK composta para conta e estorno, `CHECK` REVERSAL ⇔ `reversal_of_id`, índice do extrato, **append-only por trigger** `forbid_update_delete`). `transfers` fica para a F2.3.
- **`kernel/values.go`:** `MaxAbsCents` (±10^15, exato em JSON) e datas `YYYY-MM-DD`. **`httpx/pagination.go`:** `limit`, cursor opaco e `Page[T]`. sqlc: `DATE` → `time.Time`.
- **Módulo `accounts`:** listar (com saldo e `totalBalanceCents`), detalhar, criar, editar (nome, tipo, titularidade, saldo inicial, data de abertura), arquivar só com saldo 0 (considerando a correção feita na mesma requisição), conta arquivada congelada até ser restaurada, extrato paginado (keyset `posted_on DESC, id DESC`, filtros `from`/`to`). Titular de conta individual validado por `households.IsActiveMember` (interface `accounts.MemberChecker`); membro de outra família → 422 `OWNER_NOT_MEMBER`. Auditoria `account.created/updated/archived/restored`. Qualquer membro ativo opera.
- **Docs:** OpenAPI 0.5.0 (contas, extrato, `Date`, `Cents`, `Limit`, `Cursor`), `spec-review` I-42, ADR 0004 (implementação), `domain-model`, plano (F2.2 ✅, migrations renumeradas a partir de 000008), AGENTS.md.

## Comandos executados (entrega 5) e resultados reais
| Comando | Resultado |
|---|---|
| `go test ./internal/kernel/ ./internal/platform/httpx/` | ok (datas, limite de centavos < 2^53, `limit`, cursor). 1ª execução: 1 falha **do teste** (`cursor=%%%` é query malformada e o Go descarta o parâmetro); trocado por um cursor que não é base64 |
| `go test -race -tags integration ./internal/app/` | ok na 1ª execução, com 6 testes novos: **T21** (conjunta + individual + negativa somadas uma vez; todo membro vê todas), **RN-017** (saldo = inicial + entradas − saídas, estorno incluso; transferência interna não muda o consolidado), movimentos rejeitam UPDATE/DELETE, extrato com 7 movimentos em páginas de 3 (sem duplicatas, ordem estável em datas iguais, filtros, 5 parâmetros inválidos → 400), 9 validações (incluindo valor fracionário e acima do limite), arquivar com saldo → 422 e conta arquivada congelada, titularidade e correção de saldo inicial; suíte de isolamento com as 5 rotas novas (+ titular de outra família → 422) e ciclo de migrations com a 7 |
| `make lint` | 0 issues; ESLint, Prettier e `tsc` ok |
| `make test` | 8 pacotes Go ok; Vitest 18/18 |
| `go mod tidy -diff` / `make gen` ×2 / `make build` | limpo / idempotente / ok |
| Stack real: `make up`, `migrate-down`, `migrate-up` + curl | versão 7, dirty = false; conta conjunta criada (201) e lista com `totalBalanceCents` = 150000 |

## Próxima tarefa
F2.3 — transferências: migration `transfers` + `transfer_id`, criação atômica com idempotência (primeiro uso do middleware), travando as duas contas e exigindo que estejam ativas; listagem; estorno de movimento. Depois F2.4 (telas de contas, extrato e transferência + `MoneyInput` + gerador de `Idempotency-Key`).

## Pendências reais
- **E2E adiado para a F8.5** (decisão do usuário, 2026-11-10). Fluxos de UI a cobrir lá, desta fase: categorias (criar principal e sub, arquivar principal esconde filhas, restaurar); cadastro → onboarding → família; login com `?next=`; gerar convite → abrir link deslogado → cadastro com convite → aceitar; MEMBER sem botões de OWNER; desativar membro; sair.
- **Famílias criadas antes da entrega 3 não têm categorias** (o seed roda só na criação). Em dev, crie uma família nova ou cadastre manualmente em `/categories`.
- **Rate limit por IP** usa o endereço TCP; atrás do Caddy (F8.2) será preciso confiar no cabeçalho do proxy, senão todos os usuários compartilham o mesmo limite.
- **Sem limpeza de sessões, convites e chaves de idempotência expirados:** ficam no banco (inofensivos, filtrados por data) até existir o worker (F5.2).
- **Página de convite** não mostra o nome da família antes do aceite (não há rota pública de pré-visualização, de propósito, para não expor dados a quem só tem o link).
- **Decisões de produto em aberto:** I-04, I-08, I-15, I-18, I-19 (`docs/spec-review.md`; afetam F3–F5). ADRs 0002, 0003, 0004, 0006, 0007, 0008, 0010, 0011 e 0012 continuam "Proposto"; o 0006 já está implementado e aguarda aceite formal.
- **Web fora do Compose em dev** (decisão registrada no plano/ADR 0009). O web no Compose entra com o Caddy em F8.2.
- **`apps/web/AGENTS.md`** é gerado e reinserido pelo `next dev`; mantido versionado.
- O pnpm 12 marcou `sharp` e `unrs-resolver` como build não permitido (`allowBuilds: false`). O build e o lint funcionam sem eles; reavaliar se o ESLint import resolver precisar.
- `@types/node` está em `^22` e o TypeScript em `^5` (o create-next-app 16.4 usa TS 5; o TS 7 existe, mas não foi testado com o Next 16.4).
