# Brief — Revisão R12: delta depois da R11 (issue #17)

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está em `fix/test-portability` `435e2b4`. Confirme com
`git log --oneline -1` e revise só `git diff 32b44cf HEAD` (dois commits:
`be75df1` e `435e2b4`).

## Goal

Confirmar que seus dois achados da R11 estão resolvidos sem regressão.

## Contrato decidido (verifique que o código cumpre)

- P1 da R11: as expectativas do state dir mostrado usam `/` (`a/b/`,
  `deep/my cache/`, e também as duas de `classifyStateDir` com
  `sub/newstate/` e `deep/cache/`); o `rel` continua nativo.
- P2 da R11: o neto do fake de timeout vive 10 s (acima dos tetos de 5 s e
  8 s); no Windows o fake pai fica 4 s (o probe mata só o `cmd.exe` e o
  node do fake sobrevive — registrado como issue #20) e a limpeza tenta
  por até 20 s.
- Resultado no Windows (Node 24) em `be75df1`: 985 testes, 821 pass,
  0 fail, 164 skip.

## O que verificar

1. Os tetos agora provam "volta no limite, não na morte do neto" (mutação
   que espere o neto falha)?
2. No macOS/Linux a suíte do `setup-probe` não ficou mais lenta além do
   necessário, e nenhum processo sobra depois dela.
3. Alguma outra expectativa de `shown` ainda monta com `path.join`.

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
