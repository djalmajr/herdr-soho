# Brief — Revisão R1b: delta das correções da R1

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd agora está no commit `1b49cf9` (antes `2360abc`). Revise só o delta
`git diff 2360abc 1b49cf9` — as correções dos seus quatro achados da R1.

## Goal

Confirmar, com execução, que cada achado da R1 foi resolvido e que o delta
não criou regressão.

## O que verificar

1. Achado 1 (`CLAUDE.md` separado com bloco antigo): repita a sua sonda;
   `setup` avisa `setup --target CLAUDE.md` e o `doctor` nomeia o arquivo;
   depois de `setup --target CLAUDE.md`, nenhuma linha legacy de bloco.
   Confira também que o aviso "no herdr-soho block in AGENTS.md/CLAUDE.md"
   continua saindo num projeto sem bloco nenhum com `CLAUDE.md` separado.
2. Achado 2 (config legada em symlink): repita a sonda de usuário e de
   projeto.
3. Achado 3 (`state_dir` no `config`).
4. Achado 4 (texto do README e da SKILL.md).

## Checks you may run

Os mesmos da R1: leitura, `git diff/log/show`, testes com `node --test` e
`bun test` em `skills/herdr-soho/scripts/test/`, `run-tests.sh --env
outside <suite>`, sondas em `/tmp` com `HOME` temporário. Antes de afirmar
que algo falha, rode e cite a saída.

## Forbidden

- Editar qualquer arquivo. Revisor é somente leitura.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um item por achado da R1 (`[done]` resolvido / `[partial]` / aberto)
e qualquer achado novo do delta com `arquivo:linha` e evidência.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
