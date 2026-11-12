# Status de implementação

> Atualize este arquivo ao fim de cada tarefa (ver `AGENTS.md` §8). Registre apenas comandos efetivamente executados e seus resultados reais.

## Fase atual
F4 — Cartões e faturas: F4.1 concluída (domínio puro; entrega 13 ainda não commitada). Próxima: F4.2 (`credit_cards` + CRUD).

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

### 2026-11-10 — Entrega 6: transferências (F2.3, API)
- **Banco:** `000008_transfers` (`transfers` com FKs compostas para as duas contas e o membro, `CHECK` origem ≠ destino, append-only por trigger; `account_movements.transfer_id` com FK composta e `CHECK (kind = 'TRANSFER') = (transfer_id IS NOT NULL)`).
- **`accounts/transfers.go`:** `CreateTransfer`: valida (contas distintas → 422 `SAME_ACCOUNT`; valor 1..10^15; descrição até 200, padrão "Transferência"), trava as duas contas em ordem fixa de ID e exige contas ativas (`ACCOUNT_NOT_FOUND`/`ACCOUNT_ARCHIVED` com o campo), grava a transferência + perna OUT + perna IN + auditoria numa transação. Saldo negativo permitido. `ListTransfers` paginado por cursor (filtros `accountId` em qualquer lado, `from`, `to`). Extrato passa a expor `transferId`.
- **HTTP:** `GET /transfers` e `POST /transfers` com `idempotency.Require` (**primeiro uso real do middleware da F2.1**).
- **Invariantes:** `infra/sql/check-invariants.sql` (INV-08 e INV-18) + `make check-invariants`; os testes de integração o rodam ao fim do pacote e em testes de dinheiro.
- **Escopo:** o estorno de movimento foi para a F3.3, com o void (primeiro consumidor), em vez de nascer sem uso.
- **Docs:** OpenAPI 0.6.0, plano (F2.3 ✅, F3.3 e F8.3 ajustadas), ADR 0007 (ordem de travamento), `domain-model`, AGENTS.md.

## Comandos executados (entrega 6) e resultados reais
| Comando | Resultado |
|---|---|
| `go test -race -tags integration ./internal/app/` | ok na 1ª execução com 6 testes novos: **T04** (R$ 500 move os dois saldos e mantém o caixa; extrato com as duas pernas ligadas à transferência; **T09/T10 pela API**: replay byte a byte com `Idempotent-Replayed`, mesma chave com outro valor → 409, dinheiro não se move de novo), 8 validações + chave ausente → 400 + saldo negativo permitido, listagem paginada e filtrada, **20 transferências simultâneas em sentidos opostos** (todas 201, sem deadlock, saldos voltam ao início), imutabilidade e perna órfã rejeitada, e o **verificador de invariantes detecta INV-08 e INV-18** plantadas num banco separado. Isolamento: 2 rotas novas + contas de outra família numa transferência → 422 `ACCOUNT_NOT_FOUND` sem gravar nada. Invariantes verificadas ao fim do pacote: nenhuma violação |
| `make lint` | 0 issues; ESLint, Prettier e `tsc` ok |
| `make test` | 8 pacotes Go ok; Vitest 18/18 |
| `go mod tidy -diff` / `make gen` ×2 / `make build` | limpo / idempotente / ok |
| Stack real: `migrate-down`/`migrate-up`, curl, `make check-invariants` | versão 8, dirty = false; transferência 201 e reenvio com a mesma chave 201 + `Idempotent-Replayed: true`; Corrente 150000 / Poupança 150000 / total 300000 (inalterado); nenhuma violação |

