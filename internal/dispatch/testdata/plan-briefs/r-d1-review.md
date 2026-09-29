# Brief — Revisão R-D1: diagnóstico de Codex sem `HERDR_*`

Role: reviewer · Agent: review · Report language: pt-BR

## Goal

Dizer se o commit `4ac7993` (branch `fix/codex-env-diagnostic`, sobre
`main` `0573d1e`) pode ir para a `main`: o diagnóstico está certo, não
identifica o painel errado e não vaza linha de comando de processo.

Seu cwd está em `4ac7993`. Confirme com `git log --oneline -1` e revise
`git diff HEAD~1 HEAD`. Brief (o contrato):
`/work/herdr-soho/.herdr-soho/w14/plan-briefs/d1-codex-env-diagnostic.md`.
Relatório do implementador:
`/tmp/herdr-soho/w14/reports/soho-agy-d1-20260928T153115.md`.

## O que verificar com atenção

1. **Identidade.** A mensagem só nomeia um painel quando exatamente um
   painel de Codex tem, entre os `foreground_processes`, um pid ancestral
   do processo atual. Algum caminho usa foco, cwd, `pane current`, ou
   aceita um casamento fraco (pid reciclado, o próprio `process.pid`
   adicionado ao conjunto, pane remoto)? O `pid` do falso e do real têm o
   mesmo tipo (número vs string)?
2. **Sigilo.** Nenhum `argv`/`cmdline`/`argv0` de `process-info` ou `ps`
   chega a stdout, stderr, friction ou arquivo. O leitor do TOML nunca
   imprime outra seção do `config.toml` (pode ter tokens).
3. **Parser TOML** da seção `[shell_environment_policy]`: aspas simples e
   duplas, comentário em linha, array em várias linhas, subtabela
   `[shell_environment_policy.set]` (não pode ser lida como a seção),
   chave repetida, seção ausente. A regra do aviso casa com o que o Codex
   faz (`inherit` core/none/all, `include_only` depois de `set`,
   `exclude`, globs sem diferenciar maiúsculas).
4. **Custo e efeitos.** `requireEnv` roda em quase todo comando: fora do
   Herdr e sem Codex, quantos `ps` ele chama e quanto tempo soma? Algum
   caminho chama o Herdr sem ancestral `codex`? No Windows, nada muda?
5. **Testes** existentes enfraquecidos (`doctor*`, `herdr*`)? Os testes
   novos pegam as mutações que declaram (rode ao menos três em cópias em
   `/tmp`)?
6. Docs: a correção no `docs/guide.md` passa exatamente as `HERDR_*` além
   do que `core` passa, e avisa das chaves de `set`?

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test --timeout 60000` em
`codex-env.test.mjs`, `doctor.test.mjs`, `parity-doctor.test.mjs`,
`herdr.test.mjs`; mutações em cópias em `/tmp`. Leitura real só de
consulta é permitida: `herdr api snapshot`, `herdr pane process-info`
(sem imprimir `argv`/`cmdline`), `codex sandbox -- /usr/bin/printenv`.

## Forbidden

- Editar qualquer arquivo do checkout ou `~/.codex/config.toml`. Revisor é
  somente leitura.
- Nenhuma chamada ao Herdr real que escreva (`agent prompt`, `send-keys`,
  painéis, `send`, `dispatch`) e nenhum `herdr pane current`.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
