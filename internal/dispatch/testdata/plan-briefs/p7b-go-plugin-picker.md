# Brief — P7b: o `picker` do plugin em Go, e o clipboard do Windows igual ao JS

Role: implementer · Agent: build-3 · Report language: pt-BR

Continuação da sua P7a, mesmo assunto.

## Goal

1. Portar o `picker`: o painel sobreposto que lista as sessões (local e as máquinas habilitadas, pelo
   `find --json`), filtra enquanto se digita e copia a referência escolhida. Enter copia, Esc ou
   Ctrl-C fecham sem copiar.
2. Corrigir o clipboard do Windows da P7a. O meu brief pediu `clip.exe`, mas a referência é o JS, e o
   `plugin/clipboard.mjs` usa PowerShell com argumentos que o teste JS fixa. O Go segue o JS; o erro
   foi do brief, não seu.

O `picker` lê a tela e o teclado de quem está usando o Herdr. Um modo raw que não é desfeito na saída
deixa o terminal do usuário quebrado. Um texto de sessão com sequências de controle que não são
removidas pode pintar a tela ou mover o cursor.

Worktree: `/work/herdr-soho/.worktrees/build-3`, branch `go/p7b` (criado
de `go/port`, commit `995640f`, que já tem a sua P7a).

- Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5; a
  fase 7).
- JS de referência: `plugin/picker.mjs` e `plugin/clipboard.mjs`, com os testes
  `plugin/test/picker.test.mjs`.

## Decisions already made

1. `internal/plugin/picker.go` ← `picker.mjs`: estado, filtro, `stripControls`, render na largura
   (UTF-16 como o JS), `applyKey`, `feedChunk` com o ESC sozinho e as sequências, `flushEsc`,
   `notifyCopied`, e a lista pelo `find --json` do próprio binário.
2. Subcomando `herdr-soho plugin picker`, fora da ajuda pública.
3. Modo raw sem módulos de terceiros:
   - Unix: `termios` por `syscall` (`TIOCGETA`/`TIOCSETA` no darwin, `TCGETS`/`TCSETS` no linux),
     com build tags;
   - Windows: `GetConsoleMode`/`SetConsoleMode` por `syscall`.

   O modo antigo é restaurado em toda saída: Enter, Esc, Ctrl-C, fim da entrada, erro e pânico.
   Entrada que não é terminal (pipe) segue o JS: sem modo raw.
4. O clipboard do Windows segue o `clipboard.mjs`: o mesmo PowerShell, com os mesmos argumentos e a
   mesma ordem de ferramentas.
5. Testes: os casos de `picker.test.mjs`, com `// JS: "<título>"`. O teclado é simulado por
   `feedChunk` e o fluxo inteiro por um pipe (sem terminal real). O `herdr` e o clipboard são sempre
   o `fakecli`.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build-3`), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `GOOS=linux go vet ./...`, `go test ./...` ok (cole).
2. Diferencial Go × JS das funções puras (filtro, `stripControls`, `entryLine`/`render` em 40 e 80
   colunas, `applyKey`, `feedChunk` com ESC, setas, backspace, texto não ASCII e surrogates) com um
   gerador `testdata/gen_picker.mjs`: zero divergências (cole o resumo).
3. Uma mutação por regra, com o teste vermelho e o código de saída colados, uma linha por mutação.
   As regras:
   - modo restaurado no Esc e no pânico;
   - `stripControls` remove o ESC;
   - ESC sozinho fecha;
   - filtro por palavras (AND);
   - o PowerShell do clipboard.

   Faça cada mutação numa cópia fora do repositório, com
   `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes.
4. API nova no relatório.

## Owned files

- `internal/plugin/**`, o registro de `plugin picker` em `internal/cli/cli.go` e
  `internal/cli/plugin*_test.go`.

## Forbidden

- O resto de `internal/**`, `go.mod`, `skills/`, `plugin/` (o JS e o manifesto ficam como estão),
  `docs/`. Editar os worktrees `gate` e `h1`.
- Pôr o terminal do orquestrador ou de outro painel em modo raw: os testes não abrem terminal real.
  Herdr real, a área de transferência real, rede, módulos de terceiros (`golang.org/x/term`
  inclusive), `go.sum`. Cache e build só em `/tmp/hs-go/build-3/`.
- Subagentes (se você dividir o trabalho) nunca gravam no caminho do relatório do contrato: só você
  escreve o relatório, uma vez, no fim.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, API, divergências e perguntas abertas.
