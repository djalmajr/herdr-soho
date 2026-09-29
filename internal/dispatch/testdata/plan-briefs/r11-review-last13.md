# Brief — Revisão R11: últimas correções do Windows (issue #17)

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está em `fix/test-portability` `32b44cf`. Confirme com
`git log --oneline -1` e revise só `git diff 2b9139f HEAD` (um commit,
`408c38a`, mais o merge).

## Goal

Achados ancorados em `arquivo:linha`, com execução, antes do push e da PR
que fecha a #17.

## Contrato decidido (verifique que o código cumpre)

- `samePath`: `path.win32` no Windows, `path.posix` no resto.
- `stateDirRelShown` (`lib/setuplocal.mjs`): o state dir relativo é
  mostrado com `/` no Windows; em POSIX a saída é a de antes.
- Testes: `legacy.test.mjs` usa o diretório real do `node` no `PATH` no
  Windows (sem `node.cmd`, que encerraria o launcher batch sem `call`);
  prefixo Windows sem aspas no `mutation-guard`; `back\slash` rejeitado em
  POSIX e aceito como separador em `win32`; limpeza do `setup-probe` com
  `maxRetries`/`retryDelay` e o neto do fake de timeout terminando sozinho.

## O que verificar

1. Alguma saída em macOS/Linux mudou (`state dir ignored: …`, goldens)?
2. O fake de timeout do `setup-probe` ainda prova o timeout (o neto que
   vive 1,5 s não deixa o teste passar por acaso nem vaza processo)?
3. Os testes novos pegam as mutações que declaram.

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test` só nos arquivos
tocados; sondas em `/tmp`. Antes de afirmar que algo falha, rode e cite a
saída.

## Forbidden

- Editar qualquer arquivo do checkout. Revisor é somente leitura.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