### 2026-11-11 — Entrega 7: telas de contas e transferências (F2.4, web)
- **`lib/money.ts`:** `formatDecimal`/`formatBRL` e `digitsToCents` trabalham sobre os dígitos como texto (sem `/ 100`, sem float); `MAX_ABS_CENTS` = limite da API. `formatDecimal` recusa número não inteiro em vez de arredondar.
- **`components/money-input.tsx`:** campo preenchido da direita (1, 2, 3, 4 → "12,34" = 1234 centavos), `inputMode="numeric"`, prefixo "R$", acima do limite mantém o valor anterior; `allowNegative` (saldo inicial) aceita "-" mesmo antes dos dígitos. `components/select-field.tsx` (select nativo com label e erro acessível).
- **`lib/idempotency.ts`:** `IdempotencyKeys` = uma chave por submissão: mesmo payload reaproveita a chave (retentativa após falha de rede vira replay no servidor), payload diferente gera outra (evita o 409), sucesso confirmado zera. UUID v4 com fallback para `getRandomValues` (HTTP sem TLS na rede de casa não tem `randomUUID`).
- **Offline:** `lib/use-online.ts` (`navigator.onLine` via `useSyncExternalStore`); faixa "Você está sem conexão" no `AppShell`; o formulário de transferência desabilita o envio offline. Falha de rede nunca vira sucesso (`call` já lançava `NETWORK_ERROR`); sem atualização otimista: saldos mudam só depois do 201 (invalidação das queries de contas e transferências).
- **`lib/dates.ts`:** `formatDate` (`AAAA-MM-DD` → `DD/MM/AAAA` sem `Date`, sem deslocamento de fuso) e `todayIn(timezone)` — a data padrão dos formulários é "hoje" no fuso da família.
- **Telas:** `/accounts?household=` (caixa consolidado com a etiqueta **caixa**, saldos por conta, arquivadas opcionais, nova conta), `/account?household=&id=` (saldo, editar enviando só os campos alterados, arquivar só com saldo zero — botão desabilitado e explicação —, restaurar, extrato com "Carregar mais" por cursor), `/transfers?household=[&from=]` (formulário com saldos nas opções, mensagem de sucesso só após a confirmação, histórico paginado; menos de 2 contas ativas → estado vazio com link). Links "Contas" e "Transferências" na página da família.
- **Testes Vitest novos (45):** `money` (formatação, limite, exatidão), `dates` (fuso de São Paulo vs. UTC), `idempotency`, `MoneyInput` (caixa registradora, backspace, limite, negativo, acessibilidade), `TransferForm` (validação sem chamar a API; centavos inteiros + `Idempotency-Key`; **retentativa após falha de rede reusa a chave e a transferência seguinte usa outra**; erro de campo da API; bloqueio offline), `AccountForm` (titular padrão, conjunta sem titular, titular obrigatório, PATCH só com o campo alterado, erro geral).
- **Sem mudança na API nem no contrato** (a F2.4 consome as rotas da F2.2/F2.3).

## Comandos executados (entrega 7) e resultados reais
| Comando | Resultado |
|---|---|
| `make lint` | golangci-lint 0 issues; ESLint, Prettier e `tsc` ok |
| `make test` | 8 pacotes Go ok; Vitest 63/63 (10 arquivos) |
| `pnpm --filter web build` | export estático com as rotas novas `/accounts`, `/account`, `/transfers` |
| Stack real + `next dev` (script de fumaça via proxy, mesmas chamadas da UI) | páginas 200; cadastro → família → 2 contas (total 300000); transferência 201 e reenvio com a mesma chave 201 + `Idempotent-Replayed: true`; Corrente 150000 / Poupança 150000 / total 300000; extrato com a perna OUT; arquivar com saldo → `ACCOUNT_BALANCE_NOT_ZERO`; PATCH só do nome ok |

Não houve teste interativo em navegador nesta sessão (fica para a F8.5 com Playwright).

### 2026-11-11 — Decisões de produto
I-04, I-08, I-15 (deslocar compra retroativa para a próxima fatura aberta), I-18 e I-19 aprovados pelo usuário; ADRs 0002, 0003 e 0010 passaram a "Aceito".

### 2026-11-11 — Entrega 8: lançamentos imediatos (F3.1, API)
- **Banco (`000009_ledger`):**
  - `transactions`: FK composta `(household_id, category_id, kind)`, de modo que o banco recusa categoria de receita numa despesa; `CHECK` de `PERSONAL` com pessoa (I-43); meio de pagamento sem `CREDIT_CARD` até a F4.3.
  - `obligations`: com os campos de rascunho do I-08 e `CHECK` de previsão ⇔ sem transação.
  - `obligation_payments`: append-only, um movimento por pagamento.
  - `categories`: ganhou `UNIQUE(household_id, id, kind)`.
