# Casvuno — Especificação do produto (MVP V1)

> **Status:** contrato de produto e engenharia para MVP V1.  
> **Idioma da interface:** português brasileiro (pt-BR). **Moeda:** BRL. **Fuso padrão:** America/Sao_Paulo.

## Como executar (desenvolvimento local)

**Pré-requisitos:** Docker com Compose v2, Go ≥ 1.21 (o toolchain fixado em `apps/api/go.mod`, hoje 1.27.3, é baixado automaticamente), Node.js ≥ 22 com Corepack (o pnpm fixado em `package.json` é ativado por ele) e GNU Make. Nenhum serviço pago.

```bash
make setup     # cria .env a partir de .env.example, instala dependências web, módulos Go e golangci-lint
make up        # PostgreSQL + migrations + API no Docker Compose (aguarda ficarem saudáveis)
curl http://127.0.0.1:8090/health      # {"status":"ok","checks":{"database":"ok"}}
make web       # Next.js em http://localhost:3000 (faz proxy de /api e /health para a API)
make down      # para a stack (o volume do banco é mantido)
```

| Comando | O que faz |
|---|---|
| `make dev` | `make up` + `make web` |
| `make api` | roda a API no host com `go run` (pare a `api` do Compose antes) |
| `make migrate-up` / `make migrate-down [N=1]` | aplica / reverte migrations (`apps/api/db/migrations`) |
| `make test` | testes Go (unitários + integração com PostgreSQL real via Testcontainers; exige Docker) + Vitest |
| `make test-unit` | testes que não precisam de Docker |
| `make lint` | golangci-lint + gofmt/goimports, ESLint + Prettier + `tsc` |
| `make fmt` | formata Go e web |
| `make build` | binário Go, imagem Docker da API e export estático do web (`apps/web/out`) |
| `make logs` / `make ps` | logs e estado da stack |

Portas no host (todas em `127.0.0.1`, configuráveis no `.env`): PostgreSQL `55432`, API `8090`, web `3000`. A configuração da API é feita só por variáveis de ambiente (`.env.example` documenta cada uma); uma configuração inválida faz o processo encerrar na inicialização com a lista de erros.

Estrutura: `apps/api` (Go, Uber Fx, chi, pgx, golang-migrate), `apps/web` (Next.js, TypeScript, Tailwind, shadcn/ui), `packages/` (reservado), `infra/` (Compose), `docs/` (plano, ADRs, modelo de domínio). Antes de contribuir, leia `AGENTS.md`; o andamento está em `docs/implementation-status.md`.

## 0. Objetivo e limites do produto

Construir uma aplicação web/PWA para controle financeiro de uma **família** (inicialmente casal), com múltiplos usuários, múltiplas contas e cartões, lançamento rápido de gastos cotidianos, despesas recorrentes, compras parceladas, faturas previstas e fechadas, fluxo de caixa e relatórios por período, categoria, conta, cartão, pessoa e fonte de pagamento.

**Regra fundamental: NÃO DIVIDIR DESPESAS.** A família é a unidade de consolidação. Um almoço de R$ 80 lançado por um membro representa R$ 80 de despesa da família, independentemente de quem consumiu ou pagou. É permitido indicar quem registrou, quem realizou o gasto, a conta/cartão utilizado e uma classificação `PERSONAL` ou `FAMILY` **apenas para organização e filtros**. Não calcular cotas, percentual de responsabilidade, reembolsos automáticos, saldos devidos entre membros ou qualquer tipo de rateio. Contas individuais e conjuntas coexistem; ambas integram a visão familiar. Todos os membros ativos podem visualizar todos os lançamentos e dados financeiros da própria família.

**V1 obrigatória:** autenticação, família/convites, contas, cartões, categorias, receitas, despesas, transferências, lançamentos rápidos e favoritos, recorrências, parcelamentos, faturas, pagamentos, projeção mensal, dashboard, filtros, exportação CSV, PWA responsivo e lembretes básicos configuráveis. **Fora da V1:** Open Finance, conexão automática a bancos, OCR, IA/LLM, WhatsApp, múltiplas moedas, multi-tenant empresarial, orçamento por envelope, anexos, investimentos, compartilhamento público, rateio, conciliação automática.

## 1. Princípios e prioridades (ordem de desempate)

1. Correção contábil e não duplicidade de valores.
2. Privacidade entre famílias, segurança e integridade transacional.
3. Custo recorrente próximo de zero; nenhuma dependência SaaS obrigatória.
4. Simplicidade, legibilidade, facilidade de manutenção e boa cobertura de testes.
5. Experiência mobile-first e lançamento de gasto em poucos toques.
6. Extensibilidade pragmática, sem abstrações prematuras.

Não introduzir Kafka, Kubernetes, Redis, Elasticsearch, event sourcing, CQRS completo, microserviços, service mesh, cloud proprietária ou autenticação terceirizada sem necessidade demonstrada. Uma aplicação única + PostgreSQL é suficiente.

## 2. Stack e decisões fechadas

