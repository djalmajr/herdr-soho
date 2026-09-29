# Brief — P3b: suíte da skill no Windows (resto, parte b)

Role: implementer · Agent: build · Report language: pt-BR

Worktree própria, branch `fix/test-portability-win-b` (base:
`fix/test-portability` `08324e6`, já com o `main` atual e as fatias
P1/P2a/P2b). Caminhos relativos à raiz dela. Issue:
https://github.com/djalmajr/herdr-soho/issues/17.

## Goal

Os testes dos seus arquivos passam no Windows (Node 24) sem deixar de
passar no macOS/Linux; bug real do CLI no Windows é corrigido com teste.

## Decisions already made

- Falhas reais do Windows no commit `08324e6` (só os blocos `not ok` dos
  seus arquivos, 34):
  `/work/herdr-soho/.herdr-soho/w14/windows-fails-p3b.tap`.
  É a lista a zerar. TAP completo:
  `/work/herdr-soho/.herdr-soho/w14/windows-port-08324e6.tap`.
- **Nomes de arquivo:** o Windows não aceita `"`, `<`, `>`, `|`, `?`, `*`
  nem `:` em nomes (`mutation-guard.test.mjs` cria `ha mutation "guard"`).
  No Windows, use só o que ele aceita (espaço, `'`) e mantenha o que o
  teste prova (quoting do caminho); em POSIX o nome atual continua.
- **Caminhos em JSON esperado:** `JSON.stringify(caminho)` ou comparar o
  objeto parseado.
- **`PATH`:** `path.delimiter`; fixtures com `USERPROFILE` junto de `HOME`
  e `COMSPEC`/`PATHEXT` preservados quando o `PATH` é restrito.
- **Ferramentas no `bin/` de teste:** `linkTool` de `test/tools.mjs` (já
  existe); fakes de CLI por `writeFakeCli` de `test/fakes.mjs`. Teste cujo
  objeto é o próprio symlink pula só com `canSymlink` falso.
- **Bits de modo POSIX e `chmod 000`:** a asserção vale só onde a
  plataforma os aplica (probe real de leitura/escrita, como já feito em
  `stats.test.mjs` e `wait.test.mjs`); o resto do teste continua rodando.
- **Bug do produto:** se o CLI gera no Windows caminho/nome diferente do
  POSIX ou falha por separador, corrija em `skills/herdr-soho/scripts/lib/`
  com teste que falha sem a correção; liste como "bug do produto".
- Você não roda Windows: o orquestrador roda depois da integração.

## Expected result

Diff nos seus arquivos, com uma tabela no relatório: teste do TAP → causa
→ mudança (teste ou produto).

## Acceptance criteria

1. `node --test` e `bun test` dos seus arquivos no macOS → 0 fail (cole).
2. Cada bug do produto com um teste que falha sem a correção (mutação
   executada, `// Mutation captured: …`).
3. Nenhum teste enfraquecido: asserção removida ou relaxada só com
   justificativa no relatório.

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `legacy.test.mjs`, `mutation-guard.test.mjs`, `wait-checkpoint.test.mjs`,
  `status-ages.test.mjs`, `collect.test.mjs`, `herdr.test.mjs`,
  `regrid.test.mjs`, `state.test.mjs`, `task-report.test.mjs` (em
  `skills/herdr-soho/scripts/test/`)
- `skills/herdr-soho/scripts/lib/*` — só para bug real do Windows, com teste

## Forbidden

- `setup-local.test.mjs`, `setup-plan.test.mjs`, `setup-probe.test.mjs`,
  `setup-detect.test.mjs`, `doctor.test.mjs` (parte a) e os demais
  arquivos de teste.
- `test/tools.mjs` e `test/fakes.mjs`: só helpers novos, sem mudar os
  existentes.
- Goldens.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera. Não rode a
suíte completa.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e a tabela
teste → causa → mudança.
