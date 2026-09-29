# Brief — Revisão R5: protocolo de avaliação, kit e piloto (#5)

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está na branch `feat/eval-protocol`. Revise só
`git diff fix/mutation-guard...HEAD` (dois commits: o protocolo + kit e o
registro do piloto). Issue: https://github.com/djalmajr/herdr-soho/issues/5.

## Goal

Achados ancorados em `arquivo:linha`, com execução, sobre correção do kit e
coerência entre protocolo, kit e resultados, antes do push.

## Contrato decidido (verifique que o conteúdo cumpre)

- `docs/evaluation-protocol.md`: métricas e rubricas 0–3 por papel
  (implementer, reviewer, scouter/researcher, documenter, specialist)
  definidas antes de rodar modelos; regras de registro (JSON) e de relato
  (observação separada de inferência, `n`, limitações, sem ranking a partir
  de agregados heterogêneos do `stats`).
- `evals/prepare.mjs`: recusa destino dentro do repositório, confere o
  manifesto, não copia sondas nem gabarito.
- `evals/run-probes.mjs`: roda as sondas ocultas fora da cópia, mede
  violação de escopo pelo manifesto, imprime um JSON.
- `evals/results/*.json` seguem o formato do protocolo; o `.md` do piloto
  não afirma nada que os JSON não sustentem.

## O que verificar

1. Um jeito de a cópia do worker ver as sondas ou o gabarito, ou de
   `run-probes` executar dentro da cópia/repositório.
2. Sonda oculta que aprova uma implementação errada (rode a sonda contra
   uma variação quebrada da referência, em `/tmp`).
3. Resultados do piloto que contradizem o protocolo (por exemplo, tempos
   derivados de mtime apresentados como observados, ranking implícito).
4. O gabarito do revisor: localização, mecanismo e cenário executáveis.

## Checks you may run

Leitura, `git diff/log/show`; `node --test evals/test/` e
`bun test evals/test/`; `node evals/prepare.mjs …` e
`node evals/run-probes.mjs …` com destinos em `/tmp`. Antes de afirmar que
algo falha, rode e cite a saída.

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