- **Módulo `ledger` (novo):** `Create` grava, numa transação SQL, o movimento de caixa (que trava a conta), a transação, a obrigação `CONFIRMED`/`DIRECT`, o pagamento e a auditoria `transaction.created` (sem valor nem descrição nos metadados). Valida tipo, escopo, valor (1..10^15), descrição (1..200, aparada), estabelecimento (≤ 120), observações (≤ 1000; vazias viram `null`), modo `IMMEDIATE`, meio e conta. Referências do corpo verificadas na família: categoria (`CATEGORY_NOT_FOUND`/`CATEGORY_KIND_MISMATCH`/`CATEGORY_ARCHIVED`, com `FOR SHARE`), pessoa (`PERSON_NOT_MEMBER`), conta (`ACCOUNT_NOT_FOUND`/`ACCOUNT_ARCHIVED` no campo `payment.accountId`). `Get` por ID.
- **`accounts.PostMovement`:** único caminho para outro módulo gravar caixa (`DIRECT_PAYMENT`/`DIRECT_RECEIPT`). Trava a conta e exige conta ativa. O ledger o usa pela interface `ledger.CashPoster` (adaptador `ledgerCash` em `internal/app`); os módulos não se importam. `categories.LockForEntry` implementa `ledger.CategoryChecker`.
- **HTTP:** `POST /transactions` com `idempotency.Require`; `GET /transactions/{transactionId}` (adiantado da F3.3, para a F3.1 ser verificável pela API).
- **Invariantes:** `check-invariants.sql` ganhou INV-03 (fora do cartão), INV-04, INV-05 e a recíproca do INV-05 (movimento direto sem pagamento = caixa órfão).
- **Testes antigos:** os de contas deixaram de inserir movimentos `DIRECT_*` soltos no banco (agora seriam violação de INV-05) e usam lançamentos reais; o estorno continua inserido direto até a F3.3.
- **Bug encontrado pelo teste de concorrência e corrigido:** inserir a transação antes de travar a conta causava deadlock (FK pega `KEY SHARE`; o `FOR UPDATE` depois vira *lock upgrade*) e transformava conta desconhecida em 500. Agora o caixa é postado primeiro. Regra registrada no ADR 0007 e no AGENTS.md.
- **Docs:** OpenAPI 0.7.0, plano (F3.1 ✅, migration 000009 ✅), `domain-model`, ADR 0007, `spec-review` (I-43), AGENTS.md.

## Comandos executados (entrega 8) e resultados reais
| Comando | Resultado |
|---|---|
| `go test -race -tags integration ./internal/app/` | 1ª execução: 4 falhas reais (deadlock com 17 de 20 lançamentos concorrentes em 500; conta desconhecida → 500) e 1 erro no teste (comparação de ponteiros). Depois da correção: ok. |
| Testes novos de integração | **T01:** café de R$ 12,50 no Pix gera 1 despesa, saída `DIRECT_PAYMENT` e saldo −1250, com obrigação `CONFIRMED`/`DIRECT` paga por um pagamento; replay com `Idempotent-Replayed`, 409 com outro valor e nenhuma linha nova. **Receita:** entrada `DIRECT_RECEIPT`, obrigação `RECEIVABLE`, consumo 0. **T02/T03:** almoço pessoal de R$ 80 de um membro + mercado de R$ 300, ambos pagos pela conta individual de outro; consumo da família 38000, valores inteiros, cada membro lê o lançamento do outro. **Validação:** 20 casos com o campo certo no erro, nada gravado. **Banco:** recusa UPDATE/DELETE de pagamento, categoria de outro tipo, obrigação confirmada sem transação, `PERSONAL` sem pessoa e DELETE de transação. **Concorrência:** 20 lançamentos simultâneos na mesma conta, saldo exato. **Isolamento:** 2 rotas novas; categoria, conta ou membro de outra família → 422 sem gravar; transação de outra família → 404. **Verificador:** detecta INV-03, INV-04 e INV-05 plantadas |
| `go test ./internal/ledger/` | unitários de `normalize`: 13 rejeições com o campo certo e limites aceitos |
| `make lint` | 0 issues; ESLint, Prettier e `tsc` ok |
| `make test` | 9 pacotes Go ok (novo: `ledger`); Vitest 63/63 |
| `go mod tidy -diff` / `make gen` / `make build` | limpo / idempotente / ok (imagem e export estático) |
| Stack real: `make up`, `migrate-down`/`migrate-up`, curl, `make check-invariants` | versão 9, dirty = false; down/up ok; café de R$ 12,50 → 201 e reenvio com a mesma chave → 201 com `Idempotent-Replayed: true`; saldo −1250; extrato com um `OUT/DIRECT_PAYMENT`; nenhuma violação |

### 2026-11-11 — Entrega 9: lançamentos previstos e pagamento de obrigações (F3.2, API)
- **Previsão (`POST /transactions`, `mode=PLANNED`, `payment.dueOn`):**
  - grava só a obrigação `PLANNED`/`MANUAL_FORECAST` com o rascunho (I-08): sem transação, sem movimento e sem consumo;
  - a conta é opcional (se informada, precisa estar ativa) e `occurredOn` vira a data de competência;
  - recusa `merchant`/`notes` (I-44);
  - a resposta do endpoint passou a ser `{transaction, obligation}` nos dois modos (`transaction = null` na previsão).
- **Pagamento (`POST /obligations/{id}/payments`, idempotente):**
  - trava a obrigação, relê o total pago e recusa valor acima do saldo (`AMOUNT_EXCEEDS_OUTSTANDING`) ou obrigação quitada (`OBLIGATION_ALREADY_PAID`)/cancelada;
  - posta o movimento (saída para pagar, entrada para receber) na conta da obrigação ou no `accountId` informado;
  - o **primeiro pagamento confirma a previsão**: cria a transação com o valor total e `occurred_on = competência` e a vincula (status `CONFIRMED`), então o consumo aparece uma vez só (T22);
  - pagamentos parciais permitidos; auditoria `obligation.paid` + `transaction.created`.
