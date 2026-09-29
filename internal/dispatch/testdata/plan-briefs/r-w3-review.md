# Revisão R-W3 — terceira rodada do Windows

Role: reviewer · Agent: review · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

Revisar, antes do push, a última rodada das correções da suíte no Windows. Com ela, os arquivos
tocados passam no Windows real (Node 109 pass, 0 fail, 6 skip; Bun igual). A rodada mexe em
produção num ponto pequeno (`tempFileError` do `runCli` e a leitura dele no `setup-probe`) e no resto
são testes.

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-w3`, branch `fix/windows-tests`, commit `e801707` (pai `ce37f23`).
  Diff: `git -C <worktree> show e801707`.
- Brief: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/w3-windows-round3.md`.
- Relatório do implementador:
  `/tmp/herdr-soho/w14/reports/build-20260928T213821.md`.
- Revisão anterior (os 3 P3 que esta rodada fecha):
  `/work/herdr-soho/.herdr-soho/w14/reports/review-2-20260928T202724.md`.
- Resultado no Windows: `/tmp/hs-win-logs/hs-w3.utf8.txt`.

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. Os 3 P3 anteriores fechados, cada um com a mutação que o teste agora pega (rode).
2. A causa das duas falhas do treekill (`ComSpec`) está certa e a correção do fixture não esconde
   defeito de produção (compare com o caminho de produção).
3. O `stderr` novo do `tempFileError` e a exibição no `setup-probe`: texto, ninguém mais afetado.
4. `node --test` e `bun test --timeout 60000` dos arquivos tocados no Mac → cole.

Nenhum Herdr real: `HERDR_SOCKET_PATH=/tmp/hs-go/review/none.sock`.

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real que escreva. Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