| Área | Escolha | Observações |
|---|---|---|
| Backend | Go (versão estável suportada no momento da implementação) | `go.mod` fixado; `context.Context` em IO |
| DI / lifecycle | `go.uber.org/fx` | Apenas composição e lifecycle; sem service locator |
| HTTP | `net/http` + `github.com/go-chi/chi/v5` | Rotas, middleware e handlers simples |
| Persistência | PostgreSQL 16+ + `pgx/v5` + `sqlc` | SQL explícito e tipado; sem ORM mágico |
| Migrations | `golang-migrate/migrate` | SQL versionado up/down |
| Validação | `go-playground/validator/v10` + validações de domínio | Não confiar apenas em tags |
| Logs | `log/slog` | JSON em produção; request ID; sem dados sensíveis |
| Configuração | Variáveis de ambiente | `.env.example`; falhar cedo em configuração inválida |
| Auth | Sessões por cookies HttpOnly/Secure/SameSite | Senhas Argon2id; sessão opaca armazenada por hash no banco |
| Frontend | Next.js + React + TypeScript | App Router, UI pt-BR |
| UI | Tailwind CSS + shadcn/ui | Componentes acessíveis e mobile-first |
| Forms | React Hook Form + Zod | Validação compartilhada via OpenAPI quando possível |
| Fetch | TanStack Query | Cache e invalidação explícitos |
| Gráficos | Recharts | Gráficos leves, acessíveis |
| PWA | Manifest + service worker controlado | Instalação e app shell; sem cache de API privada |
| Monorepo | pnpm workspaces + Makefile | `apps/api`, `apps/web`, `docs`, `infra` |
| Testes backend | `testing`, `httptest`, `testcontainers-go` | Unitários e integração com PostgreSQL real |
| Testes frontend | Vitest + Testing Library + Playwright | Componentes e E2E |
| Infra local | Docker Compose | PostgreSQL, API, web; volumes persistentes |
| Hospedagem | Docker em máquina própria ou VPS econômica | Sem dependência de tier gratuito volátil |

**Alternativa econômica:** rodar API, web e PostgreSQL em uma máquina própria, mini-PC ou computador ligado continuamente. Custos de energia, internet, domínio e backup ainda existem; “zero nuvem” não significa zero custo. Para acesso remoto, preferir VPN privada (ex.: Tailscale em plano adequado) ou túnel com segurança apropriada. Não expor PostgreSQL à internet. Para produção pública, considerar VPS barata com TLS via Caddy e backups externos; verificar preços e limites atuais antes de escolher qualquer provedor. Não depender de SMTP comercial ou notificações push para funcionamento do núcleo financeiro.

## 3. Arquitetura pragmática e padrões

**Monólito modular**, cada módulo com `domain`, `application`, `infra`, `transport` apenas quando houver complexidade que justifique. Fluxo: HTTP handler -> use case/service -> interface de repositório (definida pelo consumidor) -> implementação PostgreSQL. Dependências apontam para dentro; domínio não importa HTTP, Fx, SQL, Next.js ou pacotes de infraestrutura.

- **Fx:** módulos `fx.Module(...)`, `fx.Provide(...)`, `fx.Invoke(...)`, lifecycle de servidor HTTP e worker. Não usar `fx` dentro de regras de negócio; `fx` só no composition root.
- **Repository:** interfaces pequenas por caso de uso, não repositório genérico `Repository[T]`.
- **Unit of Work / Transaction Manager:** operações atômicas para compra parcelada, fechamento/pagamento de fatura, transferência e geração de recorrências.
- **Strategy:** apenas quando há algoritmos de fato alternativos, como cálculo de ciclo de fatura; evitar factory/strategy para cada enum.
- **Domain services:** funções puras para calendário de parcelas, competência, rateio de centavos **entre parcelas apenas** (não entre pessoas), arredondamento e projeções.
- **DTOs separados das entidades:** nunca expor modelos de banco diretamente pela API.
- **Erros tipados:** `ErrNotFound`, `ErrConflict`, `ErrForbidden`, `ErrValidation`, `ErrInvariant`; mapear consistentemente para HTTP.
- **Idempotência:** `Idempotency-Key` nos POST financeiros críticos; chave por família + usuário + endpoint + chave; armazenar hash do payload e resultado; mesma chave com payload diferente retorna 409.
- **Auditoria:** eventos de criação, edição, exclusão lógica, pagamentos, cancelamentos, convites e alterações de configuração; sem segredos no payload.
- **Sem overengineering:** não criar `BaseService`, `AbstractController`, `GenericMapper` nem múltiplas interfaces que tenham uma única implementação sem motivo.

## 4. Glossário financeiro e contabilização

- **Família (`household`)**: fronteira de autorização e consolidação.
- **Membro**: usuário ativo da família. Pode registrar e filtrar operações; não implica divisão financeira.
- **Conta (`account`)**: origem/destino de dinheiro: banco, carteira, dinheiro, poupança; `INDIVIDUAL` ou `JOINT` com titular opcional; contas individuais **também** compõem o total familiar.
- **Cartão (`credit_card`)**: meio de pagamento vinculado a uma família, com titular, dia de fechamento, vencimento e limite; o cartão não é conta de caixa.
- **Transação (`transaction`)**: fato econômico único (`EXPENSE`/`INCOME`), com valor total, data de competência, categoria, pessoa associada e escopo de classificação (`PERSONAL`/`FAMILY`). Uma compra parcelada é **uma** transação de consumo.
- **Obrigação (`obligation`)**: compromisso a pagar/receber, previsto ou confirmado, com vencimento e valor; para cartão, uma obrigação por parcela vinculada à fatura; para conta recorrente, uma obrigação por ocorrência.
- **Movimentação (`account_movement`)**: entrada/saída efetiva em conta financeira. Pagamentos de cartão são movimentações de caixa, não novas despesas.
- **Transferência**: duas movimentações opostas em contas da mesma família, associadas por um identificador, sem receita/despesa.
- **Fatura (`card_invoice`)**: agrupamento de parcelas por cartão e ciclo, com estado `OPEN`, `CLOSED`, `PARTIALLY_PAID`, `PAID`, `CANCELLED` (cancelamento somente se permitido pelas regras).
- **Recorrência (`recurring_rule`)**: regra que gera obrigações futuras, sem duplicar ocorrências.

