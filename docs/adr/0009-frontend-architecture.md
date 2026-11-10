# ADR 0009 — Frontend: Next.js exportado estaticamente

- Status: Proposto (item I-24 requer aprovação)
- Data: 2026-11-10

## Contexto
O README fixa Next.js + TypeScript, mas alerta sobre o consumo de memória do Next em VPS pequena (§11.2) e proíbe cachear dados financeiros no service worker (§8.3). Todo dado é privado e autenticado por cookie, então SSR não traz ganho de SEO nem de privacidade.

## Decisão
- **Next.js (App Router) com `output: 'export'`.** Os arquivos estáticos são servidos pelo Caddy. Não há processo Node em produção.
- Dados buscados **só no cliente** com TanStack Query. O guard de autenticação usa `GET /auth/me`, e a API é a autoridade de segurança.
- **Cliente tipado:** `openapi-typescript` gera `src/lib/api/schema.d.ts` a partir de `docs/openapi.yaml`, e o `openapi-fetch` faz as chamadas. O arquivo gerado é commitado e o CI verifica divergência.
- **Formulários:** React Hook Form + Zod escrito à mão, só para UX. Erros `VALIDATION_ERROR` da API são mapeados para os campos.
- **UI:** Tailwind + shadcn/ui (componentes copiados, não dependência), Recharts, pt-BR, mobile-first.
- Rotas de detalhe usam query string (`/cards/invoice?id=…`), porque a exportação estática não suporta segmentos dinâmicos sem `generateStaticParams`.
- **Estrutura:** `src/app` (rotas finas) → `src/features/<domínio>` (hooks de query, formulários, componentes de feature) → `src/components/ui` e `src/lib` (api, money, dates).
- **Mutações financeiras** geram `Idempotency-Key` por submissão. O botão fica desabilitado enquanto a requisição está pendente. Offline (`navigator.onLine === false` ou erro de rede): mensagem explícita e nunca toast de sucesso (T19).

## Notas de implementação (F0, Next.js 16.4)
- `next.config.ts` é uma função de fase: `output: 'export'` só fora do servidor de dev, e rewrites (`/api/*`, `/health` → `API_URL`) só no `next dev`. As duas configurações não podem coexistir (o Next dá erro). O `pnpm build` no CI garante a compatibilidade com o export.
- Fontes do sistema em vez de `next/font/google`: o build não faz chamada externa e nenhum dado do usuário vai para terceiros.
- shadcn/ui no preset `base-nova` (Base UI + pacote `cn`). Componentes são adicionados só quando usados.
- Em dev, o web roda no host (`make web`). Um container Node com bind mount gravaria arquivos como root no repositório.

## Alternativa rejeitada
`output: 'standalone'` com servidor Node: mais memória, cookies tratados no servidor web e superfície extra, sem benefício para um app privado. Reavaliar só se surgir uma necessidade real de SSR.
