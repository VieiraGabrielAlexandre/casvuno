# ADR 0005 — Autenticação, sessão, CSRF e mesma origem

- Status: Aceito (2026-11-10)
- Data: 2026-11-10

## Decisão
- **Senha:** Argon2id (`golang.org/x/crypto/argon2`), formato PHC com parâmetros embutidos (m=64 MiB, t=2, p=1, salt de 16 B) para permitir rehash no login. Mínimo de 10 caracteres e máximo de 128 (limite contra DoS). Quando o e-mail não existe, faz-se uma verificação falsa com hash fixo para igualar o tempo de resposta.
- **Sessão opaca:** 32 bytes de `crypto/rand` em base64url no cookie. O banco guarda só `SHA-256(token)`. Cookie `__Host-casvuno_session` em produção (`Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`, sem `Domain`); em dev, `casvuno_session` sem `Secure`. Expiração **deslizante**: a sessão expira após 30 dias sem uso e, no máximo, 90 dias após o login (`absolute_expires_at`). Ao ser usada, a sessão é renovada no máximo a cada 5 min (`last_seen_at`, novo `expires_at` e cookie reemitido com o mesmo token). O login emite um token novo; o logout revoga no servidor. *(Revisado na F1: antes, expiração absoluta de 30 dias.)* Sem JWT e sem token em `localStorage`.
- **Mesma origem:** o Caddy serve o web estático em `/` e faz proxy de `/api/*` e `/health` para a API. Em dev, os rewrites do Next fazem o mesmo papel. Com isso não há CORS e a API não habilita origens cruzadas.
- **CSRF:** em métodos não seguros sob `/api/v1`, exige `Origin == APP_ORIGIN` ou, sem `Origin`, `Sec-Fetch-Site: same-origin`; caso contrário 403 `CROSS_ORIGIN_REQUEST`. Corpos só são aceitos como `application/json` (decode estrito). Junto com `SameSite=Lax`, isso dispensa token CSRF.
- **`APP_ORIGIN`** (obrigatoriamente https em produção) define a origem permitida, a base dos links de convite e o atributo `Secure` do cookie.
- **Rate limit em memória** (`golang.org/x/time/rate`, instância única): login 5/min por IP e por e-mail; criação e aceite de convite 10/h por usuário. Mensagens genéricas ("e-mail ou senha inválidos").
- **Cadastro:** `REGISTRATION_ENABLED`. Quando desligado, `/auth/register` só aceita requisição com token de convite válido.
- **Convites:** token aleatório; o banco guarda só o hash; expira em 7 dias; uso único; revogável. O link é compartilhado manualmente pelo OWNER. O e-mail no convite é informativo.
- **Recuperação de senha:** `casvuno admin reset-password --email …` (operador). Fluxo por e-mail fica fora da V1.
- **`GET /auth/me`** retorna só o usuário; as famílias vêm de `GET /households` (I-39).
- **IP para rate limit:** endereço TCP do cliente. Atrás do Caddy (F8.2) será preciso confiar no cabeçalho de encaminhamento do proxy.
- **Autorização:** ver ADR 0006.
