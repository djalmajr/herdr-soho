# Brief — W2: segunda rodada das falhas do Windows

Role: implementer · Agent: build · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

A primeira rodada (commit `02c88b1`, branch `fix/windows-tests`) consertou a maior parte, mas no
Windows real ainda falham 6 casos no Node e 5 no Bun, e a revisão reprovou com 4 P2 e 2 P3. Fechar
tudo isso sem esconder defeito de produção.

Worktree: `/work/herdr-soho/.worktrees/s4`, branch `fix/windows-tests` (commit `02c88b1`, já com a primeira rodada).

Leia antes:
- Revisão: `/work/herdr-soho/.herdr-soho/w14/reports/review-20260928T194610.md`
  (achados 1 a 6, com provas).
- Resultado no Windows depois da primeira rodada (Node e Bun, só os 4 arquivos tocados):
  `/tmp/hs-win-logs/hs-w.utf8.txt`.
- Brief da primeira rodada: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/w-windows-tests.md`.

## Decisions already made

1. **Fixture do clipboard** (achado 1): o fake Windows em `plugin/test/picker.test.mjs` importa
   `fs` no topo (`import fs from 'node:fs'`) e grava `process.argv.slice(2).join(' ') + '\n'`, sem
   espaço final.
2. **Skips** (achado 2): todo skip condicional dentro do corpo vira `return t.skip('<motivo>')`, ou a
   opção `{ skip: … }` do teste. Nenhum corpo roda depois de um skip.
3. **`TMPDIR` no `runCli`** (achados 3 e 5): uma função única resolve a pasta temporária de
   trabalho do `runCli` — `path.resolve(env.TMPDIR || os.tmpdir())` e `mkdirSync(…, { recursive:
   true })` — e as três rotas a usam (treekill, `mergeOutput`, `outputFiles`). Se a criação ou a
   escrita falhar (arquivo no lugar da pasta, `ENOTDIR`, `EACCES`), o `runCli` devolve o erro no
   campo `error` como faz nas outras falhas, sem lançar. Os demais usos de `os.tmpdir()`/`TMPDIR`
   citados na revisão (`models.mjs`, `dispatch.mjs`, `wait.mjs`) ficam como estão; liste no relatório
   se algum deles cai com `TEMP` inexistente.
4. **Teste de regressão do TMPDIR no Windows real**: no log, `Windows .cmd timeout creates a missing
   TMPDIR before writing its spec` volta `status: null` sem erro. O ambiente do teste tem só
   `PATH`, `PATHEXT`, `COMSPEC` e `TMPDIR`; no Windows real falta pelo menos `SystemRoot`, que o
   `cmd.exe` e o node precisam. No win32 real o teste herda o ambiente mínimo do Windows (como o
   `withFakeDir` do plugin faz), e continua simulando a rota no Mac.
5. **`send` no Windows** (achado 4 e o log): o `cmd.exe` corta o argumento no primeiro fim de linha
   e descarta também todos os argumentos seguintes (o `--wait …` some). Portanto:
   - no win32, os testes de CLI conferem só o que vem antes do argumento de texto, e a primeira
     linha dele. Nada que venha depois do texto é conferido no win32;
   - um teste de unidade independente de plataforma chama a função de `lib/peer.mjs` que monta o
     argv do `agent prompt` e confere o texto inteiro (cabeçalho, corpo hostil já limpo, rodapé) e
     os argumentos finais. Se essa função não existir separada, extraia-a sem mudar o
     comportamento;
   - o teste do corpo hostil deixa de passar vazio no win32: as asserções do corpo ficam
     explicitamente fora do win32, e o teste de unidade cobre o corpo em todas as plataformas.
6. **codex-env** (achado 6): o motivo do skip passa a ser o real (a produção devolve a mensagem base
   no win32 por desenho, `lib/codex-env.mjs:416-418`), e um teste de unidade independente de
   plataforma prova o contrato `win32 → baseMessage` com `getAncestors` injetado.

## Expected result

- Por achado da revisão e por falha do log do Windows: o que mudou, com a linha do log ou da
  revisão.
- No Mac: `node --test` e `bun test --timeout 60000` dos arquivos tocados → 0 fail (cole).
- Uma mutação por correção de produção (item 3) e pelo teste de unidade do `send`, numa cópia fora
  do repositório (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>`
  antes), mostrando o teste vermelho.
- O roteiro `/tmp/hs-go/build/win-check.ps1` atualizado com os arquivos tocados (mesmo
  formato do anterior, `/tmp/hs-go/build-4/win-check.ps1`). Não execute.

## Owned files

- `plugin/test/picker.test.mjs`
- `skills/herdr-soho/scripts/test/codex-env.test.mjs`, `collect.test.mjs`, `send.test.mjs`, e um
  arquivo de teste novo de unidade do `peer`, se precisar
- `skills/herdr-soho/scripts/lib/platform.mjs` (só o `runCli` e a função nova da decisão 3)
- `skills/herdr-soho/scripts/lib/peer.mjs` (só a extração da decisão 5, sem mudar comportamento)

## Forbidden

- Todo o resto e qualquer outro worktree. Herdr real que escreva.
- Nenhum comando git que escreva neste repositório.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, por item `[done]` / `[partial]` / `[skipped]` + motivo, com saídas
coladas e as mutações.
