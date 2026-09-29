# Brief — Revisão R8: suíte da skill portátil (Linux, root, Windows) — issue #17

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está em `fix/test-portability` (já com o `main` atual mesclado).
Confirme com `git log --oneline -1` e revise `git diff origin/main...HEAD`.

## Goal

Achados ancorados em `arquivo:linha`, com execução, antes do push e da PR
que fecha a #17.

## Contrato decidido (verifique que o código cumpre)

- Nenhum teste lê `herdr`, `pi`, `opencode` do `PATH` do host: fakes num
  `PATH` controlado; goldens intocados.
- Root: só os casos de bits de permissão de `setup-local.test.mjs` pulam
  com `permission bits do not apply to root`.
- `run-tests.sh` e as suítes Bash que usam `jq` saem 2 com mensagem que
  nomeia a ferramenta ausente.
- Windows: `linkTool`/`canSymlink` em `test/tools.mjs` (launcher `.cmd` no
  Windows, symlink no resto); testes de symlink pulam só sem privilégio;
  `USERPROFILE`, `COMSPEC`/`PATHEXT` nas fixtures; `path.delimiter`;
  expectativas JSON por `JSON.stringify`; bits de modo POSIX e `chmod 000`
  só onde a plataforma os aplica.
- Duas mudanças de produto:
  - `lib/config.mjs` `stateRootPath`: `root + '/' + d` → `path.resolve(root, d)`;
  - `lib/dispatch.mjs` `samePath` (canonização com `path.win32` no Windows)
    antes de decidir entre o estado e `$TMPDIR`.

## O que verificar com atenção

1. `path.resolve` normaliza `..` e `.` lexicamente. As recusas do
   `setup --local` e do `doctor` para estado sob symlink (`loop/..`,
   `sub/..`, `./cache`, alias externo) e as mensagens que mostram o state
   dir continuam iguais? Rode `setup-local.test.mjs` e sondas próprias.
2. `samePath` no Windows compara com maiúsculas/minúsculas (`C:` × `c:`)?
   Diga se isso muda o roteamento na prática e proponha a correção mínima.
3. Algum `skip` ou condição de plataforma esconde uma asserção que deveria
   rodar em macOS/Linux?
4. Algum teste ainda depende do host (ferramenta, usuário, locale)?

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test` nos arquivos que o
diff toca (não rode a suíte inteira); sondas em `/tmp` com `HOME`
temporário. Antes de afirmar que algo falha, rode e cite a saída.

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
