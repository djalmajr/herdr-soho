# Brief — A1 (correção da revisão R-A1): regra 4 antes do Enter, aviso e testes

Role: implementer · Agent: build · Report language: pt-BR

Worktree `.worktrees/a1`, branch `fix/dispatch-arrival`, commit `12b912b`.
Caminhos relativos à raiz dela. Contrato original:
`/work/herdr-soho/.herdr-soho/w14/plan-briefs/a1-dispatch-arrival.md`
(com as respostas em `a1-agy-continue.md`, mesma pasta). Revisão que
motiva esta fatia (leia os quatro achados, com as sondas):
`/tmp/herdr-soho/w14/reports/soho-grok-r7-20260928T150930.md`.

## Goal

Um prompt que já chegou à conversa é dado como recebido mesmo quando o
texto ainda aparece nas últimas linhas da tela; o aviso de reenvio não
afirma uma causa que não verificou; os testes (a) e (d) pegam as mutações
que declaram.

## Decisions already made

1. **Achado 1 (P1).** No fim da primeira janela, a ordem passa a ser:
   se a tela mudou (checksum diferente de H0) **e**
   `composedPathSeenOutsideInput` é verdadeiro → recebido (regra 4), sem
   Enter e sem reenvio; senão, se o texto está parado na caixa → Enter
   (regra 2); senão → reenvio único. Vale para dispatch e `--amend`. Teste
   novo reproduzindo as sondas A e D da revisão (agente `working`, mesmo
   seq, texto na tela visível e na recente com 4 linhas depois) → exit 0,
   nenhum Enter, nenhum reenvio.
2. **Achado 2.** O aviso do reenvio passa a
   `prompt to '<agent>' did not arrive; sending it once more`. Atualize os
   testes/goldens que citam o texto antigo.
3. **Achado 3.** No hook do caso (a), use o `fs` recebido, sem `require`,
   para que a tela realmente seja redesenhada sem o caminho. Prove que a
   mutação "primeira janela aceita qualquer mudança de tela" agora faz o
   (a) falhar.
4. **Achado 4.** O caso (d1) usa um falso que muda a tela a cada leitura
   até a segunda leitura igual, e falha se o `agent prompt` aparecer
   enquanto as duas últimas leituras visíveis diferem. Prove que a mutação
   `if (ready && currScreen === prevScreen)` → `if (ready)` faz o (d)
   falhar.

## Acceptance criteria

1. Os testes acima, cada um com `// Mutation captured: …` executado (cole
   a saída vermelha da mutação e a verde do código).
2. `node --test` e `bun test --timeout 60000` de
   `dispatch-arrival.test.mjs`, `dispatch.test.mjs`,
   `parity-dispatch.test.mjs`, `task-report.test.mjs`,
   `for-released.test.mjs`, `parity-config.test.mjs` → 0 fail (cole).
   Goldens que mudarem: diff decodificado.

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `skills/herdr-soho/scripts/lib/dispatch.mjs` (só o fim da primeira
  janela e o texto do aviso), `skills/herdr-soho/scripts/lib/arrival.mjs`
  se precisar, `skills/herdr-soho/scripts/test/dispatch-arrival.test.mjs`,
  testes e goldens que citam o aviso antigo

## Forbidden

- Todo o resto. Nenhuma chamada ao Herdr real que escreva: sondas só com
  `herdr` falso no `PATH` e `HERDR_SOCKET_PATH` apontando para um caminho
  inexistente.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera. Não rode a
suíte completa.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e as mutações
executadas.
