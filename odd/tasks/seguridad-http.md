# Seguridad HTTP — loopback + token opcional + rate-limit

Feature branch: `feat/seguridad-http`.

## Specs

- S1: "¿Qué modelo de protección para ask/agent?" → "Loopback + token opcional"
- S2: "Bind local + token solo si se configura, con campo en UI"
- S3: rate-limit siempre activo en `POST /api/ask` y `POST /api/agent` (puros stdlib, sin dependencias)
- S4: `GET /api/health`, `GET /api/stats` y la UI estática siempre abiertos
- S5: en Docker el bind loopback NO aplica (el port-mapping llega por eth0): compose fija `HTTP_ADDR=:8080` explícito y publica en loopback del host

## Tasks

- T1 (S1,S2,S5, route: inline, commit: feat(config): API_TOKEN, HTTP_ADDR loopback default)
- T2 (S1,S3,S4, route: inline, commit: feat(http): auth + rate-limit middleware en ask/agent)
- T3 (S2, route: inline, commit: feat(web): campo token con localStorage)
- T4 (S1-S5, route: inline, commit: docs: seguridad HTTP en README)

## Log

- L1 original: "¿Qué frente atacamos?" → "Seguridad HTTP"
- L2: "¿Qué modelo de protección para ask/agent?" → "Loopback + token opcional"
