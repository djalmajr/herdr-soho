# Brief — GW5: as 8 falhas Go que sobraram no Windows

Role: implementer · Agent: build-2 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

A sétima rodada no Windows 11 real, sobre `go/port` `4b9634f` (com GW3 e GW4), caiu de 12 para 8
falhas. Desta vez, os dois testes de golden rodaram com a mensagem de falha completa.

- **Testes que comparam caminho com `/`:**
  - `TestApplyLaneFileGolden`: o aviso de lane custom sai `<ROOT>\dest\herdr-soho.conf` e o golden,
    gravado no Mac, tem `<ROOT>/dest/herdr-soho.conf`.
  - `TestMutationGuard`: `..\.cargo\config.toml`.
- **`TestStateCommandParity`:** o produto agora diz `(EEXIST)` em todo sistema (correção da C1b-c),
  mas o teste da GW4 espera a mensagem nativa do Windows. O teste volta a esperar `(EEXIST)`.
- **`TestFakeCLIConcurrentCallOrdinalIsSerialized`:** o lock do log de chamadas ainda falha
  (`fakecli: lock call log: open ...`) com 23 de 24 chamadas.
- **`TestSetupReadCommands` e `TestSetupWriteBlockAndHooks`:** `models=[]`, onde se esperava
  `grok-4.10 grok-4.7 grok-4.6`. Pode ser produto: a listagem de modelos pelo executável do kind no
  Windows. O mesmo teste espera um modo de arquivo Unix (`mode=666`).
- **`TestRunCliWindows`:** o `.cmd` recebe os argumentos e o `fakecli` não acha regra. A referência
  JS no Windows mostrou que o `cmd.exe` tira o `^` (`caret^` chega como `caret`). O `café` saiu
  estragado pela captura do PowerShell, até na lista de argumentos pedidos, então não foi
  conclusivo.
- **`TestParityConfigGolden`:** leia o detalhe no log (seção `=== DETAIL cli TestParityConfigGolden`).

Deixar os 17 pacotes verdes no Windows.

Worktree: `/work/herdr-soho/.worktrees/build-2`, branch `go/gw5` (criado de `go/port`, commit `a999fa8`).

- Log inteiro, com os detalhes dos dois testes de golden e a referência JS do `.cmd`:
  `/tmp/hs-go/gowin7-log.txt`. As linhas de falha: `/tmp/hs-go/gowin7-failures.txt`.
- Relatórios e briefs da GW3 e da GW4 (as regras valem de novo):
  `/work/herdr-soho/.herdr-soho/w14/plan-briefs/gw4-go-windows-tests.md`.
- Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seção 3).

## Decisions already made

1. **Golden com caminho**: o teste normaliza `\` para `/` na parte do caminho sob `<ROOT>` antes de
   comparar, como o `normalizeRoots` dos testes JS. O golden não muda.
2. **`(EEXIST)`**: o teste espera `(EEXIST)` em todo sistema.
3. **Lock do `fakecli`**: se o lock ainda falha, troque a abordagem no Windows por um arquivo aberto
   com `LockFileEx` (via `syscall`, sem módulos de terceiros), ou por um lock de diretório (`os.Mkdir`
   atômico), o que for mais simples e provado. O teste de 24 processos passa três vezes seguidas.
4. **`models=[]`**: descubra se é produto. Se o `setup --detect` não lista modelos no Windows porque
   não acha o executável do kind (`.exe`, `PATHEXT`) ou não roda o `.cmd`, corrija no produto
   (`internal/kinds` ou `internal/setup`) e diga onde estava. O modo de arquivo segue a regra do
   Windows (o bit de somente leitura).
5. **`.cmd`**: o teste espera o que a referência JS mostrou (`^` removido). Para o `café`, rode o
   caso com a saída gravada em arquivo UTF-8 por dentro do Node ou do Go, não pela captura do
   PowerShell. Prepare o roteiro para o orquestrador rodar.
6. Você não roda no Windows: prepare os binários e os roteiros. O orquestrador roda e devolve o
   log.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build-2`), a partir do worktree:

1. No Mac: `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...` e
   `go test ./...` ok (cole).
2. Os binários de teste Windows de todos os pacotes compilam (cole a lista).
3. Por teste que falhou (os 8): causa, correção, e se era teste ou produto. Uma tabela.
4. O roteiro do `café` (`/tmp/hs-go/build-2/js-win-ref-cafe.mjs`), com a instrução de uma linha para
   rodar.

## Owned files

- Todo `*_test.go` e `testdata/` de `internal/**`, `internal/testutil/fakecli/**`, e no produto
  `internal/platform/**`, `internal/kinds/**` e `internal/setup/**` só para o item 4 das decisões.

## Forbidden

- Outro produto, `go.mod`, `skills/`, `plugin/`, `docs/`. Editar os worktrees `gate` e `h1`. Mudar os
  goldens ou o JSON de referência do Windows.
- Herdr real, rede, módulos de terceiros, `go.sum`. Cache e build só em `/tmp/hs-go/build-2/`.
- Subagentes (se você dividir o trabalho) nunca gravam no caminho do relatório do contrato: só você
  escreve o relatório, uma vez, no fim.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, produto corrigido e por quê, e perguntas
abertas.
