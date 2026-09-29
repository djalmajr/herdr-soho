# Brief — I20: matar a árvore de processos no timeout de um `.cmd` no Windows

Role: implementer · Agent: build · Report language: pt-BR

Worktree própria, branch `fix/win-cmd-timeout-tree` (base: `main`
`a138983`). Caminhos relativos à raiz dela. Issue:
https://github.com/djalmajr/herdr-soho/issues/20 (leia).

## Goal

No Windows, quando `runCli` roda um `.cmd`/`.bat` com `timeoutMs` e o
tempo estoura, nenhum processo iniciado por esse comando sobrevive (hoje o
`spawnSync` mata só o `cmd.exe` e o processo que o batch iniciou, ex.:
`node …\codex.js` atrás de `codex.cmd`, continua vivo).

## Decisions already made

1. **Onde:** `skills/herdr-soho/scripts/lib/platform.mjs`, `runCli`. O
   caminho novo vale só quando `platform === 'win32'`, o executável
   resolvido termina em `.cmd`/`.bat` e `opts.timeoutMs` foi dado. Todo o
   resto (POSIX, `.exe`, sem timeout) fica byte a byte como hoje.
2. **Como:** um helper novo `skills/herdr-soho/scripts/lib/treekill-run.mjs`,
   executado com `spawnSync(process.execPath, [helper, specFile], …)`
   (Node ou Bun, o que estiver rodando o CLI). O `spawnSync` externo usa
   exatamente o `stdio`/`input`/`encoding`/`env`/`cwd` do modo em uso
   (`mergeOutput`, `outputFiles` ou o padrão com pipes e `input`), sem o
   `timeout` do comando — só um teto de segurança `timeoutMs + 15000`.
   - `specFile`: JSON num arquivo temporário (modo `0600`, no mesmo
     `TMPDIR` que `runCli` já usa, apagado no fim) com `command`, `args`,
     `windowsVerbatimArguments`, `timeoutMs` e `resultFile` — assim os
     argumentos não passam por nenhuma reinterpretação de linha de
     comando.
   - O helper faz `spawn(command, args, { stdio: 'inherit',
     windowsVerbatimArguments })` (herda o stdin/stdout/stderr que recebeu,
     portanto os fds/pipes do modo), arma um timer de `timeoutMs` e, se
     ele disparar enquanto o filho vive, roda
     `<SystemRoot>\System32\taskkill.exe /PID <pid> /T /F` (com
     `spawnSync` e timeout de 10 s) **enquanto o `cmd.exe` ainda está
     vivo**, de modo que `/T` alcança a árvore inteira. Depois espera o
     `exit` do filho e grava em `resultFile` `{ status, signal, timedOut }`.
3. **Resultado:** `runCli` lê `resultFile` e devolve o mesmo formato de
   hoje. Timeout → `status: null`, `signal: 'SIGTERM'`, `timedOut: true`,
   `error: 'ETIMEDOUT'` (o que o `spawnSync` devolve hoje; `herdr.mjs`
   testa `r.error === 'ETIMEDOUT'` e outros testam `r.timedOut`); saída
   normal → `status` do filho. `stdout`/`stderr` como no modo em uso. Se o
   helper falhar (sem `resultFile`), devolva o erro sem inventar status.
4. **Não-objetivo:** POSIX (grupos de processo) e qualquer outro lugar que
   rode processos fora de `runCli`.

## Expected result

`runCli` com o caminho novo, o helper, testes; tabela no relatório de
cada modo (`mergeOutput`, `outputFiles`, padrão com `input`) × (termina
antes do timeout, estoura o timeout) com o que é devolvido.

## Acceptance criteria

1. Teste que roda **só no Windows** (`t.skip` com motivo fora dele): um
   `.cmd` falso sobre um script Node que grava o próprio pid e o de um
   neto que ele inicia, e fica pendurado; `runCli(..., { timeoutMs: 1000 })`
   volta com `timedOut: true`, `error: 'ETIMEDOUT'`, em menos de 5 s, e
   logo depois os dois pids não existem mais (`process.kill(pid, 0)` lança
   `ESRCH`, com uma folga curta). Você não roda Windows: o orquestrador
   roda; escreva-o de modo que dê para rodar isolado.
2. Testes portáteis (rodam em macOS/Linux) do helper em si: executado
   direto com um comando Node que (a) termina antes do timeout com código
   3 e saída em stdout/stderr, (b) passa do timeout — com o `taskkill`
   substituído por um matador injetável só para teste (ex.: variável de
   ambiente `HERDR_SOHO_TREEKILL_TEST_KILLER` lida só pelo helper, ou
   parâmetro de uma função exportada), provando que o matador recebe o pid
   certo e que `resultFile` e o formato devolvido batem com o do
   `spawnSync` atual. Cada teste com `// Mutation captured: …` executado.
3. Os testes existentes de `runCli`, `herdr`, `setup-probe` passam no
   macOS com `node --test` e `bun test --timeout 60000` (cole).
4. Nenhuma mudança de comportamento fora do Windows: mostre que em POSIX
   `runCli` não chama o helper (teste ou leitura do diff).

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `skills/herdr-soho/scripts/lib/platform.mjs` (só `runCli` e o que ele
  precisar)
- `skills/herdr-soho/scripts/lib/treekill-run.mjs` (novo)
- `skills/herdr-soho/scripts/test/treekill.test.mjs` (novo)
- `skills/herdr-soho/scripts/test/setup-probe.test.mjs`: só se o teste do
  timeout precisar mudar por causa da correção; diga por quê

## Forbidden

- Goldens; launchers (`scripts/herdr-soho`, `.cmd`); outros arquivos de
  `lib/`.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera. Não rode a
suíte completa.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e as mutações
executadas.
