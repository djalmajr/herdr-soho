# Brief — P1: testes herméticos no Linux (ferramentas do host, root, jq)

Role: implementer · Agent: build · Report language: pt-BR

Worktree própria, branch `fix/test-portability-linux` (base: `fix/test-portability`).
Caminhos relativos à raiz dela. Contexto: a suíte da skill passa neste Mac
porque ele tem `herdr`, `pi`, `opencode` e `jq` instalados; em Linux sem
essas ferramentas (servidor e WSL) ela falha. O produto funciona lá; o
problema é o teste depender do host.

## Goal

Nenhum teste depende de ferramenta instalada no host nem do usuário que o
roda, e o runner falha com mensagem clara quando falta uma dependência de
desenvolvimento.

## Decisions already made

1. **Ferramentas do host.** Todo teste que hoje depende de `herdr`, `pi` ou
   `opencode` estarem no `PATH` do host passa a usar fakes num `PATH`
   controlado (use `writeFakeCli` de `skills/herdr-soho/scripts/test/fakes.mjs`).
   Falhas observadas em Linux sem essas ferramentas:
   - `collect.test.mjs` (9 testes) e `friction.test.mjs` (6): `herdr-soho: herdr CLI not found in PATH`;
   - `parity-kinds.test.mjs` "kinds table with fake CLIs on PATH": `pi` e
     `opencode` saem `no` (o golden espera `yes`, gravado num host que os tem);
   - `parity-spawn.test.mjs` "config layers": stderr ganha
     `executable 'pi' not found in PATH`.
   O golden **não** muda: o teste passa a criar os fakes que faltam.
2. **Root.** Testes que provam falha por permissão (arquivo ilegível,
   diretório sem escrita) não valem como root: pule-os com
   `t.skip('permission bits do not apply to root')` quando
   `process.getuid?.() === 0`. Observados em `setup-local.test.mjs`
   (6 testes: "unreadable exclude", "unwritable parent", incluindo
   `--plan` e `--dry-run`). Não pule nada além desses.
3. **Dependência do runner.** `skills/herdr-soho/scripts/run-tests.sh`
   verifica antes de rodar as ferramentas que as suítes Bash usam (`jq` e
   as que você encontrar nos `test-*.sh`); faltando alguma, imprime em
   stderr `run-tests.sh: needs <tool> (the bash suites use it); install it or run the Node suites only`
   e sai 2. Uma suíte Bash rodada direto também falha com mensagem (não
   em silêncio): `test-<x>.sh: needs jq` e exit 2.

## Expected result

Com `PATH` sem `herdr`, `pi`, `opencode` a suíte Node passa; como root os
testes de permissão aparecem como `skip`; sem `jq` o runner explica.

## Acceptance criteria

1. `PATH=/usr/bin:/bin:$(dirname "$(command -v node)") node --test skills/herdr-soho/scripts/test/collect.test.mjs skills/herdr-soho/scripts/test/friction.test.mjs skills/herdr-soho/scripts/test/parity-kinds.test.mjs skills/herdr-soho/scripts/test/parity-spawn.test.mjs`
   → 0 fail (cole a saída), e o mesmo com `bun test` usando o `bun` por
   caminho absoluto.
2. Um teste novo prova a mensagem e o exit 2 do `run-tests.sh` sem `jq`
   (PATH controlado sem `jq`), com `// Mutation captured: …` executado.
3. Os 6 testes de permissão do `setup-local.test.mjs` usam o guard de root
   (mostre o diff); rodados como usuário comum continuam passando.
4. `node --test` e `bun test` de todos os arquivos tocados → 0 fail;
   `skills/herdr-soho/scripts/run-tests.sh --env outside test-status.sh test-setup.sh` → PASS.

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `skills/herdr-soho/scripts/test/collect.test.mjs`, `friction.test.mjs`, `parity-kinds.test.mjs`, `parity-spawn.test.mjs`, `setup-local.test.mjs`
- `skills/herdr-soho/scripts/test/fakes.mjs` — só helpers novos, sem mudar os existentes
- `skills/herdr-soho/scripts/run-tests.sh` e os `skills/herdr-soho/scripts/test-*.sh` — só a checagem de dependência
- `skills/herdr-soho/scripts/test/run-tests.test.mjs` — novo, se precisar

## Forbidden

- Código de produção em `skills/herdr-soho/scripts/lib/` e `herdr-soho.mjs`.
- Goldens.
- Testes com symlink que não estão na lista acima (outra fatia cuida do Windows).
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Sources (local paths, read before editing)

- `skills/herdr-soho/scripts/test/fakes.mjs`
- `skills/herdr-soho/scripts/test/parity.mjs` (`fixtureEnv`)
- `skills/herdr-soho/scripts/run-tests.sh`

## Checks you may run

Os dos critérios e `node --test <um arquivo>` enquanto itera. Não rode a
suíte completa nem a matriz inteira; não rode formatador.

## Non-goals

Windows; mudar comportamento do CLI.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas (nunca redigitadas)
e as mutações executadas.
