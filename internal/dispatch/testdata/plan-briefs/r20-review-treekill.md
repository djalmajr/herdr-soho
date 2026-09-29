# Brief — Revisão R20: árvore de processos no timeout de `.cmd` no Windows (#20)

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está em `4981666` (branch `fix/win-cmd-timeout-tree`). Confirme
com `git log --oneline -1` e revise só `git diff HEAD~1 HEAD`. Leia a
issue: https://github.com/djalmajr/herdr-soho/issues/20.

## Goal

Achados ancorados em `arquivo:linha`, com execução, antes do push.

## Contrato decidido (verifique que o código cumpre)

- Só `win32` + alvo `.cmd`/`.bat` + `timeoutMs > 0` passa pelo helper
  `lib/treekill-run.mjs`; o resto de `runCli` fica como estava (o diff de
  `platform.mjs` deveria ser só aditivo).
- O helper: spec JSON 0600 no `TMPDIR` (argumentos sem reinterpretação),
  `spawn(..., { stdio: 'inherit', windowsVerbatimArguments })`, timer do
  `timeoutMs`, e no disparo `taskkill /PID <pid> /T /F` enquanto o
  `cmd.exe` vive; espera o `exit` e grava `{ status, signal, timedOut,
  error }` no arquivo de resultado.
- `runCli` devolve o formato de hoje: timeout → `status null`,
  `signal 'SIGTERM'`, `timedOut true`, `error 'ETIMEDOUT'`; comando que
  não começou → o código do spawn (ex.: `ENOENT`); sem arquivo de
  resultado → erro do wrapper, nunca status inventado.
- Os três modos (`mergeOutput`, `outputFiles`, padrão com `input`) mantêm
  stdout/stderr como antes. Teto de segurança `timeoutMs + 15000`.
- `HERDR_SOHO_TREEKILL_TEST_KILLER` só substitui o `taskkill` em teste.

## O que verificar com atenção

1. Temporários: spec e resultado sempre apagados (inclusive em erro e no
   teto); nenhum vazamento de arquivo nem de processo.
2. A variável de teste do matador: dá para um ambiente comum ativá-la sem
   querer, ou alguém usá-la para rodar outro programa? Proponha a forma
   mínima de restringi-la, se precisar.
3. `taskkill` resolvido por `SystemRoot`: e se `SystemRoot` faltar no
   `env` passado?
4. Bun como runtime (`process.execPath` é o `bun`): o helper roda igual?
5. Os testes pegam as mutações que declaram.

## Checks you may run

Leitura, `git diff/log/show`; `node --test` e `bun test --timeout 60000`
em `treekill.test.mjs`, `platform.test.mjs`, `models.test.mjs`,
`herdr.test.mjs`, `setup-probe.test.mjs`; sondas em `/tmp`. Não rode a
suíte inteira. Antes de afirmar que algo falha, rode e cite a saída.

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
