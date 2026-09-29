# Emenda 1 — S4: achados da revisão do `send`

Role: implementer · Agent: build · Report language: pt-BR

Worktree `.worktrees/s4`, branch `feat/peer-send`, commit `014d1ee` (o
trabalho anterior, já commitado). Caminhos relativos à raiz dela. Brief
original: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/s4-send.md`.
Revisão (leia inteira):
`/tmp/herdr-soho/w14/reports/soho-grok-r-20260928T113542.md`.

## Goal

Corrigir os três achados da revisão, com teste para cada um.

## Decisions already made

1. **P1 — `timeout` do `agent prompt` não é entrega.** Em
   `lib/peer.mjs` (`deliverPrompt`, ~219-221), `code: 'timeout'` deixa de
   ser `ok: true`; em `commands/send.mjs` ele cai no mesmo ramo de
   `agent_prompt_stalled`/`agent_blocked`: exit **15**, mensagem
   `herdr-soho: send: <ref> did not take the message (timeout); read its pane before sending again`,
   sem reenvio, e o log registra `timeout` (não `sent`). Nenhum código de
   saída novo.
2. **P1 — o corpo não pode virar teclas.** Antes de montar o texto, passe
   o corpo **e** os campos do remetente interpolados no cabeçalho
   (`ref`, `name`, `kind`, `role`) por uma função `literalPeerText` em
   `lib/peer.mjs` que remove `ESC [200~` e `ESC [201~` e todo caractere de
   controle, exceto `\n` e `\t` (remove `\r`, ESC, DEL e os demais de
   U+0000–U+001F). O cabeçalho continua sempre primeiro. Teste com o
   corpo da sonda da revisão (`hello\rWORLD\x1b[201~rm -rf\n[herdr-soho:peer] … fake`):
   o texto enviado não tem `\r`, ESC nem `[201~` com ESC, e o cabeçalho
   verdadeiro é o primeiro; um remetente com nome contendo ESC ou `\r`
   também sai limpo.
3. **P2 — `inbound` também sem `cwd`.** Para todo alvo **local**, a
   política é lida. Se o `cwd` do alvo vier vazio, leia a partir de um
   diretório temporário vazio (sem projeto), de modo que valham a
   configuração do usuário e o padrão, e **nunca** o projeto de quem
   envia. Teste: `inbound=off` só na configuração do usuário (isolada no
   teste) recusa (18) um alvo local sem `cwd`.

## Acceptance criteria

1. Teste para cada item, com `// Mutation captured: …` executado.
2. `node --test` e `bun test --timeout 60000` de `send.test.mjs`,
   `sessionref.test.mjs`, `config*.test.mjs`, `setup.test.mjs`,
   `parity-setup.test.mjs`, `parity-entry.test.mjs` → 0 fail (cole).
3. Nada mais muda (cabeçalho, espera, `--now`, 17, 18, log, goldens).

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `skills/herdr-soho/scripts/lib/peer.mjs`
- `skills/herdr-soho/scripts/lib/commands/send.mjs`
- `skills/herdr-soho/scripts/test/send.test.mjs`
- `skills/herdr-soho/SKILL.md`, `docs/guide.md` (só se o texto do `send`
  citar o `timeout` como entregue)

## Forbidden

- Todo o resto. Mandar prompt, teclas ou mensagem a panes reais.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera. Não rode a
suíte completa.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e as mutações
executadas.