### 4.1 Três visões financeiras, sem confundir métricas

1. **Consumo por competência**: soma de `transactions` de despesa, contadas uma vez na data da compra; parcelamento não multiplica o consumo.
2. **Comprometimento por vencimento**: soma de obrigações por data de vencimento (incluindo parcelas de cartão); representa o que será exigível em cada mês.
3. **Fluxo de caixa**: soma de movimentos efetivamente realizados nas contas; transferência interna tem efeito líquido zero no consolidado.

**Regra de UX:** toda métrica deve indicar se representa consumo, compromissos ou caixa. Nunca somar os três indicadores em um “total de gastos”. Uma compra de R$ 1.200 em 6x é consumo de R$ 1.200 na data da compra e compromisso de R$ 200 por fatura; o caixa sai quando as faturas forem pagas.

### 4.2 Estados e alterações

- Transação: `ACTIVE`, `VOIDED`; preferir estorno/cancelamento auditável a exclusão física.
- Obrigação: `PLANNED`, `DUE`, `PARTIALLY_PAID`, `PAID`, `CANCELLED`; `DUE` pode ser derivado de vencimento e saldo, evitando job só para alterar status.
- Movimentação: `POSTED` ou `REVERSED`; reversão por movimento compensatório, nunca sobrescrever histórico sem auditoria.
- Fatura: `OPEN` até fechamento; `CLOSED` após congelar valor base; pagamentos parciais e ajustes ficam registrados.
- Receitas futuras podem ser previstas e posteriormente recebidas; a confirmação cria movimento na conta sem nova receita econômica.

## 5. Regras de negócio obrigatórias

**RN-001** Todas as entidades financeiras possuem `household_id`; o backend verifica a associação ativa em **toda** leitura/escrita, inclusive recursos por ID.  
**RN-002** Todos os membros ativos veem todas as despesas da família; `PERSONAL` é apenas filtro, nunca ACL.  
**RN-003** Nunca criar `expense_share`, `split`, `settlement`, `debt_between_members`, percentuais por membro ou endpoints equivalentes.  
**RN-004** Valores em `BIGINT` de centavos; moeda BRL; positivos no domínio, sinal somente nas movimentações; jamais `float` para dinheiro.  
**RN-005** Datas de calendário (`DATE`) separadas de instantes (`TIMESTAMPTZ`); timezone familiar configurável, padrão São Paulo.  
**RN-006** `amount_cents > 0`; nenhuma transação sem categoria válida da família; descrição obrigatória, limitada a 200 caracteres.  
**RN-007** Transferência interna gera duas movimentações atômicas de valores opostos, sem transação de receita/despesa; contas devem pertencer à mesma família e ser distintas.  
**RN-008** Uma despesa à vista por PIX/débito/dinheiro pode criar obrigação liquidada e movimento no mesmo comando; uma despesa prevista não gera movimento antes do pagamento.  
**RN-009** Compra no crédito gera transação + N obrigações/parcelas, vinculadas a faturas; não reduz caixa até pagamento da fatura.  
**RN-010** Parcelamento distribui centavos deterministicamente: `base = total/N`, primeiros `total % N` recebem +1 centavo; somatório exato; N entre 1 e 60 (configurável).  
**RN-011** Definir ciclo do cartão por `closing_day` e `due_day`; normalizar dias inexistentes para último dia do mês; implementar testes para fevereiro, ano bissexto, virada do ano e compra na data de fechamento. Regra V1: compra na data do fechamento entra no próximo ciclo; compras anteriores ao fechamento entram no ciclo atual, desde que o vencimento seja posterior ao fechamento. Documentar ciclo por datas concretas em vez de confiar só em mês/ano.  
**RN-012** Uma fatura fechada tem valor base congelado; ajustes posteriores geram registros auditáveis e atualização explícita de saldo, sem alterar silenciosamente parcelas originais.  
**RN-013** Pagamento parcial de fatura permitido; soma de pagamentos não pode ultrapassar saldo devido; pagamento gera `invoice_payment` e `account_movement` em uma transação SQL.  
**RN-014** Não registrar pagamento da fatura como `EXPENSE` adicional; relatórios de consumo usam transações originais.  
**RN-015** Recorrências geram ocorrências idempotentes por `(rule_id, occurrence_date)`; permitir editar apenas uma ocorrência ou todas as futuras; nunca reescrever histórico pago.  
**RN-016** Projeção de 12 meses deve considerar recorrências futuras, obrigações existentes e parcelas futuras **sem dupla contagem**; diferenciar previsto de confirmado.  
**RN-017** Saldo de conta = saldo inicial + soma de movimentos `POSTED` - compensações; não recalcular com transações de consumo.  
**RN-018** Limite disponível do cartão = limite - saldo de compras ainda não liquidadas (política V1: considerar parcelas em aberto de todas as faturas); informar que limite é estimado e não sincronizado com banco.  
**RN-019** Toda edição/cancelamento de compra parcelada já faturada exige tratamento explícito de ajustes e auditoria; na V1, bloquear edição estrutural após fechamento e oferecer cancelamento/estorno assistido apenas onde seguro.  
**RN-020** Pagamentos e transferências concorrentes devem usar transações e locks adequados para impedir pagamento duplicado e saldo inconsistente.  
**RN-021** Não permitir conta/cartão/categoria de outra família, mesmo que UUID seja conhecido.  
**RN-022** Categorias podem ser arquivadas, mas não apagadas se referenciadas; preservar histórico.  
**RN-023** Exclusão de membro não apaga lançamentos; preservar `created_by`/`person_id` e desativar vínculo.  
**RN-024** Exclusão de família exige procedimento explícito, confirmação e política de retenção; não implementar exclusão irreversível silenciosa.  
**RN-025** Toda resposta de listagem deve paginar e filtrar por família, período e campos permitidos, com ordenação estável.  
**RN-026** Exportação CSV respeita autorização, filtros e codificação UTF-8; prevenir CSV formula injection em campos de texto.  
**RN-027** Lembretes são opt-in, deduplicados por evento/período, configuráveis por usuário e não devem impedir operações financeiras quando o canal falhar.

