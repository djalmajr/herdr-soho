# Brief — Revisão R9: cópia estável por tarefa (task report)

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está em `fix/task-report-per-task` (um commit sobre o `main`).
Confirme com `git log --oneline -1` e revise só `git diff HEAD~1 HEAD`.

## Goal

Achados ancorados em `arquivo:linha`, com execução, antes do push e do
merge.

## Contrato decidido (verifique que o código cumpre)

- Observado em uso real: um `dispatch` novo (sem `--amend`) para o mesmo
  agente, depois de uma tarefa concluída, reaproveitava o `task_report`
  do ponteiro anterior; a cópia estável da tarefa anterior foi
  sobrescrita pelo relatório da nova.
- Decisão (D8): cada tarefa tem a própria cópia estável. Emenda mantém a
  cópia da tarefa (e acumula `history`); dispatch novo nomeia a cópia a
  partir do seu primeiro relatório, com `history: []`, e a cópia da
  tarefa anterior fica como estava.
- Falha de transporte num dispatch novo restaura ponteiro,
  `last-report-<agent>` e a cópia anterior byte a byte (teste existente).

## O que verificar

1. Dispatch novo × emenda × emenda sem ponteiro (tarefa legada) × falha de
   transporte em cada um.
2. `wait`, `collect`, `status` e `clean` com duas tarefas seguidas do mesmo
   agente: cada um mostra/preserva a cópia certa.
3. SKILL.md, guia e README descrevem a mesma regra (aponte texto que
   contradiga).
4. O teste novo pega a mutação que declara.

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test` só em
`task-report.test.mjs`, `dispatch.test.mjs`, `collect.test.mjs`; sondas
em `/tmp`. Não rode a suíte inteira.

## Forbidden

- Editar qualquer arquivo do checkout. Revisor é somente leitura.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
