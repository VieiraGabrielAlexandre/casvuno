# ADR 0011 — Estratégia de testes

- Status: Aceito em 2026-11-12 (aprovado pelo usuário)
- Data: 2026-11-10

## Decisão
| Nível | Ferramenta | O que cobre | Onde |
|---|---|---|---|
| Unitário de domínio | `testing`, table-driven, **fuzz nativo** (`testing.F`) | split de parcelas (soma exata), ciclo de cartão, agenda de recorrência, projeção, CSV escaping, parsing de money | `internal/*/domain/*_test.go`, `internal/kernel` |
| Integração (PostgreSQL real) | `testcontainers-go` (um container por pacote via `TestMain`, migrations aplicadas uma vez; cada teste cria a **própria família**, sem truncate) | adaptadores sqlc, casos de uso transacionais, locks, idempotência, constraints do banco, concorrência (T17) | `internal/*/…_integration_test.go` com build tag `integration` |
| API | `httptest` sobre o app Fx completo + banco real | envelope de erro, status codes, cookies, CSRF, isolamento (suíte de todas as rotas), validação das respostas contra `docs/openapi.yaml` (`kin-openapi`, só em teste) | `internal/app/*_test.go` |
| Invariantes | `infra/sql/check-invariants.sql` | INV-02…INV-18 marcadas ✔︎SQL. Roda no fim de cada pacote de integração e no `make restore-test` | — |
| Frontend unitário | Vitest + Testing Library | input monetário, formatação, formulários, mapeamento de erros, estados vazio/erro/carregando | `apps/web/src/**/*.test.tsx` |
| E2E | Playwright (Chromium + viewport mobile) contra a stack do compose | fluxos críticos: cadastro→família→convite; gasto rápido por favorito; compra parcelada→fatura→pagamento; dashboard separando métricas; offline (T19) | `apps/web/e2e` |

**Regras:**
- Todo bug financeiro corrigido ganha primeiro um teste que reproduz o problema.
- Datas sempre via `Clock` falso. Não usar `time.Now()` em domínio ou caso de uso.
- Nada de mocks do banco para lógica com SQL: usar o banco real. Fakes manuais (sem framework de mock) só para interfaces entre módulos em testes unitários de caso de uso.
- Cobertura não é meta numérica, mas `domain/` de `ledger`, `cards` e `recurrences` deve ficar perto de 100% de linhas.
- **E2E adiado (2026-11-10):** a suíte Playwright é construída de uma vez na F8.5, cobrindo todas as fases. Até lá, a UI é coberta por Vitest e os fluxos pela API por testes de integração.
- `make test` = unitários + integração (exige Docker). `make test-unit` não precisa de Docker. `make e2e` sobe a stack.
- O mapeamento T01–T22 → teste fica em `docs/IMPLEMENTATION_PLAN.md` §4 e é mantido em dia.