## 6. Modelo relacional detalhado (PostgreSQL)

Convenções: IDs UUID gerados na aplicação ou DB, `created_at` e `updated_at` TIMESTAMPTZ, `household_id` em todas as tabelas de negócio, FKs e índices. Onde houver referência cruzada, usar FK composta `(household_id, referenced_id)` quando viável, com `UNIQUE (household_id, id)` no destino, para impedir vazamento cruzado também no banco. `ON DELETE RESTRICT` em histórico financeiro; usar arquivamento lógico. Não duplicar `household_id` nas tabelas de autenticação globais (`users`, `sessions`) que não são recursos de uma família.

### 6.1 Identidade e família

- `users`: `id PK`, `name`, `email CITEXT UNIQUE`, `password_hash`, `is_active`, timestamps.
- `sessions`: `id PK`, `user_id FK`, `token_hash UNIQUE`, `expires_at`, `revoked_at`, `last_seen_at`, `user_agent_hash`, timestamps; cookie contém apenas token aleatório, nunca senha/JWT em localStorage.
- `households`: `id PK`, `name`, `timezone DEFAULT 'America/Sao_Paulo'`, `currency DEFAULT 'BRL'`, `created_by FK users`, timestamps.
- `household_members`: `id PK`, `household_id FK`, `user_id FK`, `role ENUM OWNER|MEMBER`, `status ENUM ACTIVE|INVITED|INACTIVE`, `joined_at`, timestamps, `UNIQUE(household_id,user_id)`; V1 permite um usuário em mais de uma família, exigindo seleção explícita de família ativa.
- `household_invites`: `id PK`, `household_id`, `email`, `token_hash UNIQUE`, `invited_by`, `expires_at`, `accepted_at`, `revoked_at`; tokens nunca persistidos em claro.

### 6.2 Catálogo e fontes

- `categories`: `id PK`, `household_id`, `parent_id NULL`, `name`, `kind INCOME|EXPENSE`, `icon NULL`, `color NULL`, `is_active`, `sort_order`, timestamps; impedir ciclos na hierarquia e pai de outra família.
- `accounts`: `id PK`, `household_id`, `name`, `type CHECKING|SAVINGS|CASH|OTHER`, `ownership INDIVIDUAL|JOINT`, `owner_member_id NULL`, `opening_balance_cents BIGINT`, `opened_on DATE`, `is_active`, timestamps; `owner_member_id` obrigatório se INDIVIDUAL, nulo se JOINT (titulares de contas conjuntas podem ser todos os membros na V1, sem saldos separados).
- `credit_cards`: `id PK`, `household_id`, `name`, `holder_member_id`, `last_four NULL`, `limit_cents BIGINT`, `closing_day SMALLINT 1..31`, `due_day SMALLINT 1..31`, `is_active`, timestamps; não guardar PAN completo, CVV nem dados bancários sensíveis.

### 6.3 Fatos econômicos e compromissos

- `transactions`: `id PK`, `household_id`, `kind EXPENSE|INCOME`, `scope PERSONAL|FAMILY`, `person_member_id NULL`, `created_by_member_id`, `description`, `merchant NULL`, `amount_cents BIGINT`, `currency BRL`, `category_id`, `occurred_on DATE`, `notes NULL`, `status ACTIVE|VOIDED`, `recurring_occurrence_id NULL`, timestamps. `person_member_id` é somente filtro de atribuição, **não rateio**.
- `obligations`: `id PK`, `household_id`, `transaction_id NULL`, `kind PAYABLE|RECEIVABLE`, `source DIRECT|CARD_INSTALLMENT|RECURRING|MANUAL_FORECAST`, `amount_cents`, `due_on DATE`, `status PLANNED|CONFIRMED|PARTIALLY_PAID|PAID|CANCELLED`, `card_invoice_id NULL`, `installment_number NULL`, `installment_total NULL`, `recurring_occurrence_id NULL`, timestamps. Uma obrigação pode nascer de previsão sem transação de consumo; ao confirmar, vincular à transação em vez de criar previsão paralela. `paid_cents` deve ser derivado de `obligation_payments` ou mantido com atualização transacional e checagem.
- `obligation_payments`: `id PK`, `household_id`, `obligation_id`, `account_movement_id`, `amount_cents`, `paid_on DATE`, timestamps; cada pagamento aponta para um movimento efetivo; pagamento de fatura é controlado por `invoice_payments`, não pagar diretamente suas parcelas para evitar dupla liquidação.
- `card_invoices`: `id PK`, `household_id`, `credit_card_id`, `cycle_start_on DATE`, `closing_on DATE`, `due_on DATE`, `status OPEN|CLOSED|PARTIALLY_PAID|PAID`, `closed_base_amount_cents NULL`, timestamps; `UNIQUE(credit_card_id,cycle_start_on)`.
- `invoice_payments`: `id PK`, `household_id`, `card_invoice_id`, `account_movement_id UNIQUE`, `amount_cents`, `paid_on DATE`, timestamps. A quitação de fatura liquida seu saldo agregado; parcelas permanecem associadas ao ciclo para relatórios. `invoice_adjustments`: `id`, `household_id`, `invoice_id`, `amount_cents SIGNED`, `reason`, `created_by`, `created_at` para correções auditáveis.
- `account_movements`: `id PK`, `household_id`, `account_id`, `direction IN|OUT`, `amount_cents`, `posted_on DATE`, `kind DIRECT_PAYMENT|DIRECT_RECEIPT|INVOICE_PAYMENT|TRANSFER|REVERSAL|OPENING_ADJUSTMENT`, `transfer_id NULL`, `reversal_of_id NULL`, `description`, timestamps; `amount_cents > 0`; movimentações de caixa não são despesas econômicas adicionais.
- `transfers`: `id PK`, `household_id`, `from_account_id`, `to_account_id`, `amount_cents`, `posted_on`, `created_by_member_id`, timestamps; criar exatamente duas movimentações (`OUT` e `IN`) com `transfer_id`, atomicamente.

