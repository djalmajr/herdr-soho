# Brief — Revisão R6: plugin multiplataforma (Linux, macOS, Windows)

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está na branch `feat/plugin-cross-platform`. Revise
`git diff 6de46d3 HEAD` (três commits sobre a branch da renomeação).

## Goal

Achados ancorados em `arquivo:linha`, com execução, antes do push.

## Contrato decidido (verifique que o código cumpre)

- Manifesto com `platforms = ["linux", "macos", "windows"]`.
- `getPane`: no Windows, `HERDR_BIN_PATH` terminado em `.cmd`/`.bat` roda
  por `cmd.exe` com argumentos escapados pelo `cmdInvocation` da skill (a
  mesma regra do CLI, sem cópia); `herdr.exe` e binários POSIX como antes.
- Testes: os `herdr` falsos são JS atrás de um lançador da plataforma (sh em
  POSIX, `.cmd` no Windows); no Windows o `baseEnv` do teste carrega
  `SystemRoot`, `ComSpec`, `PATHEXT`, `TEMP`, `TMP` e System32 no `PATH`.

Evidência já coletada (não precisa repetir fora do macOS): Windows (Node 24,
Bun 1.4) 38/38 nos dois runtimes e ações `doctor`/`roster` executadas pelo
Herdr do Windows; Linux (linux, Node 22, Bun 1.4.2) 38/38 e ações pelo
Herdr; WSL (Node 22) 38/38.

## O que verificar

1. Injeção: um `pane id` hostil (aspas, `&`, `%`, `^`, espaços) chegando
   ao `cmd.exe` como mais de um argumento ou executando algo. Analise o
   `cmdInvocation` usado e o teste `hostile pane id`.
2. Regressão no macOS/Linux do `getPane` (timeout, erro, JSON malformado).
3. O import relativo `../skills/herdr-soho/scripts/lib/platform.mjs` no
   plugin ligado a partir de um checkout (o layout que o README descreve).
4. README descreve algo que o código não faz.

## Checks you may run

Leitura, `git diff/log/show`; `node --test plugin/test/` e
`bun test plugin/test/`; sondas em `/tmp`. Antes de afirmar que algo falha,
rode e cite a saída.

## Forbidden

- Editar qualquer arquivo. Revisor é somente leitura.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
