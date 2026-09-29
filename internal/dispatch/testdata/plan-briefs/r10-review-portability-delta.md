# Brief — Revisão R10: delta da portabilidade depois da R8 (issue #17)

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está em `fix/test-portability` `2b9139f`. Confirme com
`git log --oneline -1`. Revise `git diff 08324e6 HEAD -- skills/` menos o
que veio do `main` (a PR #19, `fix/task-report-per-task`, já revisada:
ignore `lib/dispatch.mjs` na linha do `taskReport` e o teste novo de
`task-report.test.mjs` sobre duas tarefas seguidas).

## Goal

Achados ancorados em `arquivo:linha`, com execução, antes do push e da PR
que fecha a #17.

## Contrato decidido (verifique que o código cumpre)

- Seus dois achados da R8:
  - P1: `stateRoot` e `setup --plan` reconhecem o state dir relativo no
    Windows (qualquer grafia/caixa da raiz) e mostram/escrevem a mesma
    linha de `.gitignore` com `/` — helper único `stateGitignoreRel` em
    `lib/config.mjs` (`isPathWithin` saiu);
  - P2: `samePath` usa `path.win32.relative(a, b) === ''`.
- Mudanças de produto novas, cada uma com teste:
  - `lib/setuplocal.mjs`: `normalizeGitPath` (saída do Git com
    `path.win32` no Windows), `setupTargetPath` (`--target` nativo),
    separador Windows aceito na validação e convertido para `/` em
    `excludeEntry`;
  - `lib/legacy.mjs`: caminhos do diretório `.herdr-agents` com
    `path.join`.
- Testes: probes reais de leitura/escrita no lugar de supor `chmod`
  (também cobre root); `canSymlink` só onde o objeto é o symlink; fakes
  por `writeFakeCli`/`linkTool`; `fileURLToPath`/`pathToFileURL`;
  `USERPROFILE`, `COMSPEC`, `PATHEXT`; subtestes do `spawn` aguardados;
  `parity-kinds` e `for-released` sem o `PATH` do host.

## O que verificar com atenção

1. Em macOS/Linux, alguma saída do CLI mudou (mensagens, caminhos,
   goldens)? `normalizeGitPath` com `path.normalize` em POSIX tira `/`
   final ou `.`/`..` de algum caminho que antes era mostrado cru?
2. Algum `t.skip` novo roda em macOS/Linux como usuário comum? (não
   deveria: só root, só Windows, ou só sem privilégio de symlink).
3. `excludeEntry` com `\` em POSIX: um nome de diretório com `\`
   legítimo em Linux muda de significado?
4. Os testes novos pegam as mutações que declaram.

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test` nos arquivos que o
diff toca (não a suíte inteira); sondas em `/tmp` com `HOME` temporário.
Antes de afirmar que algo falha, rode e cite a saída.

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