### 6.4 Recorrência, experiência e observabilidade

- `recurring_rules`: `id PK`, `household_id`, `name`, `kind INCOME|EXPENSE`, `amount_cents`, `category_id`, `scope`, `person_member_id NULL`, `frequency MONTHLY|WEEKLY|YEARLY`, `interval_count DEFAULT 1`, `day_of_month NULL`, `weekday NULL`, `start_on`, `end_on NULL`, `account_id NULL`, `credit_card_id NULL`, `is_active`, timestamps; escolher exatamente uma fonte quando apropriado.
- `recurring_occurrences`: `id PK`, `household_id`, `rule_id`, `scheduled_on`, `transaction_id NULL`, `obligation_id NULL`, `status PLANNED|GENERATED|SKIPPED`, timestamps, `UNIQUE(rule_id,scheduled_on)`.
- `quick_templates`: `id PK`, `household_id`, `user_id`, `label`, `description`, `default_amount_cents NULL`, `category_id`, `scope`, `person_member_id NULL`, `account_id NULL`, `credit_card_id NULL`, `sort_order`, timestamps.
- `notification_preferences`: `id PK`, `household_id`, `user_id`, `daily_reminder_enabled`, `daily_reminder_time`, `due_reminder_enabled`, `due_days_before DEFAULT 2`, `in_app_enabled`, `push_enabled`, timestamps, `UNIQUE(household_id,user_id)`.
- `notifications`: `id PK`, `household_id`, `user_id`, `kind`, `title`, `body`, `dedup_key`, `read_at NULL`, `created_at`, `UNIQUE(user_id,dedup_key)`; push delivery best-effort e opcional.
- `idempotency_keys`: `id PK`, `household_id`, `user_id`, `endpoint`, `key`, `request_hash`, `response_status`, `response_body JSONB`, `expires_at`, timestamps, `UNIQUE(household_id,user_id,endpoint,key)`; evitar armazenar segredos nas respostas.
- `audit_events`: `id PK`, `household_id`, `actor_user_id`, `entity_type`, `entity_id`, `action`, `metadata JSONB`, `created_at`; append-only.

**Índices mínimos:** `transactions(household_id,occurred_on DESC,id)`, `transactions(household_id,category_id,occurred_on)`, `obligations(household_id,due_on,status)`, `account_movements(household_id,account_id,posted_on)`, `card_invoices(household_id,credit_card_id,due_on)`, `notifications(user_id,read_at,created_at)`; índices parciais quando fizer sentido. Criar constraints de valores positivos, status, datas e unicidade no banco, além de validações Go.

### 6.5 Pagamento de fatura: regra de reconciliação

`invoice_total = SUM(obligations CARD_INSTALLMENT associadas) + SUM(invoice_adjustments.amount_cents)`. `invoice_paid = SUM(invoice_payments.amount_cents)`. `invoice_outstanding = invoice_total - invoice_paid`. Uma fatura parcialmente paga não gera automaticamente pagamentos individuais de parcelas; o status da fatura deriva do saldo. Para projeção, considerar o saldo da fatura e **não somar novamente** as obrigações das parcelas em separado. Para consumo, considerar apenas `transactions`. Em eventual estorno, criar ajuste e movimento de reversão conforme o dinheiro tenha sido devolvido; não apagar pagamentos anteriores.

## 7. Contratos HTTP REST V1

Prefixo `/api/v1`. JSON camelCase; valores em `amountCents` inteiros; datas `YYYY-MM-DD`; timestamps RFC3339. Auth por cookie seguro. Erros padronizados:

```json
{"error":{"code":"VALIDATION_ERROR","message":"Valor inválido","details":[{"field":"amountCents","reason":"must be greater than zero"}],"requestId":"uuid"}}
```

Paginação: `?limit=25&cursor=...` (máximo 100), resposta `{ "items": [...], "nextCursor": "..." }`. Mutação financeira exige header `Idempotency-Key`. Todos os endpoints `households/:householdId/...` verificam membership. API documentada em OpenAPI 3.1 em `docs/openapi.yaml`; gerar cliente tipado para web.

### 7.1 Autenticação e família

| Método | Rota | Objetivo |
|---|---|---|
| POST | `/auth/register` | Cadastro e sessão |
| POST | `/auth/login` | Autenticar e criar cookie |
| POST | `/auth/logout` | Revogar sessão |
| GET | `/auth/me` | Perfil e famílias |
| POST | `/households` | Criar família |
| GET | `/households` | Listar famílias do usuário |
| GET/PATCH | `/households/:id` | Detalhes/editar (OWNER para editar) |
| GET | `/households/:id/members` | Listar membros |
| POST | `/households/:id/invites` | Convidar (OWNER) |
| POST | `/invites/accept` | Aceitar token de convite |
| DELETE | `/households/:id/members/:memberId` | Desativar membro (OWNER, preservar histórico) |

