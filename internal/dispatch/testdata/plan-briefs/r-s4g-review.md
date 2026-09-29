# Brief — Revisão R-S4g: janela de 10 linhas para diálogo de confiança

Role: reviewer · Agent: review · Report language: pt-BR

## Goal

Dizer se o commit `8d8f906` (branch `feat/peer-send`) corrige o falso
diálogo do Cursor sem deixar passar diálogo aberto. Seu cwd está em
`8d8f906`; revise `git diff HEAD~1 HEAD`. Contrato:
`/work/herdr-soho/.herdr-soho/w14/plan-briefs/s4g-dialog-window.md`.
Relatório: `/tmp/herdr-soho/w14/reports/build-20260928T181236.md`.
Validação real já feita pelo orquestrador: com o commit, Cursor aceito e
`idle` recebeu a mensagem; Codex, Cursor e Claude com o diálogo aberto
saíram 17 (antes do commit, com a janela de 20).

## O que verificar com atenção

1. Um diálogo aberto de confiança/confirmação pode ter o padrão acima das
   últimas 10 linhas não vazias? Pense em caixas altas, texto longo que
   quebra, rodapé de várias linhas (o Codex quebrou a pergunta em 3
   linhas e tem 3 linhas de opções/rodapé abaixo). Qual a folga?
2. O fixture do Cursor: sem dado pessoal além de caminho de `/tmp` e
   versão do CLI?
3. Testes e mutações (rode duas).

## Forbidden

- Editar qualquer arquivo. **Nenhum `send` fora dos testes**, nada ao
  Herdr real.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois os achados com severidade, `arquivo:linha`, cenário, evidência.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
