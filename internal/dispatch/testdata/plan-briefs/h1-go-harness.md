# Brief — H1: a suíte JS roda contra o binário Go (`HERDR_SOHO_TEST_BIN`)

Role: implementer · Agent: build-3 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel. Faz parte da migração do herdr-soho para Go.

## Goal

Hoje cada arquivo de teste chama a CLI JS do seu jeito (`spawnSync(nodeBin(), [JS_ENTRY, …])`,
`process.execPath`, caminhos montados à mão). Colocar todas essas chamadas atrás de um helper único
que, com `HERDR_SOHO_TEST_BIN` definida, executa esse binário (o Go) no lugar do JS. Isso transforma a
suíte JS inteira, e os goldens de paridade, na prova de paridade do porte Go.

Worktree: `/work/herdr-soho/.worktrees/build-3`, branch
`test/go-harness` (já criado, commit `e748fd3`: a árvore integrada com o B2 e a correção do Windows).

Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md`, seções 2 (D5 e D7)
e 5.

## Decisions already made

1. Helper novo `skills/herdr-soho/scripts/test/cli.mjs`, com:
   - `cliCommand(args)` → `{ bin, argv }`. Sem `HERDR_SOHO_TEST_BIN`: `bin` = `nodeBin()` de
     `parity.mjs`, `argv` = `[JS_ENTRY, ...args]`. Com a variável (caminho absoluto): `bin` = ela,
     `argv` = `args`.
   - `cliEnv(env)` → cópia do `env` recebido; com `HERDR_SOHO_TEST_BIN`, acrescenta
     `HERDR_SOHO_SKILL_DIR=<skills/herdr-soho absoluto>` e `HERDR_SOHO_JS_RUNTIME=<nodeBin()>`
     (o binário Go usa esse runtime no fallback para o JS quando o `PATH` do teste não tem node).
     Sem a variável, devolve o `env` sem mudança.
   - `spawnCli(args, opts)` → `spawnSync(bin, argv, { ...opts, env: cliEnv(opts.env) })`.
   A leitura de `HERDR_SOHO_TEST_BIN` é do processo de teste, nunca do `env` do filho (o `fixtureEnv`
   remove todo `HERDR_*`).
2. Todo teste que executa a entrada JS da CLI (`herdr-soho.mjs` por qualquer caminho, inclusive
   `ENTRY`, `JS_ENTRY`, `entry` montado à mão e o `parity.mjs`) passa a usar o helper. Sem a variável,
   o comportamento de cada teste fica idêntico ao de hoje (mesmo binário, mesmos argumentos, mesmo
   ambiente).
3. Chamadas que não são a CLI ficam como estão: `node -e`/`--input-type=module` que importam `lib/`
   (testes de unidade do JS), os falsos de `fakes.mjs`, os probes de `parity-lanes`. Liste no
   relatório cada arquivo que ficou só JS e por quê.
4. `plugin/test/*` e as suítes bash (`scripts/test-*.sh`, `run-tests.sh`) ficam fora desta fatia.
5. O binário Go ainda não conhece `HERDR_SOHO_JS_RUNTIME`; outra fatia acrescenta. Não mude código
   Go.

## Expected result

1. `node --test skills/herdr-soho/scripts/test/` e `bun test --timeout 60000` (na pasta
   `skills/herdr-soho`) sem `HERDR_SOHO_TEST_BIN` → mesmo resultado de antes da mudança: rode antes
   e depois e cole os dois resumos (0 fail esperado; se algo já falhava antes, mostre que é igual).
2. Com um "binário" de prova — um script em `/tmp/hs-go/build-3/js-as-bin` que faz
   `exec "$HERDR_SOHO_JS_RUNTIME" "$HERDR_SOHO_SKILL_DIR/scripts/herdr-soho.mjs" "$@"` —,
   `HERDR_SOHO_TEST_BIN=/tmp/hs-go/build-3/js-as-bin node --test skills/herdr-soho/scripts/test/`
   → 0 fail. Isso prova que toda chamada passa pelo helper e que o ambiente chega certo.
   Cole o resumo; qualquer falha é item do relatório com a causa.
3. Prova de que nada ficou para trás: `git grep -n "herdr-soho.mjs" -- skills/herdr-soho/scripts/test`
   só acha `parity.mjs`/`cli.mjs` e os casos listados no item 3 das decisões (cole).
4. Com o binário Go do G1 (`/tmp/hs-go/build/herdr-soho`, se existir; não o recompile nem o
   modifique), rode `HERDR_SOHO_TEST_BIN=/tmp/hs-go/build/herdr-soho node --test
   skills/herdr-soho/scripts/test/` e cole o resumo e a lista de falhas agrupadas por causa. Aqui
   falha é esperada (o fallback do Go ainda não lê `HERDR_SOHO_JS_RUNTIME`); o valor está na lista.
5. Uma mutação no helper (por exemplo ignorar a variável), numa cópia fora do repositório
   (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes),
   mostrando que o item 2 fica vermelho.

Isole a suíte do Herdr real: `HERDR_SOCKET_PATH=/tmp/hs-go/build-3/none.sock`.

## Owned files

- `skills/herdr-soho/scripts/test/cli.mjs` (novo)
- `skills/herdr-soho/scripts/test/*.test.mjs` e `parity.mjs`: só as linhas que executam a CLI e os
  imports do helper

## Forbidden

- `skills/herdr-soho/scripts/lib/**`, `herdr-soho.mjs`, os goldens, `fakes.mjs`, `plugin/`, `docs/`,
  qualquer código Go, qualquer outro worktree. Nenhum teste muda o que verifica.
- Herdr real que escreva. Nenhum comando git que escreva neste repositório.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos tocados, arquivos que ficaram só JS com o
motivo, e perguntas abertas.
