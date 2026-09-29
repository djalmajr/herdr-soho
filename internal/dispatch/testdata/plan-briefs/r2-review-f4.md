# Brief — Revisão R2: checkpoints neutros no wait (#3) e idades no status (#4)

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está na branch `fix/wait-status-activity` (commit do topo: a F4).
Revise só `git diff feat/rename-herdr-soho...HEAD` (um commit). Issues:
https://github.com/djalmajr/herdr-soho/issues/3 e /4.

## Goal

Achados ancorados em `arquivo:linha`, com execução, sobre correção do
contrato abaixo, antes do push.

## Contrato decidido (não reabra; verifique que o código cumpre)

- Atividade = mudança real do hash normalizado (contadores e glifos não
  contam) observada pelo `wait` entre duas sondagens; gravada em
  `wait/<agent>.activity-at` só quando um hash anterior não vazio muda; a
  primeira leitura de uma tela não é atividade; leitura vazia (falha) não é
  atividade; o dispatch limpa `activity-at`.
- `wait --timeout` vencido com worker `working` e atividade dentro da
  janela (`stuck_warn_minutes`×60, 20 min quando 0/não numérico): linha
  JSON com `checkpoint: true` e `activity_age_s`, só uma linha em stderr,
  **nenhuma** linha no `friction.log`; parado: `checkpoint:false` e o
  `warn` de hoje (friction). Exit 9 nos dois casos.
- `status`: TSV ganha `task_s` e `activity_s` no fim (`-` quando
  desconhecido); JSON ganha as duas como últimas chaves; somente leitura;
  nenhum texto de tela na saída.

## O que verificar

1. Um caminho em que uma tela estática (ou só lida agora) vira checkpoint
   ativo — é o risco principal. Monte fixtures e rode.
2. `friction.log`: checkpoint ativo não escreve; parado escreve.
3. `status` realmente somente leitura; colunas corretas em todas as formas
   de linha (TSV com e sem `cause`, JSON).
4. Goldens `parity-wait`/`parity-status`: mudaram só pelas colunas/chaves
   novas; o `transform` do harness não mascara outra coisa.

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
evidência colada e correção sugerida.

## Report

Relatório em Markdown no caminho do contrato, pt-BR, com `[done]` /
`[partial]` / `[skipped]` para cada um dos quatro pontos.
