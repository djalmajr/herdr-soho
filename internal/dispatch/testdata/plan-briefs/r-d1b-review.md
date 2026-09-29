# Brief — Revisão R-D1b: regra do aviso na ordem real do Codex

Role: reviewer · Agent: review · Report language: pt-BR

## Goal

Dizer se o commit `6d16c0a` fecha os sete achados da revisão anterior do
diagnóstico de Codex sem abrir outro, para `fix/codex-env-diagnostic` ir à
`main`.

Seu cwd está em `6d16c0a`. Confirme com `git log --oneline -1` e revise
`git diff HEAD~1 HEAD` (a fatia) e `git diff HEAD~2 HEAD` (o D1 inteiro).
Revisão anterior, com as provas que você pode repetir:
`/tmp/herdr-soho/w14/reports/soho-grok-r9-20260928T155123.md`.
Brief da correção (o contrato):
`/work/herdr-soho/.herdr-soho/w14/plan-briefs/d1b-fix-r9.md`.
Relatório do implementador:
`/tmp/herdr-soho/w14/reports/soho-agy-d1b-20260928T161602.md`.

## O que verificar com atenção

1. Repita as provas com `codex sandbox -- /usr/bin/printenv` e
   `CODEX_HOME` temporário para cada configuração dos achados 1, 2, 5 e 6
   e compare com o que o `doctor` diz: aviso se e só se alguma de
   `HERDR_ENV`, `HERDR_PANE_ID`, `HERDR_WORKSPACE_ID` não passa. Inclua a
   configuração real atual do usuário (copie `~/.codex/config.toml` para
   o `CODEX_HOME` temporário, sem imprimir o conteúdo): ela agora é
   `inherit = "all"` com `include_only` e deve ficar **sem** aviso.
2. Valores de `set` e outras seções nunca aparecem na saída nem ficam no
   objeto analisado.
3. Achado 3: só pids ancestrais escolhem o painel.
4. Achado 4: `herdr.test.mjs` fica verde com um `ps` falso que responde
   `codex` no `PATH`, e nenhum outro teste que confere a mensagem de fora
   do Herdr depende do `ps`/`herdr` do host (procure em `layout`, `init`,
   `title`, goldens).
5. Testes enfraquecidos ou mutações declaradas que não pegam (rode três).

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test --timeout 60000` em
`codex-env.test.mjs`, `doctor.test.mjs`, `parity-doctor.test.mjs`,
`herdr.test.mjs`, `layout.test.mjs`, `init.test.mjs`, `title.test.mjs`;
mutações em cópias em `/tmp`; `codex sandbox` com `CODEX_HOME`
temporário; `herdr api snapshot` e `herdr pane process-info` só de leitura
(sem imprimir `argv`/`cmdline`).

## Forbidden

- Editar qualquer arquivo do checkout ou `~/.codex/config.toml`. Revisor é
  somente leitura.
- Nenhuma chamada ao Herdr real que escreva e nenhum `herdr pane current`.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
