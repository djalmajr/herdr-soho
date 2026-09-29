# Brief — B2: um só estado por repositório, e brief sem caminho invisível ao worker

Role: implementer · Agent: build · Report language: pt-BR

Worktree `/work/herdr-soho/.worktrees/build-3`,
branch `fix/state-across-worktrees`, base `main` `e7667e6`. Caminhos
relativos a essa raiz. Código relevante: `projectRoot`
(`skills/herdr-soho/scripts/lib/platform.mjs`), `stateRoot`/`stateDirPath`/
`stateDir` (`lib/state.mjs`, `lib/config.mjs`), o lint do `dispatch`
(`lib/dispatch.mjs`).

## Goal

Qualquer comando do `herdr-soho` rodado de dentro de um worktree git do
projeto usa o mesmo estado (roster, briefs, relatórios, friction) do
checkout principal. O `dispatch` avisa quando o brief cita um caminho
relativo que o worker, no cwd dele, não enxerga.

## Casos reais que motivam

- Neste repositório, um `herdr-soho wait build …` rodado com cwd dentro
  de `.worktrees/d1` resolveu o estado em `.worktrees/d1/.herdr-soho/w14`
  (roster vazio) e saiu 3 `agent 'build' is not in the roster`.
- No projeto Cinzel, um brief de emenda para um worker em worktree citava
  `.herdr-agents/w7/reports/<revisão>.md`. O arquivo só existia no
  checkout principal (o estado é git-ignored), e o worker não conseguiu
  ler a revisão.

## Decisions already made

1. **Raiz do estado.** Uma função nova dá a raiz do projeto para o
   estado: se o cwd está num worktree git **ligado** (linked worktree), é a
   raiz do worktree principal (derivada de `git rev-parse
   --git-common-dir`, ou do primeiro item de `git worktree list
   --porcelain`); senão, o `projectRoot` de hoje. `stateRoot` (e com ele a
   resolução do `state_dir` relativo e a entrada do `.gitignore`) passa a
   usá-la. O resto do código que usa `projectRoot` (leitura de config,
   `setup`, comparação de cwd do worker no `dispatch`) **não muda** nesta
   fatia. Fora de git, nada muda.
2. **Caminho invisível no brief.** No `dispatch` (e `--amend`), para cada
   caminho relativo citado no brief (em código inline ou como token que
   parece caminho), quando o cwd do worker difere da raiz do projeto:
   se o caminho existe sob a raiz do projeto do orquestrador e não existe
   sob o cwd do worker, aviso (sem bloquear, sem reescrever o brief):
   `brief <arquivo> line <n> cites '<caminho>', which the worker in <cwd> cannot see; use the absolute path <absoluto>`.
   Com `brief_lint=strict`, o mesmo texto vira erro (exit 2, nada
   enviado), como os outros avisos de lint no modo estrito.
3. **Docs.** `SKILL.md` e `docs/guide.md`: uma frase dizendo que os
   worktrees de um repositório compartilham o estado do checkout principal
   e que briefs para worker em worktree citam caminhos absolutos (o aviso
   pega os relativos).

## Acceptance criteria

1. Testes com repositório git temporário e um worktree ligado, cada um com
   `// Mutation captured: …` executado:
   - `roster`/`wait`/`status` rodados com cwd dentro do worktree leem o
     `agents.tsv` do checkout principal (o agente registrado lá é achado);
     nenhum `.herdr-soho` é criado dentro do worktree;
   - fora de git e no checkout principal o caminho do estado não muda (os
     testes existentes seguem verdes);
   - brief para worker com cwd no worktree citando
     `.herdr-soho/w1/reports/x.md` (que só existe no principal) → o aviso
     exato; o mesmo com `brief_lint=strict` → exit 2 e nenhum `agent
     prompt`; caminho que existe nos dois → sem aviso; worker com cwd na
     raiz → sem aviso.
2. `node --test` e `bun test --timeout 60000` dos testes novos e dos que
   cobrem `state`, `config`, `dispatch` (lint), `roster`, `wait`,
   `status` → 0 fail (cole). Goldens que mudarem: diff decodificado e o
   motivo.

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `skills/herdr-soho/scripts/lib/platform.mjs` (só a função nova),
  `skills/herdr-soho/scripts/lib/state.mjs` e `lib/config.mjs` (só a
  resolução da raiz do estado), `lib/dispatch.mjs` (só o aviso da decisão
  2), testes novos em `skills/herdr-soho/scripts/test/`, goldens que
  mudarem por isso
- `skills/herdr-soho/SKILL.md`, `docs/guide.md` (só a frase do item 3)

## Forbidden

- Todo o resto e qualquer outro worktree. Nenhuma chamada ao Herdr real
  que escreva: `herdr` falso no `PATH` e `HERDR_SOCKET_PATH` apontando
  para um caminho inexistente.
- Nenhum comando git que escreva **neste** repositório (nos repositórios
  temporários dos testes, pode). No commit, push, tag, or PR. The
  orchestrator owns git.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera. Não rode a
suíte completa.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e as mutações
executadas.
