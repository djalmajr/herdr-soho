# Revisão R-DOCS — reaproveitar painéis e compactar/limpar sessões (PR #25)

Role: reviewer · Agent: review-2 · Report language: pt-BR

## Goal

Revisar o PR #25 antes do merge. É só texto (`skills/herdr-soho/SKILL.md` e `docs/guide.md`),
escrito pelo orquestrador, que é Claude (a mesma família que você): diga no relatório que a revisão é
da mesma família e seja especialmente cético.

O texto troca o conselho antigo ("depois de vários briefs, feche o worker e abra outro com
`--fresh`") pela regra do usuário: reaproveitar sempre o painel; mesma fatia ou assunto → reaproveitar
como está (`--amend` para correções); outro assunto no mesmo projeto → `/compact` com o worker
ocioso, esperar terminar, depois o brief; assunto sem relação → limpar (`/new` no Codex, `/clear` no
Claude Code); `release --close` só quando a sessão não vai mais usar o worker.

- Worktree: `/work/herdr-soho/.worktrees/docs-reuse`, branch
  `docs/reuse-sessions`. Diff: `git -C <worktree> diff main...HEAD`.

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`.

1. O texto novo contradiz alguma outra parte de `SKILL.md`, `docs/guide.md`, `README.md`,
   `references/*.md` ou `roles/*.md` (procure `--fresh`, `release`, `reuse`, `growing session`)?
   Liste cada contradição com arquivo:linha.
2. Os comandos citados existem e fazem o que o texto diz: `/compact` e `/new` no Codex, `/compact` e
   `/clear` no Claude Code, `/compact` no pi (verifique pela ajuda das CLIs instaladas, sem abrir
   sessão interativa: `codex --help`, `claude --help`, `pi --help` ou a documentação local; o que não
   der para verificar, marque `[partial]` com o motivo). `herdr agent prompt <name> "/compact"`
   entrega um comando de barra à CLI?
3. O inglês segue o estilo do resto de `SKILL.md` (frases curtas, sem jargão novo).

## Forbidden

- Editar qualquer arquivo. Herdr real que escreva (não mande nada a nenhum painel). Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item,
`[done]` / `[partial]` / `[skipped]` + motivo com a prova.
