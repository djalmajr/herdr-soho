# Emenda 1 — I20: pipes próprios no modo padrão, matador de teste restrito, testes simulados fora do Windows

Role: implementer · Agent: build · Report language: pt-BR

Worktree `.worktrees/i20`, branch `fix/win-cmd-timeout-tree`, commit
`4981666` (o trabalho anterior, já commitado). Caminhos relativos à raiz
dela. Issue: https://github.com/djalmajr/herdr-soho/issues/20.

## Goal

Resolver os dois achados da revisão R20 e deixar os testes do helper
passando no Windows.

## Decisions already made

1. **Achado R20-1 (P2), modo padrão com pipes.** Hoje o helper dá
   `stdio: 'inherit'` ao comando também no modo padrão (pipes do
   `spawnSync` externo). Se o `.cmd` sai e um neto herda stdout/stderr,
   o `spawnSync` externo só volta no teto (`timeoutMs + 15000`) e o
   resultado é sucesso (`error: null`), enquanto o caminho clássico volta
   no `timeoutMs` com `ETIMEDOUT`. Sonda da revisão (COMSPEC Node, filho
   sai 0 e deixa um neto com stdout/stderr herdados, `timeoutMs: 800`):
   ```
   hold-default:     ms 15804  status 0  signal null  timedOut false  error null
   hold-outputFiles: ms 337    status 0 …
   hold-mergeOutput: ms 332    status 0 …
   hold-posix-classic (sem helper): ms 802  status 0  error "ETIMEDOUT"
   ```
   Correção decidida: **só no modo padrão**, o helper cria pipes próprios
   para o comando (`stdio: ['pipe' ou o stdin herdado para o `input`,
   'pipe', 'pipe']`), copia os bytes para `process.stdout`/`process.stderr`
   e, em `finish`, drena o que já chegou e sai. O neto fica segurando só o
   pipe interno, e o `spawnSync` externo volta quando o helper sai.
   `mergeOutput` e `outputFiles` continuam com `inherit` sobre os fds de
   arquivo. Teste portátil (roda no macOS/Linux pela simulação win32 que já
   existe) com o mesmo cenário da sonda: volta em bem menos que o teto
   (use um limite como `< 5000` ms com `timeoutMs` curto), com a saída
   escrita antes.
2. **Achado R20-2 (P2), matador de teste.** `HERDR_SOHO_TREEKILL_TEST_KILLER`
   só vale quando o valor é um caminho **absoluto** cujo `realpath` fica
   dentro de `fs.realpathSync(os.tmpdir())`; senão, o helper ignora a
   variável e usa o `taskkill`. Teste: um killer fora do temporário (ex.
   relativo, ou um executável do sistema) não é chamado.
3. **Testes simulados no Windows.** No Windows (Node 24, commit `4981666`)
   o teste real passou ("runCli (Windows only) … no process of the tree
   survives": ok), mas estes quatro falham lá porque simulam o win32 com
   fakes POSIX (`sh`):
   - `treekill helper: past the timeout the killer receives the child pid…`
     (killer POSIX; terminou no teto de 30 s);
   - `runCli win32 (simulated): mergeOutput mode …`, `… outputFiles mode …`,
     `… default mode with input …` (`null !== 0`).
   Eles passam a pular com `process.platform === 'win32'` e o motivo
   `simulates win32 with POSIX fakes; on Windows the real .cmd test runs`.
   O teste real do Windows continua como está.

## Acceptance criteria

1. Teste novo do item 1 falha com a versão atual (`inherit` no modo
   padrão) e passa com a correção; teste novo do item 2; cada um com
   `// Mutation captured: …` executado.
2. `node --test` e `bun test --timeout 60000` de `treekill.test.mjs`,
   `platform.test.mjs`, `models.test.mjs`, `herdr.test.mjs`,
   `setup-probe.test.mjs` no macOS → 0 fail (cole).
3. Nenhuma mudança no caminho POSIX, `.exe` ou sem timeout (o diff de
   `platform.mjs` fora do helper não muda).

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `skills/herdr-soho/scripts/lib/treekill-run.mjs`
- `skills/herdr-soho/scripts/lib/platform.mjs` (só `runCliTreeKill` e
  `finishTreeKill`, se precisar)
- `skills/herdr-soho/scripts/test/treekill.test.mjs`

## Forbidden

- Todo o resto. Nenhum comando git que escreva. No commit, push, tag, or
  PR. The orchestrator owns git.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera. Não rode a
suíte completa.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e as mutações
executadas.