- **Listagem e detalhe:**
  - `GET /obligations`: por vencimento, paginada por cursor; `view=open|overdue|all`, `kind`, `from`/`to`; `paymentStatus`, `outstandingCents` e `isOverdue` derivados dos pagamentos (I-02);
  - `GET /obligations/{id}`: obrigação + pagamentos;
  - `PATCH /obligations/{id}`: valor, vencimento e descrição, só em `PLANNED` (422 `OBLIGATION_NOT_PLANNED`).
- **Fuso da família:** a membership carrega o `timezone` da família; `kernel.Today(clock, tz)` define "hoje" para `isOverdue` e `view=overdue`.
- **Invariante INV-13** (previsão sem pagamentos) no `check-invariants.sql`.
- **Docs:** OpenAPI 0.8.0, plano (F3.2 ✅; cancelar previsão entra na F3.3), `spec-review` (I-44), ADR 0007, `domain-model`, AGENTS.md.

## Comandos executados (entrega 9) e resultados reais
| Comando | Resultado |
|---|---|
| `go test -race -tags integration ./internal/app/` | ok na 1ª execução. O `golangci-lint` apontou depois que eu tinha enfraquecido o teste de isolamento (o "nada mudou" das requisições entre famílias deixara de ser verificado); corrigido e reexecutado: ok. |
| Testes novos de integração | **Previsão de despesa:** sem consumo nem caixa até pagar; 1º pagamento parcial confirma com o valor total na data de competência; replay sem novo pagamento; quitação não cria segunda transação; pagar quitada → 422. **T22:** receita prevista sem conta exige `accountId`, depois vira uma receita só. **Listagem:** ordem por vencimento, `isOverdue`, `view` open/overdue/all, `kind`, período, paginação, 6 filtros inválidos → 400, e "vencida" muda com o relógio no fuso de São Paulo. **Regras:** edição de previsão, 4 PATCHs inválidos, 5 pagamentos recusados sem gravar nada, edição bloqueada depois de pago e em lançamento imediato, auditoria. **T17:** 10 pagamentos simultâneos do valor total: 1 aceito, 9 × 422, uma transação. **Isolamento:** 4 rotas novas; obrigação de outra família → 404; conta de outra família no pagamento → 422 sem gravar. **Verificador:** detecta INV-13 |
| `go test ./internal/ledger/ ./internal/kernel/` | ok (regras de previsão, situação de pagamento derivada, `kernel.Today` com fuso) |
| `make lint` | 0 issues; ESLint, Prettier e `tsc` ok |
| `make test` | 9 pacotes Go ok; Vitest 63/63 |
| `go mod tidy -diff` / `make gen` / `make build` | limpo / idempotente / ok |
| Stack real: `make up`, curl, `make check-invariants` | condomínio previsto de R$ 650 → `transaction: null`, `PLANNED`, saldo inalterado (100000); pagamento de R$ 200 → 201 e reenvio com `Idempotent-Replayed: true`; R$ 450 → `PAID`, 2 pagamentos; saldo 35000; nenhuma obrigação em aberto; nenhuma violação |

### 2026-11-11 — Entrega 10: listar, editar e cancelar lançamentos (F3.3, API)
- **`GET /transactions`:** mais recentes primeiro (`occurredOn`, `id` desc), paginação por cursor; filtros combináveis `from`/`to`, `kind`, `categoryId` (inclui subcategorias), `accountId`, `personMemberId`, `scope`, `paymentMethod`, `q` (texto sem diferenciar maiúsculas em descrição, estabelecimento e observações, até 100 caracteres) e `status` (`ACTIVE` padrão, `VOIDED`, `ALL`).
- **`PATCH /transactions/{id}` (I-18):** descrição, estabelecimento, observações, categoria (ativa, mesmo tipo), escopo e pessoa (membro ativo); `""` limpa os opcionais. A obrigação vinculada acompanha descrição e classificação. Lançamento cancelado → 422 `TRANSACTION_VOIDED`. Só o que mudou é validado e auditado (`transaction.updated` com nomes de campos).
- **`POST /transactions/{id}/void` (idempotente, ADR 0007):**
  - trava transação → obrigação → contas (por ID);
  - transação `VOIDED`, obrigação `CANCELLED`, um `REVERSAL` espelhado para cada pagamento (INV-18), datado de hoje no fuso da família ou da data do pagamento, se posterior;
  - os pagamentos ficam no histórico; consumo e compromisso somem e o dinheiro volta;
  - repetir com outra chave → 200 sem nada novo; conta arquivada envolvida → 422 `ACCOUNT_ARCHIVED`.
