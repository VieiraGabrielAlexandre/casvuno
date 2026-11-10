# ADR 0001 — Monólito modular, módulos e estrutura do monorepo

- Status: Aceito (2026-11-10)
- Data: 2026-11-10

## Contexto
O README define um monólito modular em Go/Fx + Next.js + PostgreSQL. Ele lista módulos `transactions` e `obligations` separados e dois binários (`api`, `worker`). Também pede que o Fx fique só no composition root, mas o exemplo exporta `identity.Module`.

## Decisão
1. **Um repositório, um binário Go, um app web.** O binário `apps/api/cmd/casvuno` tem os subcomandos `serve`, `migrate`, `seed` e `admin`, todos com `flag` da stdlib, sem framework de CLI.
2. **Módulos de negócio:** `identity`, `households`, `categories`, `accounts`, `cards`, `ledger`, `recurrences`, `reporting`, `notifications`. O `ledger` junta transações, obrigações, pagamentos de obrigação e favoritos, porque compartilham invariantes e o caminho único de confirmação. Separados, criariam um ciclo de dependência.
3. **Fx só em `internal/app/`**, onde ficam um `fx.Module` por módulo, o lifecycle HTTP/worker e o pool. Pacotes de módulo não importam `go.uber.org/fx` e são testáveis com construtores comuns.
4. **Layout interno proporcional à complexidade:**
   - módulo simples (por exemplo `categories`): pacote plano com `service.go`, `postgres.go`, `http.go`;
   - módulo complexo (`ledger`, `cards`, `recurrences`): `domain/` (puro, sem IO), raiz do pacote com casos de uso, `postgres/` (sqlc gerado + adaptadores), `httpapi/`.
5. **Interfaces definidas pelo consumidor**, pequenas (1 a 3 métodos). Comunicação entre módulos só por essas interfaces, ligadas no composition root com `fx.As`. Nenhum módulo importa outro módulo de negócio, apenas `kernel` e `platform`.
6. **`internal/kernel`** (money, civil date, ids, erros tipados) é o único código compartilhado de domínio. Não existe pacote `utils`.
7. **`reporting`** é um módulo de leitura com consultas sqlc próprias que atravessam tabelas. Não grava nada e não importa outros módulos.
8. Categorias padrão pt-BR são criadas na criação da família. `seed` gera só dados de demonstração.
9. Sem Strategy/Factory até existir uma segunda implementação real.

## Estrutura
Ver `docs/IMPLEMENTATION_PLAN.md` §2. `packages/` existe no workspace pnpm, mas fica vazio até surgir um segundo consumidor TypeScript. A regra "Fx só em `internal/app` e `cmd`" é verificada pelo `depguard` em `apps/api/.golangci.yml`.

**Rotas por família (F1):** `internal/app/router.go` monta `/households/{householdId}` uma única vez, com `households.RequireMember`, e cada módulo registra ali as suas rotas relativas (`ScopedRoutes`/`Routes`). Assim nenhum módulo precisa importar `households` para herdar a autorização.

## Consequências
- Um deploy só, uma imagem de API e pouca RAM.
- Fronteiras explícitas sem custo de rede. A regra "não importar outro módulo" é verificada por `depguard` no golangci-lint.
- O `ledger` é o módulo maior. Se crescer demais, a divisão natural é `ledger/obligations` como subpacote interno.
