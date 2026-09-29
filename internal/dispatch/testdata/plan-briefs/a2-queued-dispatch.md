# Brief — A2: prompt para worker ocupado fica `queued`, não `not-received`

Role: implementer · Agent: build-4 · Report language: pt-BR

## Goal

Um `dispatch` (brief ou `--amend`) enviado a um worker que está `working` hoje espera 20 s por um
"settle" que nunca vem, manda Enter, às vezes reenvia, e termina `not-received` (exit 15) — mas o
worker recebeu e leu a mensagem ao fim do turno. Dois projetos relataram o mesmo caso:
`/work/herdr-soho/.herdr-soho/feedback/from-pinar-2026-09-28-c.md`
(atrito 1) e `/work/herdr-soho/.herdr-soho/feedback/from-edger-2026-09-28-b.md`
(item 1). O falso `not-received` leva a reenvio duplicado e dúvida sobre qual relatório vale.

Worktree: `/work/herdr-soho/.worktrees/build-4`, branch
`fix/dispatch-queued` (já criado, commit `02910bb`, a árvore integrada). Este brief não tem relação
com o anterior deste painel.

## Decisions already made

1. **Estado antes do envio.** O `dispatch` lê o estado do alvo antes do envio (já lê o
   `state_change_seq`). Se o estado é `working`:
   - não há espera de settle nem o aviso `did not settle`;
   - nenhuma tecla é mandada depois do envio (nem Enter, nem reenvio) enquanto o alvo segue
     `working`.
2. **Evidência de fila.** Dentro da janela de `prompt_check_seconds`, com o alvo que estava `working`:
   - regra 1 de hoje (seq mudou com `working`/`blocked`, ou relatório não vazio) → chegou, como hoje;
   - o caminho do prompt composto ou o marcador `Read the file ` visível na tela recente (qualquer
     posição) → `queued`;
   - nada disso → `not-received` (exit 15) como hoje, mas o aviso diz que o alvo estava ocupado e
     que nenhuma tecla foi mandada: `prompt to '<agent>' not confirmed: it was working and shows no
     sign of the prompt; no key was sent. Read the pane before sending anything else.`
3. **Saída.** `dispatch --no-wait` com `queued` → JSON com `wait_status: "queued"` (mesmas chaves
   do `submitted`, na mesma ordem), exit 0, e no stderr
   `herdr-soho: prompt queued: '<agent>' is working; it takes the prompt when its turn ends`.
   `dispatch` sem `--no-wait` segue para a espera do relatório como hoje.
4. **Seguimento.** O `queued` grava um marcador `<state>/wait/<agent>.queued` com o mesmo formato
   do `.not-received` (`<epoch> <seq>`). O `wait` trata esse marcador como trata o `.not-received`
   hoje: se o alvo sai de `working` e o prompt continua na caixa de entrada, manda Enter (até 3 vezes,
   um por janela); se o alvo volta a trabalhar com seq novo, ou o relatório aparece, os marcadores
   somem e a espera segue; se o alvo fica parado sem o prompt na caixa e sem relatório, termina
   `not-received` (exit 15). O `status` segue read-only como hoje e não reporta `not-received` para
   um alvo com `.queued` enquanto ele está `working`.
5. **Estatística.** `stats` não conta `queued` como `not_received`. O sidecar da tentativa grava
   `arrival: "queued"`.
6. Alvo que não estava `working` antes do envio: comportamento de hoje, byte a byte (settle, regras
   1 a 4, Enter, reenvio).
7. Documentação: a frase sobre a checagem de chegada em `SKILL.md` ("`dispatch` also checks that
   the prompt arrived…") e a lista de estados do `wait`/`dispatch` ganham o `queued`, em inglês, no
   estilo do texto ao lado. `docs/guide.md` só se ele descreve a checagem de chegada.

## Expected result

- Testes em `test/dispatch-arrival.test.mjs` com herdr falso (sem Herdr real,
  `HERDR_SOCKET_PATH` para um caminho inexistente, ids `w0test:p0a`):
  a. alvo `working` antes, marcador visível depois → `queued`, exit 0 com `--no-wait`, nenhum
     `send-keys` registrado, e nenhuma espera de settle (o teste mede: termina antes de 20 s);
  b. alvo `working` antes, nada visível → `not-received` exit 15 com a mensagem da decisão 2 e
     nenhum `send-keys`;
  c. alvo `working` antes, seq muda → chegou, como hoje;
  d. `queued`, depois o alvo fica `idle` com o prompt na caixa → o `wait` manda Enter e segue;
  e. `queued`, depois o alvo volta a `working` com seq novo e escreve o relatório → `done`;
  f. alvo `idle` antes: os casos a–f atuais de `dispatch-arrival.test.mjs` passam sem mudança.
  Cada um com `// Mutation captured: …` executado (cole a saída vermelha).
- `stats` com um par `queued` → `not_received` 0 (teste em `stats.test.mjs`).
- `node --test` e `bun test --timeout 60000` de `dispatch-arrival.test.mjs`, `dispatch.test.mjs`,
  `wait.test.mjs`, `status.test.mjs`, `stats.test.mjs` → 0 fail (cole). Se um golden de paridade
  mudar, regrave com `HERDR_SOHO_GOLDEN=update` e mostre o diff decodificado.

## Owned files

- `skills/herdr-soho/scripts/lib/dispatch.mjs` (só a checagem de chegada e a saída do `queued`)
- `skills/herdr-soho/scripts/lib/arrival.mjs`, `lib/wait.mjs` (só o seguimento do marcador),
  `lib/commands/status.mjs` (só a decisão 4), `lib/commands/stats.mjs` (só a decisão 5)
- Os testes citados e `scripts/test/golden/*` se mudarem
- `skills/herdr-soho/SKILL.md` e `docs/guide.md` (só a decisão 7)

## Forbidden

- Todo o resto e qualquer outro worktree. Herdr real que escreva.
- Nenhum comando git que escreva neste repositório.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, por item `[done]` / `[partial]` / `[skipped]` + motivo, com saídas
coladas e as mutações.