### 7.2 Catálogo e contas

| Método | Rota | Objetivo |
|---|---|---|
| GET/POST | `/households/:id/categories` | Listar/criar categorias |
| PATCH/DELETE | `/households/:id/categories/:categoryId` | Editar/arquivar |
| GET/POST | `/households/:id/accounts` | Listar/criar contas |
| GET/PATCH | `/households/:id/accounts/:accountId` | Detalhar/editar |
| GET | `/households/:id/accounts/:accountId/movements` | Extrato de movimentos |
| GET/POST | `/households/:id/cards` | Listar/criar cartões |
| GET/PATCH | `/households/:id/cards/:cardId` | Detalhar/editar |
| GET | `/households/:id/cards/:cardId/invoices` | Faturas por período |
| GET | `/households/:id/invoices/:invoiceId` | Fatura, parcelas, saldo |
| POST | `/households/:id/invoices/:invoiceId/close` | Fechar fatura |
| POST | `/households/:id/invoices/:invoiceId/payments` | Pagamento parcial/total |

### 7.3 Operações

| Método | Rota | Objetivo |
|---|---|---|
| GET/POST | `/households/:id/transactions` | Listar/criar receita ou despesa |
| GET/PATCH | `/households/:id/transactions/:transactionId` | Detalhar/editar conforme regras |
| POST | `/households/:id/transactions/:transactionId/void` | Cancelar auditavelmente |
| GET | `/households/:id/obligations` | Pendências, previstas, vencidas |
| POST | `/households/:id/obligations/:obligationId/payments` | Pagar obrigação direta |
| POST | `/households/:id/transfers` | Transferir entre contas |
| GET | `/households/:id/transfers` | Histórico de transferências |
| GET/POST | `/households/:id/recurrences` | Listar/criar regras |
| PATCH | `/households/:id/recurrences/:ruleId` | Editar futuras ocorrências |
| POST | `/households/:id/recurrences/:ruleId/skip` | Pular ocorrência específica |
| GET/POST | `/households/:id/quick-templates` | Favoritos de lançamento |
| PATCH/DELETE | `/households/:id/quick-templates/:templateId` | Editar/excluir favorito |

### 7.4 Consultas e lembretes

| Método | Rota | Objetivo |
|---|---|---|
| GET | `/households/:id/dashboard?month=YYYY-MM` | KPIs e resumo do mês |
| GET | `/households/:id/reports/expenses?from=&to=&groupBy=` | Consumo por categoria/conta/cartão/pessoa |
| GET | `/households/:id/reports/cashflow?from=&to=` | Caixa realizado e projetado |
| GET | `/households/:id/reports/forecast?months=12` | Compromissos dos próximos meses |
| GET | `/households/:id/export/transactions.csv?from=&to=` | Exportação CSV |
| GET/PATCH | `/households/:id/notification-preferences` | Preferências do usuário atual |
| GET | `/households/:id/notifications` | Caixa de notificações |
| POST | `/households/:id/notifications/:notificationId/read` | Marcar lida |
| GET | `/health/live` | Liveness sem DB |
| GET | `/health/ready` | Readiness com DB |

### 7.5 Exemplos de payload

**Criar gasto simples por Pix (imediato):**

```http
POST /api/v1/households/{householdId}/transactions
Idempotency-Key: 6c5d...
Content-Type: application/json
```

```json
{"kind":"EXPENSE","scope":"PERSONAL","personMemberId":"uuid-membro","description":"Café","amountCents":1250,"occurredOn":"2026-11-10","categoryId":"uuid-categoria","payment":{"method":"PIX","accountId":"uuid-conta","mode":"IMMEDIATE"}}
```

**Criar compra parcelada no crédito:**

```json
{"kind":"EXPENSE","scope":"FAMILY","description":"Geladeira","amountCents":240000,"occurredOn":"2026-11-10","categoryId":"uuid-categoria","payment":{"method":"CREDIT_CARD","cardId":"uuid-cartao","installments":10}}
```

**Criar gasto futuro sem pagar:**

```json
{"kind":"EXPENSE","scope":"FAMILY","description":"Condomínio","amountCents":65000,"occurredOn":"2026-12-01","categoryId":"uuid-categoria","payment":{"method":"BANK_TRANSFER","accountId":"uuid-conta","mode":"PLANNED","dueOn":"2026-12-10"}}
```

**Transferir:**

```json
{"fromAccountId":"uuid-conta-a","toAccountId":"uuid-conta-b","amountCents":50000,"postedOn":"2026-11-10","description":"Reserva"}
```

**Pagar fatura:**

```json
{"accountId":"uuid-conta","amountCents":50000,"paidOn":"2026-11-10"}
```

**Resposta de dashboard:**

```json
{"month":"2026-11","consumption":{"expensesCents":350000,"incomeCents":1000000},"commitments":{"dueCents":420000,"outstandingCents":150000},"cashflow":{"inCents":1000000,"outCents":310000,"netCents":690000},"accounts":{"totalBalanceCents":1250000},"upcomingBills":[],"expenseByCategory":[],"recentTransactions":[]}
```

**Importante:** no exemplo de gasto `PLANNED`, a operação pode criar apenas a obrigação prevista e seu rascunho de origem, sem contabilizar consumo realizado até confirmação. Definir no código um único caminho de confirmação que vincule obrigação e transação, sem duplicar registros.

## 8. Interface, páginas e UX

### 8.1 Navegação

