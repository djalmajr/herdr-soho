# Revisão R-W2 — segunda rodada das falhas do Windows

Role: reviewer · Agent: review-2 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

Revisar, antes do push, a segunda rodada das correções da suíte no Windows. A primeira rodada foi
reprovada pela revisão `review-20260928T194610.md` (4 P2, 2 P3). Confira se cada achado fechou e se a
rodada não abriu defeito novo, em especial no `runCli` (produção) e no `peer.mjs` (extração de
função).

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-w2`, branch
  `fix/windows-tests`, commit `ce37f23` (pai `02c88b1`, a primeira rodada). Diff da rodada:
  `git -C <worktree> show ce37f23`; das duas: `git -C <worktree> diff 02910bb ce37f23`.
- Revisão anterior: `/work/herdr-soho/.herdr-soho/w14/reports/review-20260928T194610.md`.
- Brief desta rodada: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/w2-windows-round2.md`.
- Relatório do implementador:
  `/tmp/herdr-soho/w14/reports/build-20260928T200632.md`.
- Resultado no Windows desta rodada: o orquestrador está rodando agora; sai em
  `/tmp/hs-win-logs/hs-w2.utf8.txt`. Se existir quando você chegar ao item 4, use; senão diga.

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. Cada um dos 6 achados da revisão anterior: fechado, com prova executada no Mac quando der.
2. `runCli` (`lib/platform.mjs`): a pasta temporária única nas três rotas; `TMPDIR` ausente,
   relativo, arquivo, sem permissão → resultado com `error` e sem exceção; nenhuma mudança para
   quem tinha um `TMPDIR` válido. Rode as sondas.
3. `peer.mjs`: a extração da função que monta o argv não muda o comportamento do `send` (compare a
   saída do `send` com herdr falso antes e depois, no Mac).
4. Windows: se o log existir, as falhas que sobraram e a causa provável.
5. `node --test` e `bun test --timeout 60000` dos arquivos tocados no Mac → cole os resumos.

Nenhum Herdr real: `HERDR_SOCKET_PATH=/tmp/hs-go/review/none.sock`.

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real que escreva. Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