- **Estorno no `accounts`:** `ReverseMovements` (só pagamentos e recebimentos diretos; transferências seguem imutáveis, I-37), ligado ao ledger por `ledger.Cash.ReverseCash` no composition root.
- **`POST /obligations/{id}/cancel`:** previsão `PLANNED` → `CANCELLED` (auditado); já cancelada → 200 sem mudança; com pagamento → 422 `OBLIGATION_NOT_PLANNED` (cancele o lançamento).
- **INV-19** no `check-invariants.sql` (3 consultas): void e obrigação cancelada andam juntos; todo pagamento de obrigação cancelada está estornado; pagamento direto só é estornado com a obrigação cancelada.
- Sem migration: as colunas de status já existiam desde a 000009.
- **Docs:** OpenAPI 0.9.0, plano (F3.3 ✅), `spec-review` (I-45), ADR 0007 (ordem de locks do void), `domain-model` (INV-19 ✔︎SQL).

## Comandos executados (entrega 10) e resultados reais
| Comando | Resultado |
|---|---|
| `go test -tags integration ./internal/app/` (testes novos) | 1ª execução: `check-invariants.sql` com erro de sintaxe (o `;` da última consulta ficou antes do novo `UNION ALL`); corrigido. 2ª: só o teste de isolamento falhou, porque a família B agora tem um lançamento próprio a mais na listagem; asserção ajustada para "nenhum item da família A". 3ª: ok |
| Testes novos de integração | **Void imediato:** consumo 0, saldo restaurado, obrigação `CANCELLED`, `REVERSAL` IN com `reversalOfId` = pagamento e data de hoje, pagamento mantido, replay com mesma chave (mesmos bytes), outra chave sem novas linhas, auditoria, PATCH depois → 422. **Void de previsão paga em 2 contas:** 2 estornos, saldos restaurados, pagamento futuro estornado na própria data, novo pagamento → `OBLIGATION_CANCELLED`; void de receita → estorno OUT. **Conta arquivada:** 422 sem gravar; após restaurar, ok. **10 voids simultâneos:** todos 200, 1 estorno. **Cancelar previsão:** some das pendências, repetir é no-op, pagar/editar → 422, cancelar paga → 422, auditoria. **PATCH:** todos os campos, obrigação sincronizada, caixa inalterado, no-op sem auditoria, 12 entradas inválidas sem gravar. **Listagem:** 16 combinações de filtro (subcategoria, texto, cancelados), paginação limit=2 percorre tudo em ordem, 10 parâmetros inválidos → 400, membro vê tudo. **Isolamento:** 4 rotas novas; void/PATCH/cancel de outra família → 404; filtros com IDs de A em B → vazio; categoria/pessoa de A no PATCH em B → 422; contagem agora também detecta void, edição e cancelamento. **Verificador:** detecta INV-19 |
| `go test ./internal/ledger/` | ok (+ descrição do estorno truncada por caractere) |
| `make lint` | 0 issues; ESLint, Prettier e `tsc` ok |
| `make test` | 9 pacotes Go ok; Vitest 63/63 |
| `go mod tidy -diff` / `make gen` | limpo / idempotente |
| Stack real: `make up`, curl, `make check-invariants` | gasto de R$ 12,50 (saldo 98750); PATCH de descrição/estabelecimento; busca `q=PÃO` encontra "Pão Quente"; void → `VOIDED`, replay com `Idempotent-Replayed: true`, outra chave → 200; saldo 100000; extrato `REVERSAL IN 1250` em 2026-11-11 + `DIRECT_PAYMENT OUT`; lista padrão 0 / `ALL` 1; previsão cancelada (`outstandingCents` 0), 0 em aberto; nenhuma violação |

### 2026-11-11 — Entrega 11: favoritos e "repetir último" (F3.4, API)
- **Migration `000010_quick_templates`:**
  - favorito por membro (`member_id`, FK composta) com `kind` + categoria do mesmo tipo (FK composta), `payment_method`, `account_id`/`default_amount_cents` opcionais, `sort_order` e CHECK de pessoa em PERSONAL (I-43);
  - também cria um índice de `transactions` por autor, usado pelo "repetir último".
- **Rotas `/quick-templates`:**
  - `GET` lista só os do membro, por `sortOrder` e nome;
  - `POST` valida categoria ativa do mesmo tipo, conta ativa e pessoa membro ativo; por padrão entra depois do último; limite de 50 por membro, garantido sob concorrência por lock consultivo;
  - `PATCH`: ausente mantém, `""` limpa pessoa/conta e `defaultAmountCents: 0` limpa o valor; o tipo não muda; só o que muda é validado;
  - `DELETE` → 204;
  - auditoria `quick_template.created/updated/deleted`.
