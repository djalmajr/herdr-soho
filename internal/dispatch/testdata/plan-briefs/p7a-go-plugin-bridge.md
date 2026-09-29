# Brief — P7a: o `bridge` e o `clipboard` do plugin em Go

Role: implementer · Agent: build-3 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

Portar a ponte do plugin do Herdr e a cópia para a área de transferência. A ponte roda as ações
`doctor`, `roster` e `pick` a partir do contexto que o Herdr injeta (`HERDR_PLUGIN_CONTEXT_JSON`).
Antes de agir, ela confere o painel focado com `HERDR_BIN_PATH pane get`. Depois roda a CLI no cwd
desse painel com `HERDR_SOHO_NOWRITE=1`. O plugin promete não escrever no projeto focado: um contexto
mal validado, ou uma CLI rodada sem `NOWRITE`, quebra essa promessa num projeto de outra pessoa.

O `picker` (a interface interativa) é outra fatia. O manifesto `herdr-plugin.toml` continua com
`node` até o corte.

Worktree: `/work/herdr-soho/.worktrees/build-3`, branch `go/p7a` (criado
de `go/port`, commit `a59fb53`).

- Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5; a
  fase 7).
- JS de referência: `plugin/bridge.mjs` e `plugin/clipboard.mjs`, com os testes
  `plugin/test/bridge.test.mjs`. Do `picker.test.mjs`, só os casos de clipboard.

## Decisions already made

1. `internal/plugin`: `bridge.go` ← `bridge.mjs`, `clipboard.go` ← `clipboard.mjs`.
2. Subcomandos do mesmo binário: `herdr-soho plugin bridge <doctor|roster|pick>` e
   `herdr-soho plugin clipboard` (o texto vem pela entrada padrão). Os dois ficam fora da ajuda
   pública, como os helpers internos.
3. A ponte roda a CLI chamando o próprio binário (`os.Executable`) com `doctor`/`roster`, em vez de
   `node ../skills/.../herdr-soho.mjs`. Mesmo cwd, mesmo ambiente, `HERDR_SOHO_NOWRITE=1` sempre.
   Mesma saída e mesmos códigos: 2 para invocação ou alvo inválido, 4 para falha do Herdr, e o
   código da CLI no resto.
4. O `pick` abre o painel do picker por `HERDR_BIN_PATH plugin pane open … --focus`, com os mesmos
   argumentos do JS.
5. O clipboard escolhe a ferramenta como o JS: `pbcopy`, `wl-copy`/`xclip`/`xsel` e `clip.exe`, na
   mesma ordem e com os mesmos argumentos, e a sequência OSC 52 como último recurso, byte a byte igual
   (`osc52Sequence`). No Windows, `.cmd` passa pela regra do `cmdInvocation`,
   que já está em `internal/platform`.
6. Testes: os casos de `bridge.test.mjs` e os de clipboard, com `// JS: "<título>"`. Nos testes, o
   `fakecli` faz o papel do `herdr` e das ferramentas de clipboard e grava o argv e o ambiente.
   Nunca use o Herdr real nem a área de transferência de verdade.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build-3`), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. Diferencial Go × JS da ponte com o herdr falso, colado:
   - contexto válido, JSON quebrado, pane de outro workspace, `pane get` falhando e subcomando
     desconhecido;
   - para cada caso: stdout, stderr, código, as chamadas ao herdr falso e o ambiente passado à CLI,
     inclusive `HERDR_SOHO_NOWRITE=1`.
3. Uma mutação por regra, com o teste vermelho e o código de saída colados, uma linha por mutação.
   As regras:
   - `NOWRITE` sempre posto;
   - workspace divergente recusado;
   - JSON quebrado recusado sem chamar a CLI;
   - cwd do painel usado;
   - ordem das ferramentas de clipboard.

   Faça cada mutação numa cópia fora do repositório, com
   `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes.
4. API nova no relatório.

## Owned files

- `internal/plugin/**`, o registro de `plugin` em `internal/cli/cli.go` e `internal/cli/plugin*_test.go`.

## Forbidden

- O resto de `internal/**`, `go.mod`, `skills/`, `plugin/` (o JS e o manifesto ficam como estão),
  `docs/`. Editar os worktrees `gate` e `h1`.
- Herdr real, a área de transferência real, rede, módulos de terceiros, `go.sum`. Cache e build só
  em `/tmp/hs-go/build-3/`.
- Subagentes (se você dividir o trabalho) nunca gravam no caminho do relatório do contrato: só você
  escreve o relatório, uma vez, no fim.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, API, divergências e perguntas abertas.