- `/login`, `/register`, `/onboarding`, `/household/select`.
- `/dashboard`: mês selecionável, receitas, consumo, contas a vencer, faturas, saldo de caixa, saldo projetado, lançamentos recentes; cards deixam explícito o tipo de métrica.
- `/transactions`: lista filtrável, pesquisa, edição/cancelamento e exportação.
- `/transactions/new`: formulário rápido; valor em destaque, descrição, categoria, data, conta/cartão, modo imediato/previsto, parcelas, escopo e pessoa opcional.
- `/accounts`: contas individuais/conjuntas, saldos e extratos.
- `/cards`: cartões, limites estimados, faturas por mês, parcelas futuras, pagamento.
- `/recurrences`: assinaturas, contas fixas, próximas ocorrências, edição de regra.
- `/forecast`: projeção mensal 12 meses; detalhamento de compromissos por vencimento.
- `/reports`: filtros e agrupamentos por categoria, pessoa, conta, cartão e período.
- `/settings`: família, membros, categorias, favoritos, notificações, preferências.

### 8.2 Requisitos de experiência

- Mobile-first, menu inferior em telas pequenas, sidebar em desktop.
- Botão flutuante `+` para gasto rápido acessível nas principais telas.
- Favoritos: “Café”, “Almoço”, “Uber” etc. com categoria e fonte pré-preenchidas.
- Repetir último lançamento; data de hoje por padrão; confirmar em no máximo 3 interações quando favorito completo.
- Campo monetário pt-BR com máscara e validação robusta; enviar centavos inteiros à API.
- Estados de carregamento, vazio, erro, sucesso, confirmação de operações destrutivas.
- Teclado apropriado em celular, labels acessíveis, contraste AA e navegação por teclado.
- Nunca apresentar “R$ X gastos” somando consumo com pagamento de fatura.
- Lembrete diário opt-in e avisos de vencimento no centro de notificações; Web Push somente com permissão explícita e suporte do dispositivo; fallback in-app.

### 8.3 PWA

- Manifest (`name`, `short_name`, `start_url`, ícones, `display: standalone`, `theme_color`).
- Service worker cacheia somente assets estáticos versionados e shell; **não cachear respostas autenticadas, extratos, saldos ou dados financeiros**.
- Sem promessa de funcionamento offline financeiro na V1; quando offline, exibir estado claro e impedir falsa confirmação de lançamento.
- Atualização de service worker com UX clara; testar instalação em Android e iOS compatíveis.

## 9. Autenticação, autorização e segurança

- Senha com Argon2id e parâmetros adequados; nunca logar senha/token.
- Sessão opaca aleatória em cookie `HttpOnly`, `Secure` em HTTPS, `SameSite=Lax` (ou Strict quando viável), `Path=/`, expiração e revogação; armazenar apenas hash do token no DB.
- Proteção CSRF para rotas mutáveis baseadas em cookie: validar Origin/Referer e usar token CSRF quando necessário; CORS restrito a origens configuradas.
- Limitar tentativas de login e convites; mensagens de erro sem enumeração desnecessária de usuários.
- Autorizar por membership ativa no backend em todos os handlers; `OWNER` administra convites e configuração da família; `MEMBER` opera finanças da família.
- HTTPS em produção; headers de segurança, CSP compatível, HSTS no domínio público, rate limiting básico.
- SQL parametrizado via sqlc/pgx; sanitização e encoding corretos na UI; nenhuma informação financeira em URL de query quando evitável.
- Logs com `request_id`, status, latência e erros sem dados sensíveis; trilha de auditoria separada.
- Backup PostgreSQL diário criptografado, retenção definida (ex.: 7 diários + 4 semanais), cópia fora da máquina principal e teste de restauração mensal. Não incluir backup não criptografado em Git.
- Segredos apenas em variáveis de ambiente/secret files, nunca em repositório. `.env.example` sem valores reais.

## 10. Worker de recorrência e lembretes (sem Redis)

- Um processo Go `worker` opcional, compartilhando módulos de domínio com a API, ou goroutine controlada por Fx Lifecycle em instalação de instância única.
- Usar PostgreSQL como fonte de verdade, `FOR UPDATE SKIP LOCKED` ou advisory locks para concorrência; jobs idempotentes.
- Rotina diária: gerar ocorrências recorrentes para horizonte configurável (padrão 12 meses) sem duplicar; criar lembretes de vencimento e lembrete diário conforme preferências.
- Não gerar 12 meses de transações de consumo confirmadas só para exibir previsão; gerar obrigações/ocorrências previstas e confirmar quando apropriado.
- Para push, usar padrão Web Push com VAPID se implementado; armazenamento de subscriptions e tratamento de expiração; notificações in-app são suficientes para aceitação inicial.
- Falha no worker não pode bloquear API; health/readiness separados; retries limitados e logs.

## 11. Infraestrutura e custo

### 11.1 Desenvolvimento local

`docker compose up -d` sobe PostgreSQL, API e web; volumes persistentes; portas apenas localhost quando possível. `make dev`, `make test`, `make lint`, `make migrate-up`, `make migrate-down`, `make seed`, `make e2e`, `make backup`, `make restore-test` devem funcionar e estar documentados.

### 11.2 Produção com custo mínimo

**Opção A (sem nuvem paga):** máquina própria com Docker Compose, PostgreSQL em volume persistente, reverse proxy Caddy, TLS, backup externo e acesso remoto via VPN ou túnel protegido. Requer máquina ligada, energia, conectividade e manutenção. Não expor banco.  
**Opção B (baixo custo):** VPS Linux pequena rodando o mesmo Compose, Caddy e PostgreSQL local, backup off-site. Sem serviços gerenciados obrigatórios. Dimensionar após medir RAM/CPU; Next.js pode consumir mais memória, preferir build otimizado e evitar SSR caro desnecessário.

