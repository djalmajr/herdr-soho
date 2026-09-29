# Brief — Revisão R1d: `setup --dry-run` prevê a renomeação no lugar

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd agora está em `6fe206d` (`Preview the renamed block in setup
--dry-run`), a correção do seu achado P2 da R1c; na rodada anterior ele
ainda estava em `66d5b0f`. Confirme com `git log --oneline -1` e revise só
`git diff HEAD~1 HEAD`.

## Goal

Confirmar que o achado P2 da R1c está resolvido sem regressão, antes do
merge da PR #11.

## Mudança decidida (verifique que o código cumpre)

- `setup --dry-run` roda `setupBlockResult` no alvo (`CLAUDE.local.md` em
  `--local`, senão o alvo canônico): resultado `null` → exit 4 com a mesma
  mensagem de `setupWriteBlock`, stdout vazio, arquivo intocado.
- Arquivo só com marcador legado → a prévia imprime só as linhas do(s)
  bloco(s) renomeado(s) (marcadores incluídos), com o texto próprio.
- Os outros casos imprimem `setupBlock()` como antes (goldens de paridade
  intocados).
- Erros de uso (`--panes`, `--lane`) e as recusas do `--local` continuam
  antes dessa checagem.

## O que verificar

1. As sondas `cli-dry-run-custom` e `cli-dry-run-unterminated` da R1c agora
   batem com a escrita real.
2. `--dry-run --local` com bloco legado em `CLAUDE.local.md`; `--dry-run
   --panes 3` com bloco legado sem fim (nenhuma linha `# would set` antes
   da recusa); bloco novo sem fim: como você mostrou, a escrita não recusa
   (`setupBlockResult` não dá `null`), então o dry-run também sai 0.
3. O teste novo em `legacy-migration.test.mjs` pega as duas mutações que
   ele declara.

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test` em
`skills/herdr-soho/scripts/test/`; sondas em `/tmp` com `HOME` temporário.
Antes de afirmar que algo falha, rode e cite a saída.

## Forbidden

- Editar qualquer arquivo. Revisor é somente leitura.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
