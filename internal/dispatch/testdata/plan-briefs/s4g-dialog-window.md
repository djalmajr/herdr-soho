# Emenda — S4g: diálogo já respondido não bloqueia o `send`

Role: implementer · Agent: build · Report language: pt-BR

## Goal

Um diálogo de confiança que já foi respondido e ficou desenhado na tela
(o Cursor deixa o quadro acima da barra de prompt) não faz o `send`
esperar nem recusar; um diálogo aberto continua bloqueando.

Mesmo worktree (`/work/herdr-soho/.worktrees/s4`),
branch `feat/peer-send`, agora no commit `9dd1c13` (o seu trabalho foi
commitado e aprovado pela revisão).

## Caso real (validação no Herdr real, 2026-09-28 18:06Z)

Cursor Agent com o diálogo de confiança já aceito, status `idle`. A tela
visível inteira está em
`/tmp/hs-screens/cursor-trust-answered.txt` (26 linhas). Contando de
baixo para cima só as linhas não vazias, `[a] Trust this workspace` é a
14ª e `Do you trust the contents of this directory?` a 19ª: estão dentro
das últimas 20, então `isDialogScreen` casa e o `send` espera o
`--timeout` (10 min) e sai 17, com o agente pronto.

Diálogos abertos observados no mesmo teste (último trecho da tela):
- Cursor aberto: `│  ▶ [a] Trust this workspace │`, `│    [q] Quit │`,
  `│ │`, `│  Use arrow keys to navigate, Enter to select, or press the key shown │`,
  `│ │`, `╰───…───╯` (o padrão é a 6ª linha não vazia de baixo).
- Claude aberto: `❯ No, exit`, `  Yes, I trust this folder`,
  `Enter to confirm · Esc to cancel` (últimas 3).
- Codex aberto: `Trust this folder? Codex can read, edit, and run files here, subject to your permission`,
  `settings. Folder settings can run code automatically, even without a model request. Continue`,
  `only if you trust these files. Your trust decision will be saved.`,
  `› 1. Trust and continue`, `  2. Back to Agent Command Center`,
  `  enter continue · esc back` (o padrão é a 6ª de baixo).

## Decisions already made

1. Os padrões de confiança/confirmação (`Trust this workspace`,
   `trust this folder`, `Do you trust`, `Enter to confirm`, `[y/N]`,
   `(y/n)`) passam a ser testados só nas **últimas 10** linhas não vazias
   da tela visível. Os marcadores de `dialog.mjs` (só com `blocked`)
   continuam nas últimas 20.
2. Documente o novo limite onde `SKILL.md`/`docs/guide.md` citam as 20
   linhas para esses padrões.

## Expected result

- Testes com as telas acima como fixture (a do Cursor respondido lida do
  arquivo copiado para o diretório de fixtures do teste, não de `/tmp`):
  Cursor respondido + `idle` → envia; Cursor, Claude e Codex abertos →
  exit 17 sem prompt. Cada um com `// Mutation captured: …` executado
  (voltar para 20 linhas faz o do Cursor respondido falhar).
- `node --test` e `bun test --timeout 60000` de `send.test.mjs` → 0 fail
  (cole).

## Owned files

`skills/herdr-soho/scripts/lib/peer.mjs`, `skills/herdr-soho/scripts/test/send.test.mjs`
(e um arquivo de fixture novo ao lado dele), `SKILL.md`/`docs/guide.md`
só na frase do limite.

## Forbidden

- Todo o resto e qualquer outro worktree. Nenhum `send` fora dos testes;
  nada ao Herdr real.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e as mutações.
