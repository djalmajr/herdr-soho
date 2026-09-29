# Emenda 1 — P3a: state dir relativo reconhecido depois de `path.resolve`

Role: implementer · Agent: build · Report language: pt-BR

Mesma worktree e branch da P3a; siga sobre o que você já deixou nela.

## Goal

No Windows, um state dir relativo sob a raiz do projeto volta a ser
reconhecido como interno (entra no `.gitignore` e aparece no plano), com
qualquer grafia da raiz (`C:/repo`, `C:\repo`, `c:/repo`).

## Decisions already made

- Achado P1 da revisão R8 (executado): `stateRootPath`
  (`lib/config.mjs:176`) passou a `path.resolve(root, d)`, que no Windows
  usa `\`. `stateRoot` (`lib/config.mjs:206-208`) e o ramo canônico de
  `setup --plan` (`lib/commands/setup-plan.mjs:486-488`) ainda testam
  `d.startsWith(root + '/')`; `projectRoot` devolve o texto cru do
  `git rev-parse --show-toplevel`:
  ```
  {"root":"C:/repo","rel":".herdr-soho","d":"C:\\repo\\.herdr-soho","prefix":"C:/repo/","newStarts":false}
  {"root":"C:\\repo","rel":".herdr-soho","d":"C:\\repo\\.herdr-soho","prefix":"C:\\repo/","newStarts":false}
  ```
- Correção: um helper único em `lib/config.mjs` (ex.:
  `relativeInside(root, d, platform = process.platform)`) que usa
  `path.win32` no Windows e `path.posix` no resto, calcula
  `relative(root, d)` e só considera interno quando o relativo não é
  vazio, não é absoluto e não começa por `..`; devolve o relativo com `/`
  (a forma que vai no `.gitignore`). `stateRoot` e `setup-plan.mjs` usam
  esse helper; nenhum outro `startsWith(root + '/')` do mesmo propósito
  fica para trás (procure e liste).
- O que a linha do `.gitignore` e as mensagens mostram em macOS/Linux não
  muda (goldens intactos).

## Acceptance criteria

1. Teste unitário do helper com `platform: 'win32'` para `C:/repo`,
   `C:\repo`, `c:/repo` × `C:\repo\.herdr-soho` (interno, `.herdr-soho`),
   `C:\other` (fora), `C:\repo` (vazio → fora), e com `posix` para os
   casos atuais; mutação `startsWith(root + '/')` executada e capturada.
2. `node --test` e `bun test` de `config.test.mjs` (ou onde o helper for
   testado), `setup-plan.test.mjs`, `setup-local.test.mjs`,
   `parity-setup.test.mjs` → 0 fail (cole).

## Owned files

- `skills/herdr-soho/scripts/lib/config.mjs`,
  `skills/herdr-soho/scripts/lib/commands/setup-plan.mjs`
- o arquivo de teste onde o helper for testado (novo ou `config.test.mjs`)
- os seus arquivos da P3a

## Forbidden

- `lib/dispatch.mjs` (outra emenda). Goldens. Nenhum comando git que
  escreva. No commit, push, tag, or PR. The orchestrator owns git.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas.
