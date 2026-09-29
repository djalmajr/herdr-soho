# Brief — W3: terceira rodada do Windows (fixture do treekill e os P3 da revisão)

Role: implementer · Agent: build · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

Depois da segunda rodada, no Windows real sobram 2 falhas, ambas nos testes novos do caminho `.cmd`
com timeout (`treekill`): voltam `status: null` sem erro. Uma diagnose no mesmo Windows provou que o
produto funciona: `runCli` com `.cmd` e timeout devolve status 0 e a saída certa com `TMPDIR`
existente ou ausente e ambiente completo ou mínimo. Então a falha é do fixture dos testes. Consertar
os dois testes e fechar os 3 P3 da revisão da segunda rodada.

Worktree: `/work/herdr-soho/.worktrees/s4`, branch `fix/windows-tests`
(commit `ce37f23`, a segunda rodada).

Leia antes:
- Log do Windows desta rodada (Bun e Node, as duas falhas com a asserção e o JSON do resultado):
  `/tmp/hs-win-logs/hs-w2.utf8.txt`.
- A diagnose que passou no Windows (script e saída): `/tmp/hs-go/tkdiag.mjs` e
  `/tmp/hs-go/tkdiag-result.txt`. Compare o fixture e o ambiente dela com os dos dois testes.
- Revisão da segunda rodada (3 P3): `/work/herdr-soho/.herdr-soho/w14/reports/review-2-20260928T202724.md`.

## Decisions already made

1. Os dois testes `Windows .cmd timeout …` (em `collect.test.mjs`) passam a usar um fixture e um
   ambiente que funcionam no Windows real, tomando a diagnose como referência. A diferença que
   explica o `status: null` vai para o relatório. Se nenhuma diferença explicar, marque `[partial]`
   com as hipóteses e escreva um roteiro de diagnose em `/tmp/hs-go/build/tk-diag2.mjs` que o
   orquestrador roda no Windows.
2. P3 #1: o teste `Decision 2: Windows returns the base message even with a Codex ancestor` põe um
   `herdr` falso no `PATH` (`writeFakeCli`, já usado no arquivo), para a mutação que remove o
   curto-circuito do win32 falhar o teste.
3. P3 #2: o `tempFileError` do `runCli` põe no `stderr` do resultado
   `herdr-soho: cannot write temporary files: <código>`, e os consumidores que já leem `stderr`
   (`structuredError`, `setup-probe`) passam a mostrá-lo; teste do `send` com `TMPDIR` apontando para
   um arquivo.
4. P3 #3: o teste de unidade de `buildPromptArgs` recebe o texto montado pelas funções reais
   (`peerHeader`, `quotePeerBody`…) e confere o prompt inteiro, não um texto escrito à mão.

## Expected result

- As mudanças, com a causa das duas falhas (linha do log contra a diagnose).
- No Mac: `node --test` e `bun test --timeout 60000` dos arquivos tocados → 0 fail (cole).
- Uma mutação para cada P3 (cópia fora do repositório,
  `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes)
  mostrando o teste vermelho.
- `/tmp/hs-go/build/win-check.ps1` atualizado com os arquivos tocados (mesmo formato do anterior).
  Não execute.

## Owned files

- `skills/herdr-soho/scripts/test/collect.test.mjs`, `codex-env.test.mjs`, `send.test.mjs`,
  `peer.test.mjs`
- `skills/herdr-soho/scripts/lib/platform.mjs` (só o `tempFileError`) e os consumidores citados no
  item 3 (só a leitura do `stderr`)

## Forbidden

- Todo o resto e qualquer outro worktree. Herdr real que escreva.
- Nenhum comando git que escreva neste repositório.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, por item `[done]` / `[partial]` / `[skipped]` + motivo, com saídas
coladas e as mutações.
