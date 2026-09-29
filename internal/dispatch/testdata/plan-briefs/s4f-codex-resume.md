# Brief — S4f (retomada no Codex): fechar mutações e relatório

Role: implementer · Agent: build · Report language: pt-BR

## Goal

Fechar a fatia S4f, que já tem o código pronto e as suítes verdes, e
entregar o relatório.

Worktree `/work/herdr-soho/.worktrees/s4`,
branch `feat/peer-send`, commit `fb9365b` + diff não commitado (5
arquivos). Caminhos relativos a essa raiz.

Leia, nesta ordem:
1. O contrato (decisões, critérios, arquivos): `/work/herdr-soho/.herdr-soho/w14/plan-briefs/s4f-proof-tighten.md`
2. A revisão que motivou a fatia: `/tmp/herdr-soho/w14/reports/soho-grok-r11-20260928T162634.md`
3. A passagem de bastão do implementador anterior (o que está feito, o que falta, armadilhas): `/work/herdr-soho/.herdr-soho/w14/plan-briefs/handoff-s4f.md`

## Expected result

- Confira o `git diff` contra cada decisão do contrato e corrija por
  edição o que divergir. Nunca use `git checkout`/`git restore`.
- Refaça as mutações que a passagem lista como não executadas ou mal
  selecionadas (`working-seq`, `proof-b-disabled`,
  `input-id-normalization`, normalização), em cópia fora do worktree
  checada com `herdr-soho mutation-guard`. Cada `// Mutation captured`
  do arquivo de teste precisa ter a saída vermelha colada no relatório.
- `node --test` e `bun test --timeout 60000` de `send.test.mjs`,
  `setup*.test.mjs`, `parity-setup.test.mjs`, `parity-entry.test.mjs` → 0
  fail (cole).
- O relatório vai no caminho do **seu** contrato (o do dispatch), não no
  caminho antigo citado na passagem.

## Owned files

Os do contrato: `skills/herdr-soho/scripts/lib/peer.mjs`,
`skills/herdr-soho/scripts/lib/commands/send.mjs`,
`skills/herdr-soho/scripts/test/send.test.mjs`,
`skills/herdr-soho/SKILL.md` e `docs/guide.md` (só o `send`).

## Forbidden

- Todo o resto e qualquer outro worktree. Mandar prompt, teclas ou
  mensagem a painéis reais; `send` fora dos testes com `herdr` falso,
  ids impossíveis e `HERDR_SOCKET_PATH` isolado.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Report

Relatório em Markdown no caminho do contrato, por decisão e critério
`[done]` / `[partial]` / `[skipped]` + motivo, com saídas coladas e as
mutações executadas; diga o que mudou em relação ao diff herdado.
