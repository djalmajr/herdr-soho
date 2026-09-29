# Brief — Revisão R1c: migração do bloco legado no lugar, mantendo o texto

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está na branch `feat/rename-herdr-soho` (topo: a mudança). Revise
só `git diff HEAD~1 HEAD`.

## Goal

Achados ancorados em `arquivo:linha`, com execução, antes do merge da PR #11.

## Mudança decidida (verifique que o código cumpre)

Motivo: projetos personalizaram o texto dentro do bloco `herdr-agents`
(regras próprias); regenerar o bloco na migração apagava essas linhas.

- `setupBlockResult`: arquivo com **só** o marcador legado → os marcadores
  e todo `herdr-agents` / `HERDR_AGENTS` **dentro** do bloco são renomeados
  e o resto do texto do bloco fica; fora do bloco nada muda; bloco legado
  sem marcador de fim → `null` (setup recusa, arquivo intocado).
- Com os dois tipos de marcador, o comportamento anterior (o legado lido
  como o atual, bloco regenerado) continua.
- Um `setup` seguinte (já com marcadores novos) regenera o bloco como sempre.
- Mensagens do `doctor` e textos de SKILL.md, README e guia descrevem isso.

## O que verificar

1. Texto fora do bloco que menciona `herdr-agents` não muda; bloco com
   CRLF, bloco sem newline final, dois blocos legados no mesmo arquivo.
2. `setup --local` com bloco legado em `CLAUDE.local.md` e `setup --target
   CLAUDE.md` com `CLAUDE.md` separado seguem o mesmo caminho.
3. Hooks antigos continuam trocados; hooks próprios ficam.
4. O `doctor` depois da migração não emite `legacy herdr-agents instruction block`.

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test` em
`skills/herdr-soho/scripts/test/`; sondas em `/tmp` com `HOME` temporário.
Antes de afirmar que algo falha, rode e cite a saída.

## Forbidden

- Editar qualquer arquivo. Revisor é somente leitura.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
