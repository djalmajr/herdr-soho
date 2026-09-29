# Emenda 1 — S3: achados da revisão do seletor

Role: implementer · Agent: build · Report language: pt-BR

Worktree `.worktrees/s3`, branch `feat/session-picker`, commit `733d50d`
(o trabalho anterior, já commitado). Caminhos relativos à raiz dela. Brief
original: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/s3-picker.md`.
Revisão (leia inteira, com as sondas):
`/tmp/herdr-soho/w14/reports/soho-grok-r3-20260928T115559.md`.

## Goal

Corrigir os sete achados da revisão, com teste para cada um.

## Decisions already made

1. **P1 — redesenho.** Ao fim de cada `redraw`, apague o resto da tela
   (`\x1b[J`), para que um quadro menor não deixe linhas do anterior
   (contagem antiga, "carregando", seleção antiga).
2. **P1 — texto de terceiros.** Uma função única que remove C0 (exceto
   nenhum: aqui nem `\n` nem `\t` ficam — troque `\t` e `\n` por espaço),
   C1 (U+0080–U+009F), DEL e sequências ESC completas (CSI, OSC até BEL ou
   ST) de cada campo antes de desenhar (`entryLine`, linha de falha com o
   `cause`) e antes de montar o texto copiado (`copyPayload`). Teste com os
   campos da sonda da revisão (CSI, OSC 52, quebra de linha no cwd): tela e
   texto copiado sem ESC e sem quebra.
3. **P1 — SIGTERM.** O seletor trata `SIGTERM` (e `SIGHUP`) passando pelo
   mesmo `finish` de Esc/Ctrl-C: restaura o modo do terminal, mata os
   `find` em voo e sai sem copiar. Cada `find` ganha timeout (60 s por
   máquina); um que estoura vira a linha de falha dessa máquina. Teste que
   manda `SIGTERM` ao seletor com um `find` falso pendurado e prova que o
   filho morreu e que o modo raw foi desligado.
4. **P2 — primeiro quadro.** Marque o carregamento local como em voo
   antes do primeiro desenho: a tela inicial diz "carregando local…", não
   "nenhum pane".
5. **P2 — UTF-8 entre leituras.** Use um `StringDecoder('utf8')` (ou
   `setEncoding('utf8')` no stdin) para não partir caracteres entre
   chunks; teste com `é` cortado entre dois chunks.
6. **P2 — PowerShell.** O comando passa a ler o stdin de verdade:
   `powershell -NoProfile -Command "[Console]::InputEncoding=[Text.Encoding]::UTF8; Set-Clipboard -Value ([Console]::In.ReadToEnd())"`
   com o texto no stdin (nunca na linha de comando). Teste no fake com o
   argv exato e um texto com aspas, `$` e acento.
7. **P2 — teste de palavras.** O teste do filtro por palavras passa a
   usar palavras que **não** são vizinhas no texto de busca (ex.: nome e
   cwd, ou kind e status fora da ordem), de modo que casar a consulta
   inteira com `includes` falhe. Execute essa mutação.

## Acceptance criteria

1. Teste para cada item, com `// Mutation captured: …` executado.
2. `node --test plugin/test/` e `bun test --timeout 60000 plugin/test/` →
   0 fail (cole).
3. Nada mais muda (manifesto, ação `pick`, formato copiado, teclas).

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `plugin/picker.mjs`, `plugin/clipboard.mjs`, `plugin/test/picker.test.mjs`
- `README.md`, `docs/guide.md` (só se o texto do seletor mudar)

## Forbidden

- Todo o resto. Abrir painéis de verdade, ligar o plugin, mandar teclas a
  panes.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e as mutações
executadas.
