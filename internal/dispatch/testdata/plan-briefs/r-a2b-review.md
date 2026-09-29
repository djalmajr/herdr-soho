# Revisão R-A2b — correções do `queued`

Role: reviewer · Agent: review · Report language: pt-BR

Este brief não tem relação com o anterior deste painel (é a nova revisão da A2, que você revisou).

## Goal

Revisar a rodada que fecha os 6 achados da sua revisão da A2. O risco continua o mesmo: um prompt
perdido reportado como `queued`, uma cota escondida, ou uma tecla para um worker ocupado ou para um
eco já consumido.

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-a2`, branch
  `fix/dispatch-queued`, commit `78c3665` (pai `1294237`, a A2). Diff: `git -C <worktree> show 78c3665`.
- Sua revisão da A2, com o harness de cenários:
  `/work/herdr-soho/.herdr-soho/w14/reports/review-20260928T204701.md`.
- Brief: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/a2b-queued-fix.md`.
- Relatório do implementador:
  `/tmp/herdr-soho/w14/reports/build-4-20260928T212351.md`.

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. Os seus cenários (`s1-*`, `s2-*`, `s3-consumed-echo`) refeitos: resultado certo em cada um.
2. Cenários novos seus: marcador `.queued` de dois campos (formato antigo); dois `wait` ao mesmo tempo;
   `status` enquanto um `wait` segue o marcador; o caminho do prompt quebrado em duas linhas na tela
   (terminal estreito); prompt na caixa com o worker em `blocked` num diálogo.
3. A tabela de mutações da sua revisão refeita linha por linha.
4. `node --test` e `bun test --timeout 60000` de `dispatch-arrival`, `dispatch`, `wait`, `status`,
   `stats` → cole.

Nenhum Herdr real: herdr falso, `HERDR_SOCKET_PATH=/tmp/hs-go/review/none.sock`, ids impossíveis.

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real que escreva. Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
