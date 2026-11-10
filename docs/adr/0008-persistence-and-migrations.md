# ADR 0008 — Persistência, transações e migrations

- Status: Proposto
- Data: 2026-11-10

## Decisão
- **PostgreSQL 18** (imagem `postgres:18.6-alpine`, fixada em `infra/compose.yaml` e nos testes de integração). **Go 1.27** (`go 1.27.0`, `toolchain go1.27.3`). Extensão `citext`. UUID v7 gerado na aplicação (`github.com/google/uuid`).
- **pgx/v5 + sqlc.** SQL fica em `apps/api/db/queries/<módulo>.sql`. O código gerado vai para `internal/<módulo>/postgres` (ou o pacote do módulo, se ele for plano) e é commitado. O CI roda `sqlc diff`/`generate` e falha se houver divergência. Não há ORM nem `Repository[T]` genérico.
- **TxManager** (`platform/postgres`): `WithinTx(ctx, func(ctx) error) error` guarda o `pgx.Tx` no contexto, e os adaptadores obtêm o `DBTX` com `postgres.Conn(ctx, pool)`. Transações aninhadas reutilizam a externa. Os casos de uso dependem de uma interface `TxRunner` de um método.
- **Migrations (golang-migrate)** embutidas no binário (`embed.FS`), executadas por `casvuno migrate up|down N` (driver `pgx5` do golang-migrate, sem `lib/pq`). No compose, um serviço one-shot `migrate` roda antes da `api`. A API **não** migra sozinha ao subir.
  - Nomes `NNNNNN_descricao.up.sql`/`.down.sql`, sequenciais. Em geral uma migration por história.
  - Uma migration já aplicada em `main` **nunca é editada**; correções viram nova migration.
  - `down` é obrigatório. Quando um `down` destruiria dados financeiros reais, ele tem um comentário dizendo isso, e o runbook proíbe usá-lo em produção.
  - Toda constraint de negócio possível vai para o banco: `CHECK` de valores, enums como `TEXT + CHECK`, `UNIQUE`, FKs compostas, `ON DELETE RESTRICT`.
  - Em produção, mudanças incompatíveis seguem expand → migrate → contract.
  - O CI roda `up` → `down` total → `up` num banco vazio.
- **Índices:** os do README §6.4, mais índices parciais de pendências (`obligations … WHERE status <> 'CANCELLED'`).
