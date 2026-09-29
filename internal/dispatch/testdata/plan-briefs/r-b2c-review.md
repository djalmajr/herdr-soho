# Revisão R-B2c — raiz do estado sem `.git` e cache das raízes por processo

Role: reviewer · Agent: review · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

Revisar, antes do push, o último commit do branch que faz os worktrees ligados compartilharem o
estado do checkout principal. O commit fecha um P2 de revisão anterior e acrescenta um cache por
processo das raízes, para cortar de 21–29 para 7 os processos `git` de um `roster`. O risco
principal de um cache é devolver uma raiz velha.

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-b2c`, branch
  `fix/state-across-worktrees`, commit `4461a88` (pai `00d1ca2`). Diff: `git -C <worktree> show 4461a88`.
- Brief: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/b2c-fix-sonnet.md`.
- Relatório do implementador:
  `/tmp/herdr-soho/w14/reports/build-3-20260928T192656.md`.
- Revisão anterior, com as sondas: `/tmp/herdr-soho/w14/reports/soho-grok-r12-20260928T184916.md`.

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. O achado anterior fecha: `core.worktree` que resolve para `super/.git` (relativo e absoluto) cai
   no `projectRoot`; segmentos `.git` em caminhos POSIX e Win32; `.gitmodules` não conta como
   `.git`. Rode as sondas.
2. O cache: a chave (cwd resolvido, `GIT_DIR`, `GIT_WORK_TREE`, `GIT_COMMON_DIR`) cobre tudo que muda
   a resposta do `git` dentro de um processo? Procure no código comandos que, no mesmo processo,
   mudam a topologia do repositório (criam repositório, worktree, `.gitignore`) e depois voltam a
   resolver a raiz — `setup`, `init`, `doctor --fix`, `session set`, `spawn --cwd` — e diga se algum
   pode ler uma raiz velha. Um `cwd` relativo e um `cwd` com symlink geram a mesma chave?
3. A contagem de processos: rode o teste que conta e confira ≤ 8 no checkout principal e no worktree
   ligado.
4. `node --test` e `bun test --timeout 60000` de `worktree-state.test.mjs`, `state.test.mjs`,
   `init.test.mjs` → cole os resumos.

Nenhum Herdr real: `HERDR_SOCKET_PATH=/tmp/hs-go/review/none.sock`.

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real que escreva. Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
