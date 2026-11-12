# ADR 0006 — Isolamento por família e autorização

- Status: Aceito em 2026-11-12 (aprovado pelo usuário)
- Data: 2026-11-10

## Decisão
Defesa em três camadas, todas obrigatórias:

1. **HTTP:** todo handler sob `/api/v1/households/{householdId}/…` passa pelo middleware `households.RequireMember`. Ele valida o UUID, carrega a membership `ACTIVE` do usuário da sessão e coloca `Membership{HouseholdID, MemberID, UserID, Role}` no contexto. Quem não é membro (ou é membro inativo) recebe **404** (não revela a existência da família). Um membro sem papel suficiente recebe **403**. `RequireOwner` protege convites, edição da família e desativação de membros.
2. **Aplicação/SQL:** todo caso de uso recebe `householdID` da membership, **nunca do corpo da requisição**. Toda query sqlc de recurso de família tem `household_id = $1` no `WHERE`/`INSERT`. IDs referenciados no corpo (conta, cartão, categoria, membro) são carregados filtrando por `household_id`. Se não existirem: `ErrNotFound`, que vira 422 com `field` quando o ID veio do corpo e 404 quando veio da URL.
3. **Banco:** FKs compostas `(household_id, x_id)` → `UNIQUE(household_id, id)`. Mesmo um bug na aplicação não consegue gravar uma referência cruzada entre famílias.

**Visibilidade:** todos os membros ativos veem tudo da família. `scope=PERSONAL` e `person_member_id` são apenas filtros (RN-002). Não existe ACL por lançamento.

**Família ativa:** não existe estado no servidor. O contexto é o `householdId` do path. `GET /households` lista as famílias ativas do usuário; o web lembra a última escolhida pela URL.

**Membros:** `ACTIVE | INACTIVE`. Desativar preserva o histórico (RN-023). Invariante: sempre ≥ 1 OWNER ativo.

## Verificação
Suíte de isolamento em `apps/api/internal/app/households_integration_test.go` (`TestHouseholdIsolation`): para **cada rota** sob `/households/{householdId}`, um usuário de outra família recebe 404 `HOUSEHOLD_NOT_FOUND`, IDs de recursos de outra família endereçados pela própria família dão 404, e nada é gravado (contagem de linhas antes e depois). `TestEveryHouseholdRouteIsCoveredByIsolationTests` percorre o router (`chi.Walk`) e falha se uma rota nova não estiver na tabela `householdRoutes`.

**Reativação:** um ex-membro que aceita novo convite volta como `MEMBER`, nunca com o papel anterior.
