# Brief — P3d: o que ainda depende do host no Linux (WSL, root)

Role: implementer · Agent: build · Report language: pt-BR

Mesma worktree e branch da P3c (`fix/test-portability-linux`); siga sobre
o que você já deixou nela. Caminhos relativos à raiz. Issue:
https://github.com/djalmajr/herdr-soho/issues/17.

## Goal

Os dois testes abaixo passam num Linux sem `herdr`, `claude` e com
qualquer outra CLI instalada ou não, sem mudar goldens.

## Decisions already made

- Resultado no WSL Ubuntu 26.04 (root, Node 22.22, sem `herdr`, `claude`,
  `jq`, `bun`), commit `08324e6`:
  ```
  not ok 213 - a reviewer is refused when a released author shares its family
    location: '.../skills/herdr-soho/scripts/test/for-released.test.mjs:71:1'
    error: |-
      herdr-soho: herdr CLI not found in PATH
      2 !== 5
  not ok 398 - parity: kinds table with fake CLIs on PATH
    location: '.../skills/herdr-soho/scripts/test/parity-kinds.test.mjs:80:1'
    + 'claude   claude        anthropic  max      no\n' +
    - 'claude   claude        anthropic  max      yes\n' +
  ```
- `for-released.test.mjs`: o teste usa um `herdr` fake num `PATH`
  controlado (`writeFakeCli` de `test/fakes.mjs`), como os de
  `collect.test.mjs`/`friction.test.mjs` já fazem.
- `parity-kinds.test.mjs` "kinds table with fake CLIs on PATH": o `PATH`
  do passo passa a ser **só** o diretório dos fakes e o do `node` em uso
  (sem o `PATH` do host), com um fake para cada executável que o golden
  marca `yes` e nenhum para os que marca `no` (`gemini`). Assim o
  resultado não muda com o que o host tem instalado. O golden não muda.
- Procure no resto de `skills/herdr-soho/scripts/test/*.test.mjs` outro
  teste cuja expectativa dependa de uma CLI do host estar ou não no `PATH`
  (`kinds`, `doctor`, `setup --detect`, `explain`): liste-os no relatório
  com arquivo:linha e corrija do mesmo jeito os que estiverem nos arquivos
  permitidos abaixo; os outros ficam como pergunta aberta.

## Expected result

Os dois testes passam com `PATH` mínimo e também com `PATH` que tenha um
`gemini` falso extra (prova de que o host não influi).

## Acceptance criteria

1. `PATH=/usr/bin:/bin:$(dirname "$(command -v node)") node --test skills/herdr-soho/scripts/test/for-released.test.mjs skills/herdr-soho/scripts/test/parity-kinds.test.mjs` → 0 fail (cole).
2. O mesmo com um diretório extra no início do `PATH` contendo um
   `gemini` executável falso → 0 fail (cole).
3. `bun test` dos dois arquivos → 0 fail (cole).

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `skills/herdr-soho/scripts/test/for-released.test.mjs`
- `skills/herdr-soho/scripts/test/parity-kinds.test.mjs`
- `skills/herdr-soho/scripts/test/parity.mjs` — só se o `PATH` do passo
  for montado lá, sem mudar o comportamento dos outros testes de paridade

## Forbidden

- Goldens; código de produção.
- `collect.test.mjs` e os arquivos das partes a/b do Windows.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera. Não rode a
suíte completa.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas.
