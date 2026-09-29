# Brief — Revisão R-A1: chegada confiável do dispatch

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está em `12b912b` (branch `fix/dispatch-arrival`, sobre `main`
`a138983`). Confirme com `git log --oneline -1` e revise
`git diff HEAD~1 HEAD`. Brief (o contrato):
`/work/herdr-soho/.herdr-soho/w14/plan-briefs/a1-dispatch-arrival.md`
e as respostas às perguntas em
`/work/herdr-soho/.herdr-soho/w14/plan-briefs/a1-agy-continue.md`.

## Por que existe

Hoje, três vezes, agentes agy recém-abertos perderam o primeiro prompt do
`dispatch`, e a conferência de chegada disse que ele tinha chegado
(nenhum `not-received`). Um Codex recém-aberto perdeu uma mensagem do
mesmo jeito.

## O que verificar com atenção

1. **Testes existentes**: `dispatch.test.mjs` mudou ~77 linhas. Algum teste
   foi enfraquecido (asserção removida, expectativa afrouxada, caso
   pulado) sem ser pela mudança decidida? Cite linha a linha.
2. A espera de assentamento: `interactive_ready`, duas telas iguais,
   `prompt_settle_seconds` (padrão 20, `0` desliga, inválido = 20), aviso
   ao estourar; roda antes de H0/preSeq; vale também para `--amend`.
3. Regra 1 com `state_change_seq` diferente de `preSeq`; regra 4 só com o
   caminho do `composed` fora das 3 últimas linhas; sem prova → reenvio
   único → `not-received`. Uma emenda a um worker **já** `working` (sem
   mudança de seq) ainda consegue ser dada como recebida quando o texto
   aparece na conversa?
4. Goldens: `parity-dispatch.json` e `parity-config.json` mudam só pelo
   que a mudança explica (diff decodificado).
5. Os testes novos (casos a–e) pegam as mutações que declaram.
6. Custo: quanto tempo a espera soma a cada dispatch com tela estável?

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test --timeout 60000` em
`dispatch-arrival.test.mjs`, `dispatch.test.mjs`, `parity-dispatch.test.mjs`,
`task-report.test.mjs`, `for-released.test.mjs`, `parity-config.test.mjs`;
mutações em cópias em `/tmp`.

## Forbidden

- Editar qualquer arquivo do checkout. Revisor é somente leitura.
- Nenhuma chamada ao Herdr real que escreva (nada de `dispatch` real,
  `agent prompt`, `send-keys`, painéis): sondas só com `herdr` falso no
  `PATH` e `HERDR_SOCKET_PATH` apontando para um caminho inexistente.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
