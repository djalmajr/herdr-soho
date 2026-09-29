# Brief — A2c: correções da revisão da A2b (`queued`)

Role: implementer · Agent: build-3 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

A revisão da A2b reprovou com 2 P2 provados com herdr falso, mais 6 lacunas de teste:

- uma falha momentânea do `herdr agent get` apaga o `.queued`, e o prompt que continua na caixa
  vira `settled-no-report` sem sinal;
- num painel estreito, o caminho do prompt quebra em duas linhas na tela `visible`, e `wait`/`status`
  não reconhecem o prompt parado na caixa.

Fechar os 8 achados. O código é JS (`skills/herdr-soho/scripts/`).

Worktree: `/work/herdr-soho/.worktrees/build-3`, branch
`fix/dispatch-queued` (commit `78c3665`, a A2b).

Leia antes a revisão inteira; cada achado tem o arquivo, a linha, a prova e, para F1 e F2, a correção
que o revisor já rodou numa cópia:
`/work/herdr-soho/.herdr-soho/w14/reports/review-20260928T225943.md`.

## Decisions already made

1. **F1**: no ramo `unavailable` de `wait.mjs:272-275`, só o `return`: o `.queued` e o
   `.enter-retry` ficam. `gone` e `blocked` continuam limpando. O teste
   `wait: normal blocked, gone and unavailable probes clear queued markers` passa a exigir que o
   marcador de `unavailable` permaneça, e um teste novo cobre o cenário `n-unavailable` inteiro
   (falha transitória, herdr de volta com o prompt na caixa → `not-received`/Enter, não
   `settled-no-report`).
2. **F2**: nos três pontos do teste do caminho (`wait.mjs:334-336`, `wait.mjs:557-558`,
   `commands/status.mjs:56-57`), a leitura é `recent-unwrapped` com 40 linhas, como o `dispatch` já
   faz. A detecção de cota do `status` continua na tela `visible`. Teste novo: `visible` traz o
   caminho partido e `recent-unwrapped` o caminho inteiro (o fake precisa respeitar `--source`).
3. **F3–F8**: um teste que mate cada mutante sobrevivente da tabela da revisão (I, N2, N3, N6, N12,
   N15). N14 é equivalente: não precisa de teste.
4. Nada muda no formato do marcador, nos códigos de saída nem no texto dos avisos.

## Expected result

1. `node --test scripts/test/wait.test.mjs scripts/test/status.test.mjs scripts/test/dispatch-arrival.test.mjs scripts/test/dispatch.test.mjs scripts/test/stats.test.mjs`
   a partir de `skills/herdr-soho` no worktree → 0 fail em Node, e o mesmo com `bun test` nesses
   arquivos (cole os resumos).
2. Os cenários `n-unavailable` e `n-wrap` da revisão reexecutados contra o código novo, com a saída
   colada (o harness do revisor ficava em `/tmp/hs-go/review/a2b/`; se não existir mais, monte um
   equivalente com herdr falso).
3. A tabela de mutantes da revisão, linha por linha (A–N15), reexecutada contra o código novo, com o
   teste que falha e o código de saída de cada um. Um mutante que você não conseguiu rodar é
   `[partial]` com o motivo, não "morto".

## Owned files

- `skills/herdr-soho/scripts/lib/wait.mjs`, `lib/arrival.mjs`, `lib/commands/status.mjs` e os testes
  `scripts/test/{wait,status,dispatch-arrival}.test.mjs`.

## Forbidden

- Qualquer outro arquivo, `internal/`, `cmd/`, `docs/`, `SKILL.md`.
- Herdr real: todo teste usa o herdr falso, `HERDR_SOCKET_PATH` para um caminho inexistente e ids
  impossíveis como `w0test:p0a`. Rede. Mutações só em cópia fora do repositório, com
  `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, testes novos, divergências e perguntas
abertas.
