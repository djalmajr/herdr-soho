# Brief — S4f: provas de chegada do `send` sem falso "entregue" nem Enter às cegas

Role: implementer · Agent: build · Report language: pt-BR

**Onde trabalhar:** worktree
`/work/herdr-soho/.worktrees/s4`, branch
`feat/peer-send`, commit `fb9365b`. Seu painel abriu em outro worktree:
faça `cd` para esse caminho antes de qualquer comando e edite só arquivos
dele. Caminhos abaixo são relativos a essa raiz.

Brief anterior (S4e):
`/work/herdr-soho/.herdr-soho/w14/plan-briefs/s4e-arrival-proof-fix.md`.
Revisão que motiva esta fatia (leia os quatro achados e as sondas do
`/tmp/s4e-r11/probe.test.mjs`, se ainda existir):
`/tmp/herdr-soho/w14/reports/soho-grok-r11-20260928T162634.md`.

## Goal

O `send` só diz `sent` quando a prova é desta mensagem, e só pressiona
Enter quando a mensagem está visivelmente parada na caixa de entrada e não
há diálogo.

## Decisions already made (substituem as do S4e onde conflitam)

1. **Prova (a), estado (achado 1).** Leia `agent get` logo antes do
   `agent prompt` (preSeq e preStatus). A mudança de `state_change_seq`
   só prova a chegada quando preStatus era `idle` ou `done` **e** o status
   novo é `working` ou `blocked`, com o seq novo não vazio e diferente de
   preSeq. Com preStatus `working` (caso `--now`) a prova (a) não vale;
   só a (b).
2. **Prova (b), transcrição (achado 3).** Normalize um texto removendo
   todo espaço em branco (incluindo quebras de linha) e os caracteres de
   desenho de caixa (U+2500–U+257F). A (b) vale quando: o `#<id>` aparece
   no `recent-unwrapped` (linhas = linhas da mensagem + 60), a tela
   visível difere da preScreen, e a linha final normalizada **não** aparece
   em lugar nenhum da tela visível normalizada (a mensagem já saiu da
   área visível). Linha final visível em qualquer ponto → a (b) não prova.
3. **Enter (achado 2).** Ao fim de uma janela sem prova, antes de qualquer
   Enter: leia de novo a tela visível e o status (`agent get`).
   - `isDialogScreen` casa → nada de Enter; exit 17, log `dialog`,
     `send: <ref> is showing a dialog after the message was typed; press nothing and read its pane`.
   - O `#<id>` aparece nas últimas 15 linhas não vazias da tela visível
     (normalizadas como na decisão 2, linha a linha) → um Enter e a
     segunda janela.
   - Senão → nada de Enter; exit 15 `lost` (mensagem de hoje).
   Leitura que falha nesse passo → exit 15 `unverified`, sem Enter.
4. **Status na hora (achado 4).** No laço de diálogo antes do envio, cada
   iteração lê o status com `agent get` junto com a tela visível; o
   `isDialogScreen` recebe esse status, não o da resolução do alvo nem o
   literal `'idle'` depois da espera.

## Acceptance criteria

1. Testes com `herdr` falso, ids impossíveis e `HERDR_SOCKET_PATH`
   isolado, cada um com `// Mutation captured: …` executado:
   - as sondas da revisão viram testes: `now-working-seq-moves-id-absent`
     → não `sent` (15); `seq-moves-id-absent-dialog-screen` → não `sent`;
     `enter-into-dialog-after-prompt` → exit 17, zero Enter;
     `end-line-above-last-15`, `viewport-clips-end-line`,
     `wrapped-end-line` → não `sent` pela (b);
     `stale-status-question-after-wait` → exit 17, zero prompt;
   - idle → working com seq novo e id nunca visível → `sent` (a);
   - id no histórico, linha final fora da tela visível, seq parado → `sent`
     (b);
   - mensagem parada (id nas últimas 15, seq parado) → um Enter, depois
     seq muda (preStatus idle) → `sent`.
2. `node --test` e `bun test --timeout 60000` de `send.test.mjs`,
   `setup*.test.mjs`, `parity-setup.test.mjs`, `parity-entry.test.mjs` →
   0 fail (cole). Atualize `SKILL.md`/`docs/guide.md` se o texto das
   provas mudar.
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

- Todo o resto, e qualquer arquivo de outro worktree (inclusive o do seu
  painel). Mandar prompt, teclas ou mensagem a painéis reais.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera. Não rode a
suíte completa.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e as mutações
executadas.
