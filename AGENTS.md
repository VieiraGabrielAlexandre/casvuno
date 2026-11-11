# AGENTS.md — Instruções permanentes para agentes (LLMs) no Casvuno

Casvuno é uma aplicação web/PWA de gestão financeira **familiar**: um monólito modular em Go (Uber Fx) + Next.js (TypeScript) + PostgreSQL, com custo de infraestrutura próximo de zero.

## 1. Antes de qualquer trabalho

1. Leia `docs/implementation-status.md` para saber onde o projeto parou.
2. Leia a tarefa correspondente em `docs/IMPLEMENTATION_PLAN.md`. Trabalhe em **uma tarefa por vez**, na ordem de dependência.
3. Consulte `docs/domain-model.md` (invariantes INV-xx) e os ADRs relevantes em `docs/adr/`.
4. **Precedência** em caso de conflito: ADR com status *Aceito* > `docs/spec-review.md` (itens aprovados) > `docs/domain-model.md` > `README.md`. Se o conflito não estiver resolvido em nenhum deles, **pare e pergunte**; não decida silenciosamente.
5. Mudança de regra de negócio, de modelagem ou de stack exige um ADR novo (curto, numerado) e aprovação humana. Se houver incompatibilidade técnica, explique o trade-off e proponha a menor alteração. Nunca troque Go, Fx, PostgreSQL ou Next.js silenciosamente.
6. O `README.md` é a especificação de **produto**, voltada a pessoas. Backlog, ordem de execução e instruções para agentes ficam só aqui e em `docs/` (`IMPLEMENTATION_PLAN.md`, `implementation-status.md`, ADRs). Não adicione backlog nem instruções para LLM ao README.

### Como executar uma tarefa
- Fatia vertical, nesta ordem: migration → domínio → caso de uso → HTTP → OpenAPI/cliente TS → UI → testes. Cada incremento termina executável.
- Priorize correção financeira e lançamento rápido sobre gráficos ou acabamento visual.
- Não entregue scaffolding, pseudocódigo, endpoint sem persistência ou tela com dados mockados como funcionalidade pronta.
- Se o contexto acabar, retome pelo último item registrado em `docs/implementation-status.md`.

## 2. Regras inegociáveis

- **Não dividir despesas.** Nada de `split`, `share`, `settlement`, cotas, percentuais, reembolsos ou dívidas entre membros, em tabela, rota, tipo ou tela. `scope` e `person_member_id` servem só para filtro.
- **Dinheiro em centavos inteiros** (`money.Cents`/`int64`/`BIGINT`/`amountCents`). Nunca `float`, nem no frontend.
- **Três visões que nunca se somam:** consumo (`transactions`), compromissos (obrigações + saldo de faturas) e caixa (`account_movements`). Pagamento de fatura nunca é despesa. Parcelas nunca são somadas fora da fatura.
- **Isolamento por família em toda leitura e escrita.** O `householdID` vem da membership no contexto, nunca do corpo. Toda query tem `household_id`. FKs compostas. Recurso de outra família → 404/422.
- Histórico financeiro não se apaga: void, estorno e ajuste, sempre auditados.
- Mutações financeiras críticas exigem `Idempotency-Key` (lista no ADR 0007).
- Sem Kubernetes, Kafka, Redis, Elasticsearch, microserviços, SaaS pago ou autenticação terceirizada.
- Não implementar nada da lista "Fora da V1" (`docs/IMPLEMENTATION_PLAN.md` §3 e README §0).

## 3. Backend (Go) — convenções

- Layout: `apps/api/internal/<módulo>`. `go.uber.org/fx` **só** em `internal/app` e `cmd/`. Módulos não importam outros módulos de negócio; a comunicação é por interfaces pequenas **definidas pelo consumidor** e ligadas no composition root (`fx.As`).
- `domain/` é puro: sem IO, sem `time.Now()` (use `clock.Clock`), sem `context` de banco, sem HTTP/SQL/fx.
- Rotas de família: o módulo expõe `Routes(r)` com caminhos relativos (ex.: `/categories`) e `internal/app/router.go` as monta dentro de `/households/{householdId}`, já protegidas por `households.RequireMember` (use `households.RequireOwner` para ações administrativas). O handler lê `authctx.MembershipFrom(ctx)`. Toda rota nova sob `/households/{householdId}` entra em `householdRoutes` (suíte de isolamento); o teste de cobertura falha caso contrário.
- Rotas das mutações listadas no ADR 0007: `r.With(idem.Require).Post(...)` (`*idempotency.Middleware`, já no Fx). O handler roda dentro da transação da chave; os serviços devem gravar via `TxManager` (o `WithinTx` aninhado reaproveita a transação). Só respostas 2xx são memorizadas.
- Valores: `int64` com sufixo `Cents`, limitados por `kernel.MaxAbsCents`. Datas de calendário: `kernel.ParseDate`/`FormatDate` (`YYYY-MM-DD`; `time.Time` UTC no Go). Listagens paginadas: `httpx.ParseLimit`, `httpx.EncodeCursor`/`DecodeCursor` e `httpx.Page[T]` (keyset com ordenação estável).
- Textos para o usuário ficam em pt-BR dentro de strings; o `misspell` só verifica comentários.
- Casos de uso recebem `context.Context` e um `householdID` já autorizado, e devolvem erros de `kernel` (`ErrValidation`, `ErrNotFound`, `ErrConflict`, `ErrForbidden`, `ErrInvariant`), com `%w` para contexto.
- Operações com mais de uma escrita usam `postgres.TxManager.WithinTx` (stores obtêm a conexão com `tx.Conn(ctx)`). Locks com `SELECT … FOR UPDATE` conforme o ADR 0007. Auditoria e idempotência na mesma transação.
- SQL explícito em `db/queries/<módulo>.sql` com sqlc. Sem ORM, sem `Repository[T]`, sem SQL concatenado.
- DTOs HTTP separados das entidades. JSON em camelCase. Decode estrito.
- Proibido: pacote `utils`/`common`/`helpers`, `BaseService`, interfaces com uma única implementação sem consumidor que precise delas, `panic` fora de `main`, variáveis globais mutáveis, `init()` com efeitos colaterais.
- Identificadores, comentários e mensagens de commit em **inglês**. Mensagens exibidas ao usuário em **pt-BR**.
- `gofmt`/`goimports`; `golangci-lint` sem novos `//nolint` sem justificativa na mesma linha.

