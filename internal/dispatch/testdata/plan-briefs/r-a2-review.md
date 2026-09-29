# Revisão R-A2 — prompt para worker ocupado fica `queued`

Role: reviewer · Agent: review · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

Revisar, antes do push, a mudança da checagem de chegada do `dispatch` para alvos que já estavam
`working`: sem espera de settle, sem teclas, estado `queued` quando a tela mostra o prompt, e o
seguimento pelo `wait`. Dois projetos relataram o falso `not-received` que isso corrige. O risco
agora é o contrário: um prompt perdido reportado como `queued`, ou uma tecla mandada a um worker
ocupado.

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-a2`, branch
  `fix/dispatch-queued`, commit `1294237` (pai `02910bb`). Diff: `git -C <worktree> show 1294237`.
- Brief: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/a2-queued-dispatch.md`.
- Relatório do implementador:
  `/tmp/herdr-soho/w14/reports/build-4-20260928T194517.md`.
- Relatos: `/work/herdr-soho/.herdr-soho/feedback/from-pinar-2026-09-28-c.md`
  (atrito 1) e `from-edger-2026-09-28-b.md` (item 1).

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. Evidência de fila: o marcador `Read the file ` ou o caminho do prompt "em qualquer posição da tela
   recente" pode vir de um prompt anterior ainda visível (um `--amend` logo depois do brief, o mesmo
   worker reaproveitado). Isso daria `queued` para um prompt que não chegou? Monte o cenário com
   herdr falso e diga.
2. Alvo `idle` antes do envio: comportamento idêntico ao de antes (settle, regras 1 a 4, Enter,
   reenvio). Rode os casos antigos de `dispatch-arrival.test.mjs` e compare com o pai `02910bb`.
3. O seguimento no `wait` com o marcador `.queued`: Enter só quando o alvo saiu de `working` e o
   prompt ainda está na caixa; limpeza por seq novo e por relatório; `not-received` quando o alvo
   para sem prompt e sem relatório. Procure corrida entre dois `wait` ou entre `wait` e `status`.
4. JSON do `queued` (mesmas chaves e ordem do `submitted`) e o texto do stderr; `stats` sem contar
   `queued`; a documentação em `SKILL.md`/`docs/guide.md` bate com o código.
5. `node --test` e `bun test --timeout 60000` de `dispatch-arrival`, `dispatch`, `wait`, `status`,
   `stats` → cole os resumos.

Nenhum Herdr real: herdr falso e `HERDR_SOCKET_PATH=/tmp/hs-go/review/none.sock`, ids impossíveis.

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real que escreva. Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
