# Brief — Revisão R13: limpeza dos processos do fake de timeout (issue #17)

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está em `fix/test-portability` `5aa5885`. Confirme com
`git log --oneline -1` e revise só `git diff 435e2b4 HEAD` (dois commits,
só `skills/herdr-soho/scripts/test/setup-probe.test.mjs`).

## Goal

Confirmar que o seu P2 da R12 está resolvido sem regressão.

## Contrato decidido (verifique que o código cumpre)

- O fake `hang` grava o pid do neto e o próprio em `PROBE_PID_DIR/pids`
  (diretório separado dos logs de argumentos, que os testes limpam).
- `test.after` encerra esses pids e depois apaga o fixture; no Windows
  repete o `rmSync` em `EPERM`/`EBUSY`/`ENOTEMPTY` até 30 s (o `maxRetries`
  do `rmSync` não cobria o caso no Node 24).
- O neto continua vivo 10 s durante os asserts (tetos de 5 s e 8 s);
  o caso especial de 4 s no Windows saiu.

## O que verificar

1. Depois de `node --test` e de `bun test` do arquivo, nenhum processo do
   fake sobra (conte PIDs; não liste linhas de comando).
2. Os tetos continuam provando "volta no limite, não na morte do neto".
3. O `test.after` não mascara erro real no POSIX (lá ele não repete).

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test` só em
`setup-probe.test.mjs`; `pgrep -f … | wc -l` para contar processos.
Antes de afirmar que algo falha, rode e cite a saída.

## Forbidden

- Editar qualquer arquivo do checkout. Revisor é somente leitura.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
