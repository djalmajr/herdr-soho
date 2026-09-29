# Brief — Revisão R-SR: integração de referências, seletor e `send`

Role: reviewer · Agent: review · Report language: pt-BR

## Goal

Dizer se o branch de integração `feat/session-refs` (HEAD `a7e2158`) pode
ir à `main`: `find` (referências de sessão), seletor (plugin `pick`) e
`send` (mensagens entre sessões) juntos, sobre a `main` atual.

Seu cwd está em `a7e2158`. Confirme com `git log --oneline -1`. O diff
total é `git diff main...HEAD` (27 arquivos). Cada fatia já foi revisada
isoladamente; **ainda não foram revisados**:
- `88a19de` (orquestrador): linha de ajuda do `find` e o teste de
  `foreground_cwd` sobre `cwd`;
- `73f0f62` (orquestrador): a varredura do arquivo de teste do `send` por
  ids de painel reais;
- `ccdfe6e` (S3e): o seletor lista cada painel uma vez só (dedupe);
- os três merges (`e07bc78`, `85c3453`, `a7e2158`) e a resolução do
  conflito do golden `parity-config.json` (deve diferir da `main` só pela
  linha `inbound auto defaults`).

Plano e decisões: `/work/herdr-soho/.agents/plans/2026-09-28-herdr-soho-refs-e-mensagens.md`.

## O que verificar com atenção

1. Os quatro itens não revisados acima (use `git show <commit>`).
2. Coerência entre as fatias: o formato de referência `[máquina/]<pane>`
   (`lib/sessionref.mjs`) é o mesmo em `find`, no texto que o seletor
   copia e no alvo do `send`; o cabeçalho do `send` cita a referência do
   remetente no mesmo formato; ajuda (`usage.mjs`), `SKILL.md` e
   `docs/guide.md` descrevem os três de forma consistente (códigos de
   saída 0/2/4/15/17/18, `inbound=auto|off`).
3. O merge com a `main` não desfez nada das fatias da `main` (#23
   dispatch arrival, #24 diagnóstico de Codex) nem vice-versa: compare
   `git diff main...HEAD` com a soma esperada das fatias.
4. Segurança do `send`: nada no caminho padrão manda texto a um painel sem
   passar por `inbound`, pela checagem de diálogo e pelo
   `literalPeerText`; nenhum teste fala com o Herdr real.

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test --timeout 60000` em
`find.test.mjs`, `send.test.mjs`, `plugin/test/`, `parity-config.test.mjs`,
`parity-entry.test.mjs`; mutações em cópias em `/tmp`.

## Forbidden

- Editar qualquer arquivo do checkout. Revisor é somente leitura.
- **Nenhuma execução de `send` ou do seletor fora dos testes**, e nenhuma
  chamada ao Herdr real que escreva: `herdr` falso no `PATH` e
  `HERDR_SOCKET_PATH` inexistente, ids impossíveis.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
