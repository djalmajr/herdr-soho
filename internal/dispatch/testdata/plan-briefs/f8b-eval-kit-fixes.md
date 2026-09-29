# Brief — F8b: sondas, gabarito e rubrica do kit de avaliação (achados da R5)

Role: implementer · Agent: build · Report language: pt-BR

Worktree própria na branch `feat/eval-protocol`; caminhos relativos à raiz
dela. A revisão R5 achou buracos no kit; as correções abaixo estão
decididas.

## Goal

As sondas ocultas reprovam as variações quebradas que hoje passam, o
gabarito do revisor tem cenários executáveis na ordem real das checagens, e
as rubricas não deixam resultados sem escore.

## Decisions already made

1. `evals/fixtures/implementer-prune/probes/prune.test.mjs`:
   - no teste de sequência duplicada (linhas ~85–100), **depois** do laço de
     argumentos inválidos, chame `pruneBackups({ dir, keep: 1, now })` com
     argumentos válidos e exija rejeição (`assert.rejects`) com os dois
     nomes duplicados ainda no diretório;
   - no teste de entradas que não são backup (linhas ~66–83), exija que o
     arquivo `.tmp` continue existindo e que o symlink com nome de backup
     continue sendo symlink (`fs.lstatSync(...).isSymbolicLink() === true`).
2. `evals/fixtures/reviewer-seeded/ANSWER-KEY.md`:
   - defeito 3: o cenário usa um ator **não-dono** (o dono é rejeitado na
     linha 4 antes do crédito): `amount = 1.6` debita `1.6` e credita `2`;
   - defeito 2: o parêntese diz que a checagem de valor (linha 3) corre
     primeiro, que `0` passa por ela, e que só então o defeito 1 rejeita o
     dono, antes dos lançamentos.
   Local, mecanismo e severidade dos três defeitos não mudam.
3. `docs/evaluation-protocol.md`, rubricas:
   - reviewer, escore 2: "at least half of the seeded defects found and
     false positives do not outnumber the hits, but a seeded defect is
     missing, a false positive remains, a severity does not match, or a
     claimed execution is not supported";
   - specialist, escore 2: o mesmo acréscimo de falso positivo que não
     supera os acertos.
   Nenhuma outra linha do protocolo muda.
4. Testes do kit em `evals/test/kit.test.mjs`: dois testes novos provam que
   as sondas reprovam (a) uma variação da referência que apaga as duplicadas
   com argumentos válidos e (b) uma que apaga `.tmp` e symlinks; a
   referência continua 6/6 e o esqueleto continua reprovando. Cada teste com
   `// Mutation captured: …` executado. As variações ficam em
   `evals/test/fixtures/` como arquivos novos.

## Expected result

`run-probes` reprova as duas variações quebradas; a referência passa tudo;
gabarito e rubricas atualizados.

## Acceptance criteria

1. `node --test evals/test/` e `bun test evals/test/` → 0 fail (cole).
2. `node evals/prepare.mjs evals/fixtures/implementer-prune /tmp/<x>`, copie
   cada variação para `/tmp/<x>/src/prune.mjs` e rode
   `node evals/run-probes.mjs evals/fixtures/implementer-prune /tmp/<x>` →
   `passed < total` para as duas variações e `6/6` para a referência (cole
   os JSON).
3. `grep -n "false positive" docs/evaluation-protocol.md` mostra os dois
   escores 2 novos.

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `evals/fixtures/implementer-prune/probes/prune.test.mjs`
- `evals/fixtures/reviewer-seeded/ANSWER-KEY.md`
- `docs/evaluation-protocol.md` — só os dois escores 2
- `evals/test/kit.test.mjs`, `evals/test/fixtures/*` (novos arquivos de variação)

## Forbidden

- `evals/results/*` (o orquestrador atualiza), `evals/fixtures/*/brief.md`,
  `src/`, `MANIFEST.sha256`, `base/`, `change.diff` (o conteúdo entregue ao
  worker não muda), tudo fora de `evals/` e `docs/evaluation-protocol.md`.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e mutações
executadas.