Não pressupor que serviços free-tier permanecerão gratuitos. Medir custo mensal real, espaço de backups e uso de memória antes de escolher hospedagem. Registrar variáveis de deploy e runbook de recuperação.

## 12. Estrutura de repositório

```text
casvuno/
├── README.md
├── Makefile
├── .env.example
├── .github/workflows/ci.yml
├── apps/
│   ├── api/
│   │   ├── cmd/api/main.go
│   │   ├── cmd/worker/main.go
│   │   ├── internal/
│   │   │   ├── bootstrap/          # fx.New e fx.Module
│   │   │   ├── platform/           # config, db, auth, http, logs, clock, txmanager
│   │   │   ├── identity/
│   │   │   ├── households/
│   │   │   ├── categories/
│   │   │   ├── accounts/
│   │   │   ├── cards/
│   │   │   ├── transactions/
│   │   │   ├── obligations/
│   │   │   ├── recurrences/
│   │   │   ├── reporting/
│   │   │   └── notifications/
│   │   ├── db/migrations/
│   │   ├── db/queries/            # sqlc SQL
│   │   ├── sqlc.yaml
│   │   ├── go.mod
│   │   └── Dockerfile
│   └── web/
│       ├── app/
│       ├── components/
│       ├── features/
│       ├── lib/api/              # cliente gerado por OpenAPI
│       ├── public/manifest.webmanifest
│       ├── package.json
│       └── Dockerfile
├── docs/
│   ├── openapi.yaml
│   ├── adr/
│   ├── domain-model.md
│   ├── deployment.md
│   └── test-cases.md
└── infra/
    ├── compose.yaml
    └── Caddyfile
```

Dentro de um módulo Go complexo, preferir `domain/*.go`, `application/*.go`, `infra/postgres/*.go`, `transport/http/*.go`. Módulos pequenos podem ter menos pastas. Pacotes `internal` evitam API pública desnecessária. Não criar pacote `utils` genérico; compartilhar somente código realmente transversal e estável.

### 12.1 Exemplo de composição Fx (orientativo)

```go
func NewApp() *fx.App {
    return fx.New(
        platform.Module,
        identity.Module,
        households.Module,
        accounts.Module,
        cards.Module,
        transactions.Module,
        obligations.Module,
        recurrences.Module,
        reporting.Module,
        notifications.Module,
        httpserver.Module,
    )
}
```

Usar `fx.In`/`fx.Out` quando simplificarem construtores; lifecycle do HTTP com shutdown gracioso; pool pgx aberto e fechado no ciclo de vida; dependências validadas no startup.

## 13. Casos de teste obrigatórios

| ID | Cenário | Resultado esperado |
|---|---|---|
| T01 | Café R$ 12,50 no Pix | 1 despesa, 1 movimento de saída, saldo -R$ 12,50 |
| T02 | Almoço pessoal R$ 80 | Consumo familiar +R$ 80, sem divisão entre membros |
| T03 | Mercado familiar R$ 300 | Consumo familiar +R$ 300, independentemente de titular da conta |
| T04 | Transferir R$ 500 entre contas | Saldos individuais mudam; consolidado líquido = 0 |
| T05 | Compra R$ 1.200 em 6x | 1 consumo R$ 1.200; 6 parcelas R$ 200 em faturas |
| T06 | Compra R$ 100,01 em 3x | 33,34 + 33,34 + 33,33 |
| T07 | Pagar fatura R$ 200 | Caixa -R$ 200; consumo não aumenta |
| T08 | Pagar R$ 50 de fatura R$ 200 | Saldo devido R$ 150; estado parcial |
| T09 | Dois POST idênticos com mesma chave | 1 operação financeira; replay do mesmo resultado |
| T10 | Mesma chave, payload diferente | 409, sem nova operação |
| T11 | Gerar recorrência duas vezes | 1 ocorrência por regra/data |
| T12 | Editar recorrência futura | Histórico pago preservado |
| T13 | Compra em 28/02, ano bissexto | Fatura correta conforme ciclo |
| T14 | Compra no dia de fechamento | Próximo ciclo, conforme RN-011 |
| T15 | UUID de conta de outra família | Acesso negado, nenhuma alteração |
| T16 | Membro desativado | Sem acesso futuro; histórico preservado |
| T17 | Duas tentativas simultâneas de pagar saldo | Sem pagamento acima do saldo |
| T18 | Dashboard com compras + fatura paga | Consumo e caixa exibidos separadamente |
| T19 | PWA sem conexão | Mensagem offline, sem sucesso falso |
| T20 | CSV com descrição iniciando `=` | Campo neutralizado contra execução em planilha |
| T21 | Conta conjunta e individual | Ambas incluídas uma vez no consolidado |
| T22 | Receita prevista depois recebida | Previsão substituída por realizado, sem duplicação |

## 14. Definição de pronto (Definition of Done)

Uma história só está pronta quando: (a) regra implementada e coberta por testes, (b) migration reversível quando possível, (c) validação e autorização corretas, (d) OpenAPI atualizado, (e) frontend funcional e acessível, (f) erros/estados vazios tratados, (g) logs e auditoria apropriados, (h) `make test`, `make lint` e CI passam, (i) documentação de execução atualizada. Não marcar como pronto endpoint sem persistência ou tela com dados mockados quando deveria usar API.

---

**Fim da especificação V1.** Alterações futuras são registradas em ADRs (`docs/adr/`) e refletidas nos testes de aceitação. O plano de implementação e as instruções para agentes ficam em `docs/IMPLEMENTATION_PLAN.md` e `AGENTS.md`.
