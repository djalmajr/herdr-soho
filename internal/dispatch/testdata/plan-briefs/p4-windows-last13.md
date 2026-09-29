# Brief — P4: as 13 falhas que restam no Windows

Role: implementer · Agent: build · Report language: pt-BR

Worktree própria, branch `fix/test-portability-win-a`, já avançada para
`fix/test-portability` `2b9139f` (todas as fatias anteriores integradas).
Caminhos relativos à raiz dela. Issue:
https://github.com/djalmajr/herdr-soho/issues/17.

## Goal

As 13 falhas do Windows (Node 24) no commit `2b9139f` somem, sem mudar o
comportamento em macOS/Linux.

## Decisions already made

TAP completo:
`/work/herdr-soho/.herdr-soho/w14/windows-port-2b9139f.tap`.
A causa de cada grupo já foi diagnosticada (algumas com sonda no próprio
Windows); aplique exatamente estas correções:

1. **`dispatch.test.mjs:41` (1 falha).** `samePath(a, b, 'posix')` usa
   `path` do host, que no Windows é `path.win32` (ignora caixa). Produto:
   em `lib/dispatch.mjs` `samePath`, a API é
   `platform === 'win32' ? path.win32 : path.posix`.
2. **`legacy.test.mjs` "doctor (launcher)" e "setup + doctor (launcher)"
   (8 falhas, `rc 0` e stdout vazio).** No Windows o `doctorRun` põe no
   `PATH` um `node.cmd` feito por `linkTool`. O launcher
   `scripts/herdr-soho.cmd` chama `node -e …` sem `call`; em batch, chamar
   outro `.cmd` sem `call` transfere o controle e o launcher termina ali
   com 0 e sem saída. Correção **no teste**: no Windows, não crie o link de
   `node`; ponha o diretório real do `node` em uso
   (`path.dirname(nodeBin())`) no `PATH` depois do `bin` dos fakes. Deixe
   um comentário curto com o motivo. O launcher **não** muda.
3. **`mutation-guard.test.mjs:107` (1 falha, linha 121).** O prefixo
   Windows do diretório temporário (`ha mutation 'guard' `) põe aspas
   simples no caminho, e o caso `target-dir = '<caminho absoluto>'` (string
   literal TOML entre aspas simples) quebra. Sonda no Windows: com o mesmo
   caso sem aspas no caminho o guard sai 1 como esperado. Correção: prefixo
   Windows `ha mutation guard ` (só espaço); POSIX continua com o atual.
4. **`setup-local.test.mjs:647` `assertSafeLocalRels` (1 falha).** No
   Windows `\` agora é separador (aceito), então `back\slash` não é mais
   rejeitado ali. A expectativa passa a depender da plataforma testada:
   rejeitado em POSIX, aceito como `back/slash` em `win32` (use o parâmetro
   de plataforma que a função já recebe, testando os dois lados em
   qualquer host).
5. **`setup-local.test.mjs:1137` (1 falha).** No Windows o CLI imprime
   `state dir ignored: a\b/` (separador misto). Produto: a forma mostrada
   pelo `stateDirShown`/`classifyStateDir` (`lib/setuplocal.mjs`) usa `/`
   como separador em todas as plataformas (a mesma forma que vai no
   exclude); em POSIX nada muda. Teste com `platform: 'win32'` (ou a API
   `path.win32`) que falha sem a correção.
6. **`setup-probe.test.mjs:112` (1 falha, `hookFailed`).** O `test.after`
   que apaga o `ROOT` dá `EPERM` no Windows (um processo filho do fake de
   timeout ainda segura o diretório). Correção: `fs.rmSync(ROOT, {
   recursive: true, force: true, maxRetries: 10, retryDelay: 200 })`; se
   o fake de timeout deixa um neto vivo, garanta que ele termine (ex.: o
   fake encerra o próprio filho ao receber o sinal), sem mudar o que o
   teste prova.

## Expected result

Diff mínimo nos arquivos abaixo; tabela no relatório: falha → correção.

## Acceptance criteria

1. `node --test` e `bun test` dos seis arquivos de teste tocados no macOS
   → 0 fail (cole).
2. Os itens 1 e 5 (produto) com teste que falha sem a correção (mutação
   executada, `// Mutation captured: …`).
3. Nenhuma asserção removida ou relaxada fora do que está decidido acima.

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `skills/herdr-soho/scripts/lib/dispatch.mjs` (só `samePath`)
- `skills/herdr-soho/scripts/lib/setuplocal.mjs` (só a forma mostrada do
  state dir)
- `skills/herdr-soho/scripts/test/dispatch.test.mjs`, `legacy.test.mjs`,
  `mutation-guard.test.mjs`, `setup-local.test.mjs`, `setup-probe.test.mjs`

## Forbidden

- `scripts/herdr-soho.cmd` e os outros launchers; goldens; os demais
  arquivos.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera. Não rode a
suíte completa.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e a tabela
falha → correção.
