# Brief — S4e: prova de chegada do `send` sem reenvio, diálogo só no fundo da tela

Role: implementer · Agent: build · Report language: pt-BR

Worktree `.worktrees/s4`, branch `feat/peer-send`, commit `88c39a7`.
Caminhos relativos à raiz dela. Brief anterior (S4d):
`/work/herdr-soho/.herdr-soho/w14/plan-briefs/s4d-send-arrival-dialog.md`.
Revisão que motiva esta fatia (leia os cinco achados e as sondas):
`/tmp/herdr-soho/w14/reports/soho-grok-r8-20260928T152710.md`.

## Goal

O `send` nunca manda a mesma mensagem duas vezes, só diz `sent` com prova,
e só se recusa por diálogo quando o diálogo está de fato no fundo da tela.

## Decisions already made (substituem as decisões 1 e 2 do S4d onde conflitam)

1. **Formato.** A primeira linha volta a começar pelo literal de
   `PEER_PREFIX`: `[herdr-soho:peer] #<id> Message from another agent — …`
   (o resto da linha igual). A mensagem ganha uma última linha, depois do
   corpo citado: `[herdr-soho:peer] #<id> end of message`. Use a constante,
   não o literal. O bloco de setup não muda. Atualize `SKILL.md` e
   `docs/guide.md` (formato, prova, códigos).
2. **Sem reenvio, nunca.** Remova o reenvio automático do ramo "ausente".
3. **Prova de chegada** (janela de 15 s, poll ~1 s, como hoje). Logo antes
   do `agent prompt`, leia `state_change_seq` com `agent get` (preSeq) e
   guarde a tela visível (preScreen). A chegada está provada quando
   **qualquer** destas vale:
   - (a) `agent get` devolve `state_change_seq` não vazio e diferente de um
     preSeq não vazio;
   - (b) o `#<id>` aparece em `agent read --source recent-unwrapped
     --lines <linhas da mensagem + 60>`, a tela visível difere de
     preScreen, e a linha final (`… #<id> end of message`) **não** está
     entre as últimas 15 linhas não vazias da tela visível.
4. **Sem prova ao fim da janela:** um único `Enter` (`agent send-keys
   <pane> enter`) e uma nova janela com as mesmas provas. Ainda sem prova
   → exit 15, log `lost`, mensagem
   `send: <ref> did not take the message (no sign of it in its state or transcript); read its pane before sending again`.
5. **Leitura que falha não é ausência.** Se, numa janela, todas as
   leituras (`agent get` e `agent read`) falharem, nada de Enter: exit 15,
   log `unverified`, mensagem
   `send: could not confirm that <ref> took the message (<causa>); read its pane before sending again`.
   `agentReadScreen` passa a distinguir falha de tela vazia.
6. **Diálogo só no fundo.** Os padrões de confiança/confirmação e os
   marcadores de pergunta de `dialog.mjs` são testados só nas últimas 20
   linhas não vazias da tela visível. Os marcadores de `dialog.mjs` só
   contam com o alvo `blocked` (como `wait`/`status`); os padrões de
   confiança contam com qualquer status (o Cursor mostrava o diálogo com
   status `idle`).
7. **Leitura da tela visível que falha antes do envio:** nada é enviado,
   exit 4, log `unreadable`,
   `send: could not read <ref>'s screen (<causa>); nothing was sent`.
8. **Variáveis de teste só encurtam.** `HERDR_SOHO_SEND_WINDOW_MS` com teto
   de 15000 (fora da faixa → 15000), como o poll.

## Acceptance criteria

1. Testes com `herdr` falso, ids impossíveis e `HERDR_SOCKET_PATH`
   isolado, cada um com `// Mutation captured: …` executado:
   - as sondas da revisão viram testes: corpo de 57 linhas entregue (seq
     muda) → `sent`, um único prompt; leitura `recent` sempre falhando →
     exit 15 `unverified`, um prompt, zero Enter; citação `[y/N]` acima das
     últimas 20 linhas → envia; `Trust this workspace` no fundo com status
     `idle` → exit 17 sem prompt; leitura visível falhando → exit 4 sem
     prompt; `HERDR_SOHO_SEND_WINDOW_MS=999999` → 15000;
   - prova (a) sozinha (id nunca visível, seq muda) → `sent`;
   - prova (b) sozinha (seq igual, id no histórico, linha final fora do
     fundo) → `sent`;
   - mensagem parada na caixa (linha final nas últimas 15, seq igual) →
     um Enter, depois seq muda → `sent`; sem mudança → exit 15 `lost`,
     **um** prompt no total;
   - primeira e última linha exatas, começando por `[herdr-soho:peer] #`.
2. `node --test` e `bun test --timeout 60000` de `send.test.mjs`,
   `setup*.test.mjs`, `parity-setup.test.mjs`, `parity-entry.test.mjs` →
   0 fail (cole).
3. Nenhuma execução de `send` fora dos testes. Não mande nada a painel
   real.

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `skills/herdr-soho/scripts/lib/peer.mjs`,
  `skills/herdr-soho/scripts/lib/commands/send.mjs`,
  `skills/herdr-soho/scripts/test/send.test.mjs`
- `skills/herdr-soho/SKILL.md`, `docs/guide.md` (só o `send`)

## Forbidden

- Todo o resto (`dialog.mjs` é importado, não alterado). Mandar prompt,
  teclas ou mensagem a painéis reais.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera. Não rode a
suíte completa.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e as mutações
executadas.
