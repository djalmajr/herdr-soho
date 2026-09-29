# Brief — Revisão R-B4: lint do brief em pt-BR e itens de exclusão na posse

Role: reviewer · Agent: review · Report language: pt-BR

## Goal

Dizer se o commit `f021af2` (branch `fix/brief-lint-ptbr`, sobre `main`
`6b764d9`) pode ir à `main`: aceita títulos pt-BR sem abrir buracos no
lint, e itens de exclusão deixam de dar posse sem esconder posse real.

Seu cwd está em `f021af2`. Confirme com `git log --oneline -1` e revise
`git diff HEAD~1 HEAD`. Contrato:
`/work/herdr-soho/.herdr-soho/w14/plan-briefs/b4-brief-lint.md`.
Relatório do implementador (Codex, que terminou o que um pi começou):
`/tmp/herdr-soho/w14/reports/build-2-20260928T173143.md`.

## O que verificar com atenção

1. **Falso "sem posse".** Um item legítimo de posse que começa por uma das
   palavras de exclusão deixa de contar? Exemplos: `- No arquivo
   \`a/b.ts\`, adicione…` (pt-BR "No" = "em o"), `- Sem mudar a API,
   edite \`x.ts\``, `- Fora de testes: \`src/y.ts\``. A lista de palavras
   da decisão 1 (`no`, `sem`, `fora`) casa com esses começos? Qual o
   efeito prático (aviso de sobreposição perdido)? Proponha, sem decidir.
2. **Títulos pt-BR amplos demais.** `Arquivos` e `Escopo` como prefixo:
   um título como `## Arquivos proibidos` ou `## Escopo fora` passa a ser
   lido como seção de posse (e os caminhos proibidos viram posse)? `Meta`
   casa com `## Metadados`?
3. Comparação sem acentos também nos aliases configurados: algum alias
   existente muda de comportamento?
4. Testes enfraquecidos? Mutações declaradas que não pegam (rode três em
   cópias em `/tmp`)?

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test --timeout 60000` em
`dispatch.test.mjs` e nos testes de lint/aliases; mutações em cópias em
`/tmp`.

## Forbidden

- Editar qualquer arquivo do checkout. Revisor é somente leitura.
- Nenhuma chamada ao Herdr real que escreva: `herdr` falso no `PATH` e
  `HERDR_SOCKET_PATH` inexistente.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