- **Privacidade (I-46):** favorito de outro membro da mesma família → 404 `TEMPLATE_NOT_FOUND`; de outra família → 404 `HOUSEHOLD_NOT_FOUND`.
- **Repetir último:** `GET /transactions` ganhou `createdBy=me|<memberId>`; `createdBy=me&limit=1` devolve o lançamento ativo mais recente de quem chama.
- **Docs:** OpenAPI 0.10.0 (tag `quick-templates`), plano (F3.4 ✅), `spec-review` (I-46), `domain-model`.

## Comandos executados (entrega 11) e resultados reais
| Comando | Resultado |
|---|---|
| `make gen` | 1ª: o sqlc recusou a query do lock consultivo (`@member_id::uuid::text`); trocada por uma chave em texto. 2ª e 3ª: o OpenAPI não carregou (`: ` e `"` sem aspas em descrições); corrigido. 4ª: ok |
| `go test -tags integration ./internal/app/` | 1ª: todos os testes falharam ao carregar o OpenAPI (uma vírgula numa descrição virou campo extra no schema); corrigido. 2ª: ok |
| Testes novos de integração | **Ciclo:** criar (ordem automática e explícita), listar em ordem, lançar a partir do favorito completo, editar e limpar campos, no-op sem auditoria, favorito com categoria arquivada continua editável, excluir (204 e depois 404), lançamento feito com ele intacto, auditoria. **Privacidade:** outro membro não vê, não edita e não exclui (404 sem gravar); cada membro tem sua lista. **Validação:** 15 criações e 8 edições inválidas sem gravar (`kind`, nome, valor, escopo, meio cartão, ordem, campo desconhecido, categoria de outro tipo/arquivada, conta arquivada/inexistente, pessoa inválida). **Limite:** com 46, 8 criações simultâneas → 4 aceitas, total 50, a seguinte → `TEMPLATE_LIMIT`. **Repetir último:** vazio sem lançamentos; devolve o próprio (ignora o do outro membro e o cancelado); filtro por autor; `createdBy=you` → 400. **Isolamento:** 4 rotas novas; editar/excluir favorito de A por B → 404; categoria, conta e pessoa de A em favorito de B → 422; "repetir último" de B sem lançamentos de A. `TestMigrationsUpDownUp` com 10 migrations |
| `go test ./internal/ledger/` | ok (edição e limpeza de campos, campos alterados, 7 regras inválidas) |
| `make lint` | 0 issues; ESLint, Prettier e `tsc` ok |
| `make test` | 9 pacotes Go ok; Vitest 63/63 |
| `go mod tidy -diff` / `make gen` | limpo / idempotente |
| Stack real: `make up`, `migrate-down`/`migrate-up`, curl, `make check-invariants` | versão 10, dirty = false; favorito "Café" R$ 12,50 criado e listado; lançamento com os campos dele; `createdBy=me&limit=1` → o café; edição limpa o valor; DELETE 204 e lista vazia; nenhuma violação |

### 2026-11-11 — Entrega 12: telas de lançamento, favoritos, lançamentos e contas a pagar (F3.5, web)
- **Lançamento rápido (`/entry`):**
  - botão `+` flutuante na família, nos lançamentos e nas contas a pagar;
  - despesa/receita, valor em destaque (`MoneyInput large`), categoria (subcategorias recuadas), descrição opcional (vazia = nome da categoria), hoje no fuso da família por padrão;
  - "Pago agora" ou "Previsto" (vencimento; conta opcional; sem estabelecimento/observações, I-44); conta, meio; "Mais detalhes" com escopo, pessoa, estabelecimento e observações (e o aviso de que não há divisão entre membros);
  - **favoritos** como botões (o completo preenche tudo e foca o "Lançar") e **"Repetir último"** (`createdBy=me&limit=1`, com a data de hoje);
  - `Idempotency-Key` por submissão, botão bloqueado enquanto envia e offline, sucesso só depois do servidor, erros `payment.*` da API ligados aos campos.
- **Corrigir (`/entry?correct=<id>`):** formulário preenchido com o lançamento; cancela o original e lança o novo (I-47).
- **Lançamentos (`/transactions`):**
  - filtros recolhíveis: texto, período, tipo, categoria, conta, meio, escopo, pessoa e situação, com contador;
  - lista paginada ("Carregar mais") com sinal e "consumo/receita";
  - cancelados riscados;
  - por item: detalhes, **Editar** (campos não estruturais, envia só o que mudou), **Corrigir valor ou data** e **Cancelar lançamento** com confirmação explicando o estorno.
