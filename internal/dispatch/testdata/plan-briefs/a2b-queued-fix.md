# Brief — A2b: correções da revisão da A2 (`queued`)

Role: implementer · Agent: build-4 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel (é a correção da sua A2).

## Goal

A revisão da A2 reprovou com 1 P1 e 1 P2, provados com herdr falso: depois de um `queued`, uma cota
estourada vira `not-received` e o `wait` manda Enter a quem já consumiu o prompt; e o marcador
genérico de um prompt anterior ainda na tela faz um prompt perdido virar `queued`. Fechar os 6
achados.

Worktree: `/work/herdr-soho/.worktrees/build-4`, branch `fix/dispatch-queued` (commit `1294237`, a A2).

Leia antes:
- Revisão, com os cenários e o harness do revisor (`/tmp/hs-go/review/a2r/harness/scen.mjs`, se
  ainda existir):
  `/work/herdr-soho/.herdr-soho/w14/reports/review-20260928T204701.md`.
- Brief da A2: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/a2-queued-dispatch.md`.

## Decisions already made

1. **Evidência de fila (F2).** Só o caminho do prompt composto deste envio (único por envio) visível
   na tela recente conta como evidência de `queued`. O marcador genérico `Read the file ` sozinho não
   conta. Sem o caminho: `not-received` como na decisão 2 da A2.
2. **Seguimento no `wait` e no `status` (F1).** O `.queued` passa a guardar também o caminho do
   prompt composto (`<epoch> <seq> <caminho>`; um marcador antigo de dois campos continua válido e
   usa o caminho do último dispatch do agente). Com o alvo fora de `working`:
   - primeiro rodam as sondas normais (cota, erro de provedor, capacidade, diálogo, pergunta); se uma
     delas decide, os marcadores `.queued`/`.enter-retry` somem e vale o resultado dela;
   - "prompt ainda na caixa de entrada" só quando o caminho deste prompt está nas últimas 3 linhas não
     vazias da tela visível (a mesma regra de caixa de entrada que o `dispatch` usa em
     `composedPathSeenOutsideInput`); fora delas é eco de um prompt já consumido: nenhuma tecla;
   - seq diferente do marcador e prompt fora da caixa: o marcador some e segue a sonda normal (como o
     ramo do `.not-received` faz hoje);
   - o `status` segue read-only e só reporta `not-received` para um `.queued` com o prompt na caixa.
3. **F3.** Um `wait` que termina `not-received` a partir do `.queued` grava o `.not-received` (com o
   seq atual) antes de remover o `.queued`, para o `wait` e o `status` seguintes dizerem o mesmo. A
   documentação do `SKILL.md` descreve esse fim.
4. **F4, F5, F6.** Como a revisão sugere: com o alvo `working` no mesmo seq, a sonda normal segue
   (sem `return 'working'` antecipado); `.queued` vazio ou ilegível usa o agora como época; testes que
   matam as quatro mutações sobreviventes do F6 — refaça a tabela da revisão linha por linha, com o
   código de saída de cada mutação colado; mutações suas no lugar das da revisão não contam.
5. Os cenários `s1-plain`, `s1-amend`, `s1-control`, `s2-quota-after-queued`,
   `s2-control-notreceived` e `s3-consumed-echo` da revisão viram testes em
   `dispatch-arrival.test.mjs`, `wait.test.mjs` e `status.test.mjs`.

## Expected result

- Os seis cenários da revisão como testes, com o resultado certo: `s1-*` → `not-received` (exit 15)
  sem tecla; `s2-quota-after-queued` → `quota` (exit 11) no `status` e no `wait`; `s3-consumed-echo` →
  nenhum Enter. Cada um com `// Mutation captured: …` executado (cole a saída vermelha).
- `node --test` e `bun test --timeout 60000` de `dispatch-arrival`, `dispatch`, `wait`, `status`,
  `stats` → 0 fail (cole).

## Owned files

Os da A2: `lib/dispatch.mjs` (só a checagem de chegada), `lib/arrival.mjs`, `lib/wait.mjs` (só o
seguimento dos marcadores), `lib/commands/status.mjs` (só o `.queued`), os testes citados e a frase
do `SKILL.md`/`docs/guide.md` sobre o `queued`.

## Forbidden

- Todo o resto e qualquer outro worktree. Herdr real que escreva.
- Nenhum comando git que escreva neste repositório.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, por achado `[done]` / `[partial]` / `[skipped]` + motivo, com saídas
coladas e as mutações.
