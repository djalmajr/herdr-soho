# Brief — D1b: a regra do aviso segue a ordem real do Codex

Role: implementer · Agent: build · Report language: pt-BR

Worktree `.worktrees/d1`, branch `fix/codex-env-diagnostic`, commit
`4ac7993`. Caminhos relativos à raiz dela. Brief anterior:
`/work/herdr-soho/.herdr-soho/w14/plan-briefs/d1-codex-env-diagnostic.md`.
Revisão que motiva esta fatia (leia os sete achados e as provas):
`/tmp/herdr-soho/w14/reports/soho-grok-r9-20260928T155123.md`.

## Goal

O aviso do `doctor` sai exatamente quando o Codex 0.158 não passaria as
`HERDR_*` que a skill usa, e a escolha do painel só usa pids ancestrais.

## Decisions already made

1. **Ordem do Codex (achados 1 e 2).** O avaliador simula, para cada nome
   de `HERDR_ENV`, `HERDR_PANE_ID` e `HERDR_WORKSPACE_ID`: conjunto inicial
   por `inherit` (`"all"` ou ausente → presente; `"core"`/`"none"` →
   ausente) → `exclude` remove o que casa → as chaves de `set` recolocam
   (só os nomes) → `include_only`, se não vazio, remove o que não casa.
   Avisa se algum dos três fica ausente. Motivo curto no aviso, conforme o
   passo que tirou o primeiro nome ausente: `inherit="core"`,
   `inherit="none"`, `exclude matches <NOME>`,
   `include_only does not match <NOME>`. `include_only` nunca traz de volta
   o que `inherit` não passou.
2. **`set`.** Leia só os nomes das chaves de `set`, tanto da subtabela
   `[shell_environment_policy.set]` quanto da forma em linha
   `set = { A = "1", B = "2" }`. Nunca guarde nem imprima valores.
3. **Chave repetida (achado 6).** Chave repetida na seção (ou na
   subtabela `set`) torna o parse nulo: sem aviso, sem eco.
4. **Glob (achado 5).** `*` = qualquer sequência, `?` = um caractere, o
   resto literal (escape tudo que a regex trataria como especial), sem
   diferenciar maiúsculas.
5. **Painel (achado 3).** O conjunto comparado com
   `foreground_processes[].pid` tem só os pids ancestrais; não acrescente
   `process.pid` nem `opts.pid`.
6. **Testes existentes (achado 4).** Todo teste que confere a mensagem
   exata de "fora do Herdr" (`herdr.test.mjs` e os outros que o `grep`
   achar) roda com `PATH` sem `ps` nem `herdr` reais (ou ancestrais
   injetados vazios), mantendo a asserção exata antiga.
7. **Docs (achado 7).** A lista do guia passa a
   `["HOME", "LANG", "LOGNAME", "PATH", "SHELL", "USER", "TMPDIR", "HERDR_*"]`,
   mantendo a nota das chaves de `set` e do reinício.

## Acceptance criteria

1. Testes, cada um com `// Mutation captured: …` executado: `core` +
   `include_only = ["HERDR_*"]` → avisa (`inherit="core"`); `none` + `set`
   com `HERDR_ENV`, `HERDR_PANE_ID` e `HERDR_WORKSPACE_ID` → não avisa;
   `all` + `exclude = ["HERDR_*"]` + `set` com os três → não avisa; `all` +
   `include_only = ["PATH"]` + `set HERDR_ENV` → avisa (`include_only does
   not match HERDR_ENV`); `exclude = ["HERDR_EN?"]` → avisa; chave
   repetida → sem aviso; valores de `set` nunca na saída; o pid do
   processo atual sozinho num painel não o nomeia.
2. `node --test` e `bun test --timeout 60000` de `codex-env.test.mjs`,
   `doctor.test.mjs`, `parity-doctor.test.mjs`, `herdr.test.mjs` e dos
   outros arquivos que o item 6 tocar → 0 fail (cole). Prove o item 6
   rodando `herdr.test.mjs` com um `ps` falso que responde `codex` no
   `PATH` do processo de teste: continua verde.

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `skills/herdr-soho/scripts/lib/codex-env.mjs`,
  `skills/herdr-soho/scripts/test/codex-env.test.mjs`, os testes do
  item 6, `docs/guide.md` (só a seção Codex)

## Forbidden

- Todo o resto. Editar `~/.codex/config.toml`. Chamadas ao Herdr real que
  escrevam; `herdr pane current`.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera;
`codex sandbox -- /usr/bin/printenv` com `CODEX_HOME` temporário para
conferir a ordem. Não rode a suíte completa.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e as mutações
executadas.