- **Contas a pagar e receber (`/obligations`):**
  - "Compromissos" com filtros Em aberto/Vencidas/Todas e A pagar/A receber, situação (Prevista, Parcial, Vencida, Paga…) e quanto falta;
  - **Pagar/Receber** (valor sugerido = saldo em aberto, limitado a ele; conta da obrigação ou a única ativa; mensagem com o que falta);
  - **Editar previsão** e **Cancelar previsão** com confirmação.
- **Favoritos (`/favorites`):** listar, criar (tipo, nome, descrição, valor padrão opcional, categoria, conta opcional, meio, escopo, pessoa), editar (só o que mudou; 0/"" limpam) e excluir com confirmação.
- **Navegação:** a página da família ganhou Lançar, Lançamentos, Contas a pagar e receber e Favoritos.
- **Código:** `features/ledger` (hooks de transações, obrigações e favoritos, `entry.ts` puro, rótulos, formulário, botão `+`), `features/transactions`, `features/obligations`, `features/templates`; tipos novos em `lib/api/client.ts`. Sem dependência nova.

## Comandos executados (entrega 12) e resultados reais
| Comando | Resultado |
|---|---|
| `pnpm typecheck` | 1ª: erro de tipo na lista de consultas de `entry-view`; corrigido |
| `eslint` | 1ª: 3 avisos do React Compiler por `form.watch`; trocado por `useWatch` |
| `pnpm test` (Vitest) | 84/84 (21 novos): conversões favorito/último/correção → requisição (imediato, previsão sem estabelecimento/observações); favorito completo em 2 cliques + `Idempotency-Key`; validação sem chamar a API; previsão com vencimento e descrição = categoria; receita só oferece categorias de receita; repetir último com a data de hoje; erro `payment.accountId` no campo Conta; **corrigir**: void, falha de rede no novo (aviso), reenvio com a mesma chave, sem segundo void, botão bloqueado depois; offline bloqueia; pagamento sugere o saldo em aberto, recusa valor maior sem chamar a API e informa o que falta; filtros e PATCH só com o que mudou; favoritos (criar sem opcionais vazios, editar com 0/"") |
| `make lint` / `make test` | 0 issues, ESLint/Prettier/`tsc` ok / Go ok; Vitest 84/84 |
| `make build` | export estático com `/entry`, `/transactions`, `/obligations` e `/favorites` |
| Chrome headless (viewport 390×844) via DevTools Protocol, `make web` + API do Compose | botão `+` na família → `/entry`; toque no favorito "Café" preenche R$ 12,50; "Lançar" → "Despesa de R$ 12,50 lançada em 11/11/2026" (**3 interações**); lista mostra o café e o mercado; cancelar o mercado com confirmação → some da lista padrão; condomínio previsto aparece "Vencida"; Pagar sugere R$ 650,00 e, sem conta definida e com duas contas, pede a conta; com a conta → "paga por completo"; favoritos listados. Saldo final conferido pela API: 100000 − 1250 − 23490 + 23490 − 65000 = 33750 |

### 2026-11-12 — Entrega 13: regras puras de cartão (F4.1, domínio)
- Pacote `apps/api/internal/cards/domain` (sem IO, sem relógio, sem banco):
  - `Split(total, n)` (RN-010): `base = total/n`, as primeiras `total % n` recebem +1 centavo; `MaxInstallments = 60` (I-32); rejeita `n` fora de 1..60, total fora de 1..`MaxAbsCents` e parcela menor que R$ 0,01 (I-48).
  - `NewCycle(closingDay, dueDay)` (1..31), `PeriodFor(data)` (fatura com o menor fechamento **posterior** à compra; compra no dia do fechamento vai para a próxima; dias normalizados para o último dia do mês; vencimento = primeiro `due_day` estritamente depois do fechamento), `Next(período)` (encadeado no fechamento anterior).
  - `Allocate(faturasExistentes, data, n)`: usa a fatura existente que contém a data, senão continua a cadeia a partir da última existente (devolvendo em `Create` as que faltam, sem buracos), senão a regra atual; pula faturas `CLOSED` (`Shifted`); rejeita período calculado que se sobrepõe a uma fatura existente (`CARD_CYCLE_OVERLAP`) e cadeia inconsistente (`CARD_INVOICES_INCONSISTENT`, com teto de iterações contra laço infinito).
- Ainda sem rota, tabela ou tela: a F4.2 cria `credit_cards` e a F4.3 usa `Allocate` na compra no crédito.

