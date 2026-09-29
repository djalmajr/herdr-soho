# Emenda — B4b: correções da revisão (exclusão ambígua e títulos amplos)

Role: implementer · Agent: build · Report language: pt-BR

## Goal

Fechar os três achados da revisão do B4 sem abrir outro. Mesmo worktree
(`/work/herdr-soho/.worktrees/build-2`),
branch `fix/brief-lint-ptbr`, agora no commit `f021af2` (o seu trabalho
foi commitado). Revisão:
`/tmp/herdr-soho/w14/reports/soho-grok-r12-20260928T174500.md`.

## Decisions already made

1. **Achado 1.** Tire `no`, `sem` e `fora` da lista de palavras de
   exclusão (são começo comum de frase de posse em pt-BR, e `No` inglês
   colide com o pt-BR). Ficam: `nenhum`, `nenhuma`, `não`, `nunca`,
   `exceto`, `not`, `never`, `none`, `except`, `excluding`, `outside`.
2. **Achados 2 e 3.** Os títulos pt-BR embutidos só valem quando, depois
   do prefixo, o resto do título (sem espaços à esquerda) está vazio ou
   começa por um caractere que não é letra nem dígito (ex.: `## Arquivos`,
   `## Arquivos — donos`, `## Escopo:`, `## Meta` valem; `## Arquivos
   proibidos`, `## Escopo fora`, `## Metadados`, `## Metas` não). Vale no
   lint e na leitura da seção de posse. Os títulos em inglês de hoje e os
   aliases configurados em `brief_lint_aliases` mantêm o comportamento
   atual (prefixo simples).

## Expected result

- Testes com as sondas da revisão: `- No arquivo \`ok.ts\``, `- Sem mudar
  a API, edite \`x.ts\``, `- Fora de testes: \`src/y.ts\`` → caminhos
  **dentro**; `- Nenhum \`z.ts\`` → fora; `## Arquivos proibidos` antes da
  seção de posse real → os caminhos proibidos **não** entram e a posse
  real entra; `## Escopo fora` idem; `## Metadados`/`## Metadata` não
  satisfazem Goal; `## Meta` satisfaz. Cada um com `// Mutation
  captured: …` executado (cole a saída vermelha).
- `node --test` e `bun test --timeout 60000` de `dispatch.test.mjs` → 0
  fail (cole).
- Atualize as frases de `SKILL.md`/`docs/guide.md` se citarem a lista ou
  a regra de prefixo.

## Owned files

Os do contrato do B4 (`skills/herdr-soho/scripts/lib/dispatch.mjs`, os
testes dele, `SKILL.md` e `docs/guide.md` só nas frases do B4).

## Forbidden

- Todo o resto e qualquer outro worktree. Herdr real que escreva.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e as mutações.
