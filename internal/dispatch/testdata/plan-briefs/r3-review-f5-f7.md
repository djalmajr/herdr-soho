# Brief — Revisão R3: lint prévio e matriz de falhas (#9, #2) e mutation-guard (#7)

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está no commit `fa143ad` (branch `fix/mutation-guard`). Revise
`git diff de934ac fa143ad` — dois commits: `7af5156` (#9 e #2) e `fa143ad`
(#7). Issues: https://github.com/djalmajr/herdr-soho/issues/9, /2 e /7.

## Goal

Achados ancorados em `arquivo:linha`, com execução, sobre correção e
segurança, antes do push.

## Contrato decidido (verifique que o código cumpre)

- `herdr-soho lint <brief.md> [--role <role>]`: mesmos avisos do dispatch
  (a mesma função alimenta os dois), sem dispatch, estado ou friction;
  exit 0 limpo, 1 com avisos, 2 strict com seções faltando (mesma mensagem
  do dispatch), 3 papel desconhecido; `brief_lint=off` → `lint off`, 0.
  O dispatch continua byte a byte igual ao de antes para briefs sem a
  seção nova.
- Seção opcional `Failure matrix` (cabeçalho nível 1–3 que começa com
  `Failure matrix`, até o próximo de nível igual ou maior) com os
  marcadores `[crash]`, `[retry]`, `[clock]`; faltando algum, o aviso
  `brief <path> failure matrix is missing: … — …`; sem a seção, nenhum aviso.
  Regras condicionais em implementer/reviewer; checkpoint depois dos gates
  em implementer/tasker/designer; template e checklist.
- `herdr-soho mutation-guard <copy> [--source <dir>] [--env NAME]…`:
  checagens `copy-outside-source`, `no-symlink-into-source` (sem seguir
  links, sem entrar em `.git`), `build-env` (`CARGO_TARGET_DIR`,
  `CARGO_BUILD_TARGET_DIR`, `--env`; sem imprimir valor), `cargo-config`;
  exit 0/1/2; não escreve; falha fechado em caminho que não resolve.

## O que verificar

1. Divergência entre o `lint` e o dispatch no mesmo brief (texto e ordem).
2. Falso positivo/negativo da matriz (cabeçalho em nível 4, marcador fora da
   seção, seção seguida de subcabeçalho).
3. `mutation-guard`: um caso de artefato compartilhado que passa (symlink
   aninhado, caminho relativo em `target-dir`, cópia via symlink para a
   fonte, `--source` relativo), e qualquer escrita ou vazamento de valor.
4. Textos novos de papéis, template e SKILL.md descrevendo algo que o
   código não faz.

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test` em
`skills/herdr-soho/scripts/test/`; `run-tests.sh --env outside <suite>`;
sondas em `/tmp` com `HOME` temporário. Antes de afirmar que algo falha,
rode e cite a saída.

## Forbidden

- Editar qualquer arquivo. Revisor é somente leitura.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.
- Imprimir variáveis de ambiente ou linhas de comando de processos.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
