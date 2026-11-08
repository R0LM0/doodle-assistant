# Fase 8 — Búsqueda híbrida (vector + FTS con RRF)

Feature branch sugerida: `feat/fase-8-hibrida`. Plan base: `.zcode/plans/plan-session_520ad40ffdc4abfb96e379f560e1a7f3.md`.

## Specs

- S1: "hay que continuar con los que nos hace falta"
- S2: "hay que hacer las pruebas en local"
- S3: "Fase 8 híbrida" (elección del usuario ante "Pruebas locales en verde y pendientes mapeados. ¿Con qué seguimos?")
- S4: Plan fase 8 textual: "SEARCH_MODE (vector|hybrid|lexical, default vector) y RRF_K (default 60)", "Migración 0003_fts_hybrid.sql", "switch-source en el judge", "+8 docs, +12 preguntas", "tabla real en el README comparando vector vs lexical vs hybrid vs agentic"

## Tasks

- T1 (S1,S3,S4, route: inline, commit: feat(ports): SearchRequest with mode; Distance → Score — 2169041)
- T2 (S3,S4, route: inline, commit: feat(postgres): lexical + hybrid search with RRF — 221ec35)
- T3 (S3,S4, route: inline, commit: feat(config,eval): SEARCH_MODE y RRF_K, flag -mode en eval — c6319ec)
- T4 (S3,S4, route: inline, commit: feat(app): judge retry switches source — f7168a6)
- T5 (S3,S4, route: inline, commit: feat(docs): corpus fase 8)
- T6 (S1,S2,S3,S4, route: inline, commit: docs: fase 8 en README)

## Log

- L1 original: "hay que continuar con los que nos hace falta"
- L2: "hay que hacer las pruebas en local" → unitarias/vet/race/integración/healthcheck en verde (stats documents=8 chunks=33)
- L3: explorador reportó fase 8 planificada no implementada + 10 gaps; usuario eligió "Fase 8 híbrida"
- L4: plan leído en .zcode/plans/plan-session_520ad40ffdc4abfb96e379f560e1a7f3.md; puerto actual Search(ctx, embedding, limit) solo vectorial
- L5: T1 hecho en 2169041 (branch feat/fase-8-hibrida). Verificado: go test ./..., vet, integration postgres, web build.
- L6: T2 hecho en 221ec35. Verificado: unit, integration (3 tests search), healthcheck spanish FTS. Slice boundary: T1 276 + T2 191 = 2 commits reviewables, cada uno <400.
- L7: T3 hecho en c6319ec. Medido real: vector r1 0.867, lexical 0.133, hybrid 0.800. Slice: 3 commits/693 líneas acumuladas, cada commit <400.
- L8: T4 hecho en f7168a6. Verificado: 6 tests agentic incl. switch-source. Slice: 4 commits/775 acumuladas, cada commit <400.
