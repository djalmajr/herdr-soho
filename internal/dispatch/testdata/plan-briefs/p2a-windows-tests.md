# Brief — P2a: suíte da skill no Windows (parte a)

Role: implementer · Agent: build · Report language: pt-BR

Worktree própria, branch `fix/test-portability-win-a` (base:
`fix/test-portability`). Caminhos relativos à raiz dela. Issue:
https://github.com/djalmajr/herdr-soho/issues/17.

## Goal

Os testes dos arquivos abaixo passam no Windows (Node 24) sem deixar de
passar no macOS/Linux, e qualquer bug real do CLI no Windows que eles
exponham é corrigido no código com teste.

## Decisions already made

- Resultado real do Windows (TAP completo da suíte, commit `794c0af`):
  `/work/herdr-soho/.herdr-soho/w14/windows-port-794c0af.tap`. Leia os blocos `not ok` dos seus arquivos: é a lista a zerar.
  Maioria: `EPERM ... symlink` ao ligar ferramentas reais (`git`, `node`, …) num `bin/` de teste, e saídas esperadas com caminhos montados por interpolação.
- **Ferramentas no `bin/` de teste:** nunca `symlinkSync` de uma
  ferramenta no Windows. Crie no arquivo **novo**
  `skills/herdr-soho/scripts/test/tools.mjs` (só se ainda não existir na
  sua worktree; a outra parte cria o mesmo helper com o mesmo contrato)
  `linkTool(binDir, name, target)`: POSIX → symlink como hoje; Windows →
  `<name>.cmd` com `@"<target absoluto>" %*\r\n`. E
  `canSymlink(dir)` (tenta uma vez, guarda o resultado) para testes cujo
  objeto é o próprio symlink: sem privilégio, `t.skip('symlinks need privilege on Windows')`.
- **Caminhos em JSON esperado:** monte com `JSON.stringify(caminho)` (ou
  compare o objeto parseado), nunca interpolando o caminho numa string JSON.
- **`PATH`:** use `path.delimiter`, nunca `:` fixo.
- **Bug do produto:** se o CLI gera no Windows um caminho/nome diferente do
  POSIX ou falha por separador, corrija em `skills/herdr-soho/scripts/lib/`
  e acrescente um teste que falharia sem a correção; liste no relatório
  como "bug do produto" com o teste do TAP que o revelou.
- Você não roda Windows: o orquestrador roda depois da integração e manda
  emenda com o que sobrar.

## Expected result

Diff nos arquivos da sua parte (e `tools.mjs`), com uma tabela no
relatório: teste do TAP → causa → mudança (teste ou produto).

## Acceptance criteria

1. `node --test` e `bun test` dos seus arquivos no macOS → 0 fail (cole).
2. `skills/herdr-soho/scripts/run-tests.sh --env outside test-status.sh test-setup.sh` → PASS.
3. Nenhum teste enfraquecido: asserção removida ou relaxada só com
   justificativa no relatório; skip só pelo `canSymlink`.

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `init.test.mjs`, `legacy.test.mjs`, `legacy-migration.test.mjs`, `setup-probe.test.mjs`, `setup-detect.test.mjs`, `setup.test.mjs`, `doctor.test.mjs`, `env.test.mjs`, `models.test.mjs`, `kinds.test.mjs`, `platform.test.mjs` (em `skills/herdr-soho/scripts/test/`)
- `skills/herdr-soho/scripts/test/tools.mjs` — novo, com o contrato acima
- `skills/herdr-soho/scripts/lib/*` — só para bug real do Windows, com teste

## Forbidden

- `collect.test.mjs`, `friction.test.mjs`, `parity-kinds.test.mjs`,
  `parity-spawn.test.mjs`, `setup-local.test.mjs`, `fakes.mjs`,
  `run-tests.sh`, `test-*.sh` (outra fatia) e os arquivos da outra parte.
- Goldens, salvo se a mudança for de caminho/escape e você mostrar o diff.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e a tabela
teste → causa → mudança.
