# Brief — W: falhas conhecidas da suíte no Windows

Role: implementer · Agent: build-4 · Report language: pt-BR

## Goal

Deixar verdes no Windows os testes que falham hoje na árvore integrada, sem esconder defeito de
produção. O orquestrador roda a suíte no Windows depois; você não tem acesso a essa máquina, então
trabalhe pelos logs abaixo e pelo código.

Worktree: `/work/herdr-soho/.worktrees/build-4`, branch
`fix/windows-tests` (commit `02910bb`, a árvore integrada).

Logs do Windows (Node 24.12, commit `d7bd2bd`, mesmo código de teste desta árvore):
- Node, TAP em UTF-8: `/tmp/hs-win-logs/node.tap.txt` (16 falhas) e `/tmp/hs-win-logs/plugin.tap.txt`
  (5 falhas).
- Bun: `/tmp/hs-win-logs/bun.txt` e `/tmp/hs-win-logs/plugin.txt`.
O "ΓÇö" que aparece no TAP é só o travessão exibido em CP437 pelo console; não é a causa.

## Decisions already made

1. **Falhas e o que já se sabe:**
   - `codex-env.test.mjs`, 4 casos "Decision 2 …": dependem de ancestrais de processo (`ps`,
     `/proc`). Se o código de produção não tem esse caminho no Windows, o caso ganha
     `skip` no win32 com o motivo; se tem, corrija o teste.
   - `collect.test.mjs` casos 30 e 31: `runCliTreeKill` (`lib/platform.mjs`) grava o arquivo de
     especificação em `os.tmpdir()`, que no Windows vem de `TEMP`/`TMP`; o teste aponta para uma
     pasta que não existe e o processo cai com `ENOENT` (exit 1 em vez de 4). Isso é defeito de
     produção: um `TEMP` inexistente não pode derrubar a CLI. Corrija a produção (a regra: criar a
     pasta, ou cair para outra pasta gravável — escolha uma, justifique) e ajuste o teste só se ele
     também estiver errado.
   - `send.test.mjs`, 9 casos: o `herdr` falso no Windows é um `.cmd` (`test/fakes.mjs`), e o
     `cmd.exe` corta o argumento no primeiro fim de linha; o teste recebe só a primeira linha. O
     `herdr` real é um `.exe` e recebe o texto inteiro. Não mude a produção por isso. Escolha no
     teste um caminho que receba o texto inteiro no Windows (por exemplo o falso gravar o que
     recebe de outra forma, ou um executável que não passe pelo `cmd.exe`); se nenhum for possível,
     `skip` no win32 só nas asserções de texto multilinha, com o motivo, mantendo o resto do caso.
   - `plugin/test/picker.test.mjs`, 5 casos de clipboard e `notifyCopied`: leia o TAP (o primeiro
     devolve `osc52` em vez de `pbcopy`) e decida entre defeito de produção e teste que não isola a
     plataforma. Defeito de produção se corrige na produção.
2. Nenhum prazo de teste muda, e nenhum teste que passa hoje no Mac muda de comportamento no Mac.
3. Todo `skip` no win32 cita no próprio teste o motivo em uma linha.

## Expected result

- Para cada falha, no relatório: causa com a evidência do log (linha do TAP), a correção e se é de
  produção ou de teste.
- No Mac: `node --test` e `bun test --timeout 60000` dos arquivos tocados → 0 fail (cole). Suíte
  completa não: o orquestrador roda.
- Para cada correção de produção, uma mutação numa cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes)
  mostrando o teste que fica vermelho.
- Um roteiro PowerShell em `/tmp/hs-go/build-4/win-check.ps1` que o orquestrador copia para o
  Windows e roda: executa só os arquivos tocados com Node e com Bun a partir de uma cópia do
  repositório em `%TEMP%\herdr-soho-w` e imprime `HS-W-DONE` no fim. Não execute.

## Owned files

- `skills/herdr-soho/scripts/test/codex-env.test.mjs`, `collect.test.mjs`, `send.test.mjs`,
  `fakes.mjs`
- `plugin/test/picker.test.mjs`
- Produção só onde a decisão 1 diz "defeito de produção": `skills/herdr-soho/scripts/lib/platform.mjs`
  (só `runCliTreeKill` e o que ele chama) e `plugin/clipboard.mjs`/`plugin/picker.mjs` (só o
  necessário)

## Forbidden

- Todo o resto e qualquer outro worktree. Herdr real que escreva.
- Nenhum comando git que escreva neste repositório.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, um item por falha e por entrada de "Expected result", com
`[done]` / `[partial]` / `[skipped]` + motivo e saídas coladas.
