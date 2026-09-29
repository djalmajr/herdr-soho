# Emenda 1 — P3b: `samePath` no Windows

Role: implementer · Agent: build · Report language: pt-BR

Mesma worktree e branch da P3b; siga sobre o que você já deixou nela.

## Goal

`samePath` trata como o mesmo diretório duas grafias Windows que só
diferem em maiúsculas ou barras.

Seu commit anterior já foi integrado na branch (o item do `collect` como
root ficou coberto pela sonda de leitura que você fez).

## Decisions already made

- Achado P2 da revisão R8 (executado): `samePath`
  (`lib/dispatch.mjs:1422-1424`) compara `path.win32.resolve(a) ===
  path.win32.resolve(b)`; `C:/work/repo` × `c:/work/repo` e
  `C:/Work/Repo` × `c:/work/repo` dão falso, e o dispatch manda os
  relatórios para `$TMPDIR` sem precisar (`dispatch.mjs:929`).
- Correção decidida: `return pathApi.relative(left, right) === '';`
  (`path.win32.relative` já ignora caixa e barras).
- Aceite: o teste de `samePath` ganha os pares `C:`×`c:` e
  `Work/Repo`×`work/repo` (iguais) e `C:\work\repo`×`C:\work\other`
  (diferentes), com `platform: 'win32'`; em POSIX a caixa continua
  distinguindo; mutação para a comparação por `resolve` executada e
  capturada; `node --test` e `bun test` de `dispatch.test.mjs` → 0 fail.
- Arquivos permitidos neste item: `skills/herdr-soho/scripts/lib/dispatch.mjs`
  (só `samePath`) e `skills/herdr-soho/scripts/test/dispatch.test.mjs`
  (só o teste de `samePath`).

## Owned files

- `skills/herdr-soho/scripts/lib/dispatch.mjs` (só `samePath`) e
  `skills/herdr-soho/scripts/test/dispatch.test.mjs` (só o teste de `samePath`)

## Forbidden

- Todo o resto. Nenhum comando git que escreva. No commit, push, tag, or
  PR. The orchestrator owns git.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas.
