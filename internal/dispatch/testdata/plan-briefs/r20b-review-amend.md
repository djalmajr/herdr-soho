# Brief — Revisão R20b: emenda da #20 (pipes próprios, matador restrito)

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está em `25364f5` (branch `fix/win-cmd-timeout-tree`). Confirme
com `git log --oneline -1` e revise `git diff 4981666 HEAD` (um commit
sobre o que a revisão R20 já viu).

## Goal

Confirmar que os dois achados P2 da R20 estão resolvidos sem regressão.

## Contrato decidido

- R20-1: no modo padrão (pipes), o helper dá ao comando pipes próprios
  (`stdio: ['inherit', 'pipe', 'pipe']`), copia a saída e sai no `exit`
  do comando, drenando o que já chegou; um neto que segura os pipes não
  prende mais a chamada até o teto. `mergeOutput`/`outputFiles` seguem
  com `inherit` sobre fd de arquivo.
- R20-2: `HERDR_SOHO_TREEKILL_TEST_KILLER` só vale como caminho absoluto
  cujo `realpath` fica estritamente dentro de `realpath(os.tmpdir())`.
- Testes que simulam o win32 com fakes POSIX pulam no Windows.

## O que verificar

1. Saída grande (ex.: 5 MB em stdout) no modo padrão chega inteira e na
   ordem; stdout e stderr intercalados não se perdem; o `input` ainda chega
   ao comando.
2. Algum byte escrito pelo comando logo antes de sair pode se perder por o
   helper sair no `exit` e não no `close`? Mostre com uma sonda.
3. O teste novo do neto e o do matador pegam as mutações que declaram.
4. Nada mudou fora do caminho do helper (POSIX, `.exe`, sem timeout).

## Checks you may run

Leitura, `git diff/log/show`; `node --test` e `bun test --timeout 60000`
em `treekill.test.mjs`, `platform.test.mjs`, `models.test.mjs`,
`herdr.test.mjs`, `setup-probe.test.mjs`; sondas em `/tmp`.

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