## Comandos executados (entrega 13) e resultados reais
| Comando | Resultado |
|---|---|
| `go test -race ./internal/cards/domain/...` | ok: T06 (100,01 em 3× = 33,34 + 33,34 + 33,33), rejeições do `Split`; tabela do ADR 0003 (fechamento 31 em fevereiro bissexto e não bissexto, abril, dezembro→janeiro; `closing == due`, `due < closing`, `due > closing`; compra em fechamento −1, no fechamento (T14) e +1; 28/02 e 29/02 de ano bissexto (T13); 12× atravessando o ano; mudança de `closing_day` com faturas existentes; fatura fechada deslocando parcelas) |
| `go test -fuzz=FuzzSplit` (20 s e 15 s) / `-fuzz=FuzzCycle` (30 s e 20 s) | PASS (~4,9 milhões de execuções no `Split`): soma exata, parcelas ≥ 1, não crescentes, diferença ≤ 1; período contém a compra, ciclo de 28 a 31 dias, vencimento até 31 dias após o fechamento, `Next` = `PeriodFor(fechamento)`, parcelas encadeadas |
| Mutação manual (compra no dia do fechamento ficando no ciclo atual) | 1ª tentativa: o `sed` não casou e os testes passaram (sem efeito); 2ª: o teste travou num laço infinito do `walker` e derrubou a sessão. Corrigido com a checagem de período inválido e teto de iterações; 3ª: 11 testes falham (T13, T14, virada do ano, fuzz) em vez de travar; código restaurado |
| `make lint-go` | 1ª: `gofmt` no teste e falso positivo G602 do gosec; corrigidos sem `//nolint` → 0 issues |
| `make test-go-unit` | ok em todos os pacotes |

## Próxima tarefa
F4.2 — migration `credit_cards` + CRUD (sem PAN/CVV, `last_four` opcional; `closing_day`/`due_day` validados com `domain.NewCycle`); arquivar só sem saldo em aberto.

## Pendências reais
- **E2E adiado para a F8.5** (decisão do usuário, 2026-11-10). Fluxos a cobrir lá, **da F3.5:** `+` → favorito completo → Lançar em ≤ 3 interações; lançamento pago e previsto (com e sem conta); repetir último; descrição vazia vira a categoria; duplo clique/retentativa sem lançar em dobro; offline sem falso sucesso (T19 parcial); filtros da lista (texto, período, categoria com subcategorias, cancelados); editar lançamento; corrigir (original cancelado, novo lançado, saldo certo); cancelar lançamento e conferir o saldo; pagar previsão parcial e total, vencida, previsão sem conta pedindo a conta; editar e cancelar previsão; criar/editar/excluir favorito; favorito invisível para outro membro. **Das fases anteriores:** categorias (criar principal e sub, arquivar principal esconde filhas, restaurar); cadastro → onboarding → família; login com `?next=`; gerar convite → abrir link deslogado → cadastro com convite → aceitar; MEMBER sem botões de OWNER; desativar membro; sair. **F2.4:** criar conta individual e conjunta; saldo negativo no saldo inicial; editar; arquivar bloqueado com saldo ≠ 0 e permitido com saldo 0; restaurar; extrato com "Carregar mais"; transferir de A para B e conferir saldos e **consolidado inalterado**; duplo clique/retentativa sem duplicar (uma transferência só); offline: faixa e envio bloqueado, sem falso sucesso; origem = destino bloqueada no cliente.
- **Famílias criadas antes da entrega 3 não têm categorias** (o seed roda só na criação). Em dev, crie uma família nova ou cadastre manualmente em `/categories`.
- **Rate limit por IP** usa o endereço TCP; atrás do Caddy (F8.2) será preciso confiar no cabeçalho do proxy, senão todos os usuários compartilham o mesmo limite.
- **Sem limpeza de sessões, convites e chaves de idempotência expirados:** ficam no banco (inofensivos, filtrados por data) até existir o worker (F5.2).
- **Página de convite** não mostra o nome da família antes do aceite (não há rota pública de pré-visualização, de propósito, para não expor dados a quem só tem o link).
- **Decisões de produto:** I-04, I-08, I-15 (deslocar compra retroativa), I-18 e I-19 aprovados em 2026-11-11 (ADRs 0002, 0003 e 0010 aceitos); nenhum item [APROVAR] pendente. ADRs 0004, 0006, 0007, 0008, 0011 e 0012 continuam "Proposto"; o 0006 já está implementado e aguarda aceite formal.
- **Web fora do Compose em dev** (decisão registrada no plano/ADR 0009). O web no Compose entra com o Caddy em F8.2.
- **`apps/web/AGENTS.md`** é gerado e reinserido pelo `next dev`; mantido versionado.
- O pnpm 12 marcou `sharp` e `unrs-resolver` como build não permitido (`allowBuilds: false`). O build e o lint funcionam sem eles; reavaliar se o ESLint import resolver precisar.
- `@types/node` está em `^22` e o TypeScript em `^5` (o create-next-app 16.4 usa TS 5; o TS 7 existe, mas não foi testado com o Next 16.4).