## 4. Banco e migrations

- golang-migrate com `NNNNNN_desc.up.sql` + `.down.sql`. **Nunca editar uma migration já mesclada**; criar uma nova.
- Constraints no banco sempre que possível (`CHECK`, `UNIQUE`, FKs compostas `(household_id, x_id)`, `ON DELETE RESTRICT`). Enum = `TEXT` + `CHECK`.
- `DATE` para datas de calendário e `TIMESTAMPTZ` para instantes.
- Novas invariantes verificáveis em SQL vão para `infra/sql/check-invariants.sql` (uma linha por violação). Os testes de integração de `internal/app` o executam ao fim do pacote e via `assertInvariants(t)`; `make check-invariants` roda contra o banco do Compose.

## 5. Frontend (Next.js) — convenções

- **Next.js 16.4 tem APIs diferentes das versões antigas:** antes de escrever código web, leia `apps/web/AGENTS.md` e a documentação embutida em `apps/web/node_modules/next/dist/docs/`.
- `output: 'export'`, dados só no cliente (TanStack Query), cliente tipado gerado de `docs/openapi.yaml` (`make gen`). **Nunca editar código gerado à mão.**
- `src/app` fino → `src/features/<domínio>` → `src/components/ui` e `src/lib`.
- Valores: use `lib/money.ts` (dígitos → centavos; formatação `pt-BR`/BRL).
- Mutações financeiras: `Idempotency-Key` por submissão, botão desabilitado enquanto pendente, nunca sucesso otimista. Offline → mensagem clara.
- Service worker nunca cacheia `/api/*` nem dados financeiros.
- UI pt-BR, mobile-first, acessível (labels, teclado, contraste AA). Toda métrica indica se é **consumo**, **compromisso** ou **caixa**.

## 6. API

- Prefixo `/api/v1`; rotas de família em `/households/{householdId}/…`.
- **Atualize `docs/openapi.yaml` na mesma mudança** que altera uma rota (spec-first). Os testes de API validam as respostas contra ele.
- Erros no envelope `{"error":{"code","message","details","requestId"}}`. Listagens com `limit` (máx. 100) + `cursor` e ordenação estável.

## 7. Checklist de segurança (toda mudança que toca rota ou dados)

- [ ] Rota sob `RequireMember` (e `RequireOwner` quando for administrativa) e incluída na suíte de isolamento.
- [ ] IDs do corpo carregados filtrando por `household_id`.
- [ ] Nenhum segredo, token, senha, valor ou descrição em log.
- [ ] Mutação financeira: idempotência, transação SQL, lock e evento de auditoria.
- [ ] Entrada validada (tags + regras de domínio); limites de tamanho.

## 8. Definição de pronto

Uma tarefa só está pronta quando:
1. A regra está implementada e coberta por testes (unitário de domínio + integração com PostgreSQL real; UI com Vitest). **E2E (Playwright) fica concentrado na F8.5** por decisão de 2026-11-10: não crie E2E antes disso, mas anote em `implementation-status.md` os fluxos de UI que a F8.5 deve cobrir.
2. Migration tem `up`/`down` e o ciclo up/down/up passa.
3. Autorização e validação estão corretas, e a suíte de isolamento foi atualizada.
4. `docs/openapi.yaml` e o cliente gerado estão atualizados.
5. A UI (quando houver) usa a API real, sem mocks, com estados de carregando, vazio, erro e sucesso.
6. Logs e auditoria estão adequados.
7. `make lint` e `make test` passam.
8. `docs/implementation-status.md` está atualizado.

## 9. Comandos

```
make setup          # .env, dependências web, módulos Go, golangci-lint (versão fixada)
make up / down      # PostgreSQL + migrations + API (Docker Compose) / parar
make web            # Next.js dev no host (http://localhost:3000, proxy para a API)
make dev            # up + web
make api            # API no host via go run (pare a api do Compose antes)
make test           # Go unit + integração (Testcontainers; exige Docker) + Vitest
make test-unit      # só testes sem Docker
make lint           # golangci-lint + gofmt/goimports + eslint + prettier + tsc
make fmt | build
make migrate-up | migrate-down [N=1]
make migrate-new name=snake_case   # par up/down sequencial
make gen            # sqlc + cliente TS a partir de docs/openapi.yaml (commite o resultado)
make check-invariants  # invariantes financeiras no banco do Compose (sem linhas = consistente)
```
Previstos no plano (ainda não existem): `seed`, `e2e`, `backup`, `restore-test`.

## 10. Honestidade e relato

- **Nunca** diga que testes, build ou lint passaram sem tê-los executado nesta sessão. Copie o resultado real.
- Ao terminar, informe: arquivos criados ou alterados, comandos executados com o resultado, decisões não previstas (com link para o ADR) e o próximo passo.
- Dependência nova só com justificativa (o que resolve e por que a stdlib ou uma dependência existente não serve).
- Se a tarefa exigir algo fora do escopo ou contraditório com estas regras, pare e pergunte.
