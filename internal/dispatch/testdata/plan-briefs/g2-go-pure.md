# Brief — G2: pacotes Go puros (text, jsonjs, provider, sessionref, setuptext)

Role: implementer · Agent: build-2 · Report language: pt-BR

Este brief não tem relação com os anteriores deste painel. Faz parte da migração do herdr-soho para Go.

## Goal

Portar para Go os módulos JS que não dependem de disco, processo nem config, e criar o pacote de JSON
compatível com o JS que toda saída da CLI vai usar. São a base das fatias seguintes e têm muita regra
de texto e regex: aqui o porte tem que ser exato.

Worktree: `/work/herdr-soho/.worktrees/build-2`, branch `go/g2-pure`
(já criado, commit `02910bb`). O JS de referência é o deste worktree:
`/work/herdr-soho/.worktrees/build-2/skills/herdr-soho/scripts/`.

Leia antes, inteiro: o plano `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md`
(seções 2, 3, 4 e 5 valem como decisões deste brief).

Outro worker cria agora, em outro worktree, o `go.mod` e `internal/platform`. Esta fatia não depende
deles: crie aqui um `go.mod` idêntico (`module github.com/djalmajr/herdr-soho`, `go 1.24`, sem
`require`), que o orquestrador reconcilia na integração.

## Decisions already made

1. Pacotes e origens:
   - `internal/text` ← `lib/text.mjs`
   - `internal/provider` ← `lib/provider.mjs`, `lib/quota.mjs`, `lib/dialog.mjs`, `lib/arrival.mjs`
     (um arquivo `.go` por módulo JS)
   - `internal/sessionref` ← `lib/sessionref.mjs`
   - `internal/setuptext` ← `lib/setuptext.mjs`
   - `internal/jsonjs` ← novo
   Nenhum destes pacotes importa outro pacote do projeto além de `internal/text` e
   `internal/jsonjs`.
2. `internal/jsonjs`:
   - `Object`: objeto com chaves em ordem de inserção (`Set`, `Get`, `Delete`, `Keys`). Construtor
     curto, por exemplo `jsonjs.O("a", 1, "b", "x")`.
   - `Undefined`: sentinela; chave com esse valor é omitida, como o `JSON.stringify` omite
     `undefined`. Dentro de array, vira `null`, como no JS.
   - `Stringify(v any) string` (compacto) e `StringifyIndent(v any, indent int) string` (igual a
     `JSON.stringify(v, null, 2)` com `indent` 2).
   - Valores aceitos: `nil`, `bool`, `string`, inteiros, `float64` (formato de número do JS: `1e21`,
     `1e-7`, `-0` → `0`, `NaN`/`Infinity` → `null`), `[]any`, `[]string`, `*Object`/`Object`.
     `map` na saída é erro de programação: `panic` com mensagem clara.
   - Escape de string igual ao do JS: `"` `\\` `\b` `\f` `\n` `\r` `\t`, outros controles abaixo de
     0x20 como `\u00xx` minúsculo; `<`, `>`, `&`, U+2028 e U+2029 sem escape.
   - `Parse(data []byte) (any, error)`: objetos viram `*Object` na ordem do JS (chaves que são
     índices inteiros primeiro, em ordem crescente, depois a ordem de inserção), números `float64`,
     arrays `[]any`. A mensagem de erro não precisa ser a do JS; apenas indique se o JSON é válido.
3. Regex: siga a seção 3 do plano. Toda regex dos módulos JS vira `regexp` do Go ou código, e cada
   uma tem casos hostis (CR, ESC, U+2028, U+00A0, emoji) no diferencial.
4. Testes diferenciais (seção 3 do plano): um gerador por pacote em
   `internal/<pkg>/testdata/gen_<nome>.mjs`, rodado com `node` da raiz do worktree. Ele importa o
   módulo JS, aplica a um corpus (as entradas dos testes JS e as hostis) e grava
   `testdata/<nome>.json` com entrada e saída. O teste Go lê o JSON e compara. Para o `jsonjs`, o
   gerador usa `JSON.stringify` e `JSON.parse` do próprio node sobre um corpus que cubra escape,
   números, ordem de chaves, `undefined` e indentação.
5. Testes portados: casos de `scripts/test/text*`, `provider.test.mjs`, `quota.test.mjs`,
   `dialog.test.mjs`, `sessionref.test.mjs` e dos testes que importam `setuptext.mjs`
   (`setup-legacy.test.mjs`, `setup.test.mjs`, `setup-local.test.mjs`, `parity-setup.test.mjs`,
   `doctor.test.mjs`, `legacy-migration.test.mjs` — só os casos que chamam funções de
   `setuptext`), um `t.Run` por caso com `// JS: "<título>"`. Os testes que chamam `arrival.mjs` por
   caixa-preta (`dispatch-arrival.test.mjs`) não se portam aqui; o diferencial cobre as funções de
   `arrival`.
6. Nomes: exportações do JS em PascalCase, parâmetros na mesma ordem; valores padrão do JS viram
   parâmetros explícitos. Constantes e tabelas exportadas também.

## Expected result

Cole a saída de cada comando no relatório. Rode com o ambiente da seção 4 do plano
(`<slot>` = `build-2`), a partir de `/work/herdr-soho/.worktrees/build-2`.

1. `gofmt -l internal` → vazio. `go vet ./...` → limpo. `go test ./...` → ok, com a contagem de
   casos por pacote (`go test -v ./... | grep -c '^    --- PASS'` ou equivalente).
2. `GOOS=windows GOARCH=amd64 go vet ./...` → limpo.
3. Os geradores rodam de novo e não mudam os JSON versionados:
   `for g in internal/*/testdata/gen_*.mjs; do node "$g"; done && git status --porcelain internal`
   mostra só arquivos novos, e rodar duas vezes dá o mesmo conteúdo (mostre `shasum` antes e depois).
4. Tabela no relatório: cada regex do JS → a forma Go (ou "código") → os casos hostis que a cobrem.
5. Uma mutação por pacote com regra (`provider`, `quota`, `dialog`, `jsonjs`, `sessionref`), numa
   cópia fora do repositório com
   `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes,
   mostrando o teste que fica vermelho.
6. Lista da API criada: cada nome exportado com a assinatura Go.

## Owned files

- `go.mod` (cópia provisória, idêntica à descrita acima)
- `internal/text/**`, `internal/jsonjs/**`, `internal/provider/**`, `internal/sessionref/**`,
  `internal/setuptext/**`

## Forbidden

- Tudo em `skills/`, `plugin/`, `docs/`, `cmd/`, `internal/platform/`, `internal/cli/`,
  `README.md`, `AGENTS.md`, `.gitignore`. O JS é a especificação: não mude uma linha dele.
  Divergência ou bug achado no JS vai para o relatório.
- Módulos de terceiros, `go.sum`, rede. Cache e build só em `/tmp/hs-go/build-2/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos criados, a API (item 6), divergências do
JS encontradas, casos JS não portados com motivo e perguntas abertas.
