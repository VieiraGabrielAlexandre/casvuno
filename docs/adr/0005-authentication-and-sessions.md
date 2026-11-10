# ADR 0005 — Autenticação, sessão, CSRF e mesma origem

- Status: Proposto (itens I-12 e I-13 requerem aprovação)
- Data: 2026-11-10

## Decisão
- **Senha:** Argon2id (`golang.org/x/crypto/argon2`), formato PHC com parâmetros embutidos (m=64 MiB, t=2, p=1, salt de 16 B) para permitir rehash no login. Mínimo de 10 caracteres e máximo de 128 (limite contra DoS). Quando o e-mail não existe, faz-se uma verificação falsa com hash fixo para igualar o tempo de resposta.
- **Sessão opaca:** 32 bytes de `crypto/rand` em base64url no cookie. O banco guarda só `SHA-256(token)`. Cookie `__Host-casvuno_session` em produção (`Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`, sem `Domain`); em dev, `casvuno_session` sem `Secure`. Expiração absoluta de 30 dias. `last_seen_at` é atualizado no máximo a cada 5 min. O token é rotacionado no login, e o logout revoga. Sem JWT e sem token em `localStorage`.
- **Mesma origem:** o Caddy serve o web estático em `/` e faz proxy de `/api/*` e `/health/*` para a API. Em dev, os rewrites do Next fazem o mesmo papel. Com isso não há CORS e a API não habilita origens cruzadas.
- **CSRF:** em métodos não seguros, a requisição precisa ter `Origin` ou `Sec-Fetch-Site` compatível com `APP_ORIGIN`, além de `Content-Type: application/json`. Junto com `SameSite=Lax`, isso dispensa token CSRF. Uma requisição sem nenhum dos dois cabeçalhos é rejeitada.
- **Rate limit em memória** (`golang.org/x/time/rate`, instância única): login 5/min por IP e por e-mail; criação e aceite de convite 10/h por usuário. Mensagens genéricas ("e-mail ou senha inválidos").
- **Cadastro:** `REGISTRATION_ENABLED`. Quando desligado, `/auth/register` só aceita requisição com token de convite válido.
- **Convites:** token aleatório; o banco guarda só o hash; expira em 7 dias; uso único; revogável. O link é compartilhado manualmente pelo OWNER. O e-mail no convite é informativo.
- **Recuperação de senha:** `casvuno admin reset-password --email …` (operador). Fluxo por e-mail fica fora da V1.
- **Autorização:** ver ADR 0006.
