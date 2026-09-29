# Brief — S4d: `send` só dá como entregue com prova, e nunca digita num diálogo

Role: implementer · Agent: build · Report language: pt-BR

Worktree `.worktrees/s4`, branch `feat/peer-send`, commit `73f0f62`.
Caminhos relativos à raiz dela. Brief original do `send`:
`/work/herdr-soho/.herdr-soho/w14/plan-briefs/s4-send.md`.

## Problemas observados na validação real (2026-09-28 ~18:02Z)

Quatro agentes descartáveis recém-abertos com `herdr agent start` (todos
`idle`, `interactive_ready: true`), cada um recebeu um `send`:
- Claude e Grok: entregue, cabeçalho e corpo citado corretos.
- **Codex:** o `send` imprimiu `sent to …` (exit 0, log `sent`), mas a
  mensagem não chegou: a tela ficou só com a abertura, "Context 0% used",
  nada no histórico. Entrega falsa: o `agent prompt --wait` aceitou.
- **Cursor:** o painel mostrava o diálogo "Trust this workspace [a] Trust
  / [q] Quit" enquanto o Herdr dizia `idle`; o texto da mensagem foi
  digitado no diálogo e uma letra `a` o respondeu ("Trusting
  workspace…"); a mensagem se perdeu (`agent_prompt_stalled`, exit 15).

## Goal

O `send` só diz `sent` com prova de que **esta** mensagem chegou à conversa
do alvo, e não digita nada numa tela de diálogo.

## Decisions already made

1. **Id da mensagem.** A primeira linha do cabeçalho passa a
   `[herdr-soho:peer <id>] Message from another agent — …`, com `<id>` = 8
   caracteres hexadecimais aleatórios por envio (as outras linhas do
   cabeçalho não mudam). O log `peer-messages.tsv` ganha uma coluna `id`
   no fim.
2. **Prova de chegada.** Depois do `agent prompt`, numa janela de 15 s
   (poll ~1 s), leia a tela recente do alvo (`agent read --source
   recent-unwrapped --lines 60`, com `--machine` quando remoto):
   - o `<id>` aparece **fora** das últimas 3 linhas não vazias → entregue
     (`sent`);
   - o `<id>` só aparece nas últimas 3 linhas (texto parado na caixa de
     entrada) → um `Enter` (`agent send-keys … enter`), nova janela;
   - não aparece → **um** reenvio do mesmo texto (mesmo id), nova janela;
   - ainda nada → exit 15 (`did not take the message (not seen in its
     transcript)`), log `lost`.
   O `--wait` do `agent prompt` continua como está, mas não é mais prova
   de entrega.
3. **Diálogo não é caixa de entrada.** Antes de enviar (e depois da espera
   por ocioso), leia a tela visível. Se ela casa com um diálogo — os
   detectores de pergunta/aprovação que a skill já usa em `wait`/`status`
   (ache-os e reuse) **mais** os padrões de confiança de pasta:
   `Trust this workspace`, `trust this folder`, `Do you trust`,
   `Enter to confirm`, `[y/N]`, `(y/n)` (sem diferenciar maiúsculas) —,
   trate como `blocked`: espere até o `--timeout` que o diálogo suma; se
   não sumir, exit 17 `herdr-soho: send: <ref> is showing a dialog;
   nothing was sent`, log `dialog`. Nada é digitado.
4. Documente (SKILL.md, guia) o id, a prova de chegada e a regra do
   diálogo; o cabeçalho no bloco de setup só muda se o texto dele citar a
   primeira linha.

## Acceptance criteria

1. Testes com `herdr` falso (ids impossíveis e `HERDR_SOCKET_PATH`
   isolado, como o arquivo já faz): id no cabeçalho e no log; tela com o
   id fora da caixa → `sent`; id só na caixa → um Enter e depois
   entregue; id nunca visível → um reenvio e exit 15 `lost`; tela de
   diálogo de confiança → nenhum `agent prompt`, exit 17 `dialog`; diálogo
   que some antes do timeout → envia. Cada um com `// Mutation captured:
   …` executado.
2. `node --test` e `bun test --timeout 60000` de `send.test.mjs`,
   `setup*.test.mjs`, `parity-setup.test.mjs`, `parity-entry.test.mjs` →
   0 fail (cole).
3. Nenhuma execução de `send` fora dos testes com `herdr` falso e socket
   isolado. Não mande nada a painel real.

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `skills/herdr-soho/scripts/lib/peer.mjs`,
  `skills/herdr-soho/scripts/lib/commands/send.mjs`,
  `skills/herdr-soho/scripts/test/send.test.mjs`
- `skills/herdr-soho/SKILL.md`, `docs/guide.md` (só o `send`)
- `skills/herdr-soho/scripts/lib/setuptext.mjs` e goldens, só se o item
  do bloco citar a primeira linha do cabeçalho

## Forbidden

- Todo o resto (os detectores de diálogo que você reusar são importados,
  não copiados nem alterados). Mandar prompt, teclas ou mensagem a
  painéis reais.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera. Não rode a
suíte completa.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e as mutações
executadas.
