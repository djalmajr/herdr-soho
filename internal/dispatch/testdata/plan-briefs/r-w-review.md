# Revisão R-W — correções da suíte no Windows

Role: reviewer · Agent: review · Report language: pt-BR

## Goal

Revisar, antes do push, o commit que conserta as falhas conhecidas da suíte no Windows. A pergunta
central: cada teste pulado ou condicionado no Windows esconde só uma limitação do fixture, ou
esconde defeito de produção?

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-w`, branch
  `fix/windows-tests`, commit `02c88b1` (pai `02910bb`). Diff: `git -C <worktree> show 02c88b1`.
- Brief da fatia: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/w-windows-tests.md`.
- Relatório do implementador:
  `/tmp/herdr-soho/w14/reports/build-4-20260928T192715.md`.
- Logs do Windows antes da correção: `/tmp/hs-win-logs/node.tap.txt`, `/tmp/hs-win-logs/plugin.tap.txt`.
- O orquestrador roda agora os arquivos tocados no Windows; o resultado sai em
  `/tmp/hs-win-logs/hs-w.utf8.txt` (pode não existir ainda quando você começar; se existir, use).

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. `runCliTreeKill` (`skills/herdr-soho/scripts/lib/platform.mjs`): criar o `TMPDIR` com
   `recursive` é seguro? Pense em `TMPDIR` relativo, apontando para arquivo, sem permissão, e em
   quem mais usa `os.tmpdir()` no mesmo caminho sem criar. O teste de regressão falha sem a
   correção (rode a mutação numa cópia).
2. Cada `skip` e cada asserção condicionada ao win32 em `codex-env.test.mjs`, `send.test.mjs` e
   `plugin/test/picker.test.mjs`: o motivo está certo? Sobrou no Windows asserção suficiente para
   pegar um defeito de produção do mesmo caminho? Em especial: o texto multilinha do `send` chega
   inteiro a um `herdr.exe` real no Windows (leia como `send` monta os argumentos e como
   `cmdInvocation` trata um `.exe`)?
3. Os casos de clipboard 63 e 65 do plugin (o implementador deixou `[partial]`): pelo código de
   `plugin/clipboard.mjs` e do fixture, a queda para `osc52` no Windows é do fixture ou da produção?
4. No Mac: `node --test` e `bun test --timeout 60000` dos quatro arquivos tocados → cole o resumo.

Nenhum Herdr real: `HERDR_SOCKET_PATH=/tmp/hs-go/review/none.sock`.

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real que escreva. Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
