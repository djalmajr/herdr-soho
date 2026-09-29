# Brief — F8: protocolo de avaliação comparável por papel e kit reproduzível (#5)

Role: implementer · Agent: build · Report language: pt-BR

Worktree própria; caminhos relativos à raiz dela. Issue:
https://github.com/djalmajr/herdr-soho/issues/5 (resumo: relatos de uso real
misturam briefs, riscos e tempos e não sustentam ranking; queremos mesmo
brief, fixtures congeladas, cópias isoladas, limites iguais e avaliação por
sondas independentes, com métricas e rubricas por papel **definidas antes**
de rodar modelos; o piloto separa observação de inferência; recomendações
dizem o tamanho da amostra e as limitações, sem ranking por estatística
heterogênea).

## Goal

Entregar o protocolo e um kit executável para **dois** papéis (implementer e
reviewer), prontos para o orquestrador rodar o piloto. Você **não** roda
modelos nem o piloto.

## Decisions already made

1. `docs/evaluation-protocol.md` (inglês, tom dos outros docs), seções nesta
   ordem: Purpose; What a comparable run is (mesmo brief byte a byte,
   fixture congelada por manifesto sha256, cópia isolada por execução fora
   do repositório, mesmo timeout, mesmo esforço declarado, papel sozinho —
   sem emendas, ou emendas contadas); Roles and rubrics — uma subseção por
   papel: `implementer`, `reviewer`, `scouter/researcher`, `documenter`,
   `specialist` (security-reviewer, designer), cada uma com **métricas**
   (nome, definição exata, como medir, unidade) e **rubrica** (níveis 0–3
   com critério observável). Mínimos:
   - implementer: sondas ocultas aprovadas (x/N), violação de escopo
     (arquivo fora de Owned modificado: sim/não, por manifesto), relatório
     honesto (itens `[done]` que as sondas reprovam), tempo até o primeiro
     patch e até o relatório (min, do timestamp do dispatch e do mtime do
     relatório/primeira escrita observada), emendas necessárias, custo só
     quando o painel mostra tokens (senão "não observável").
   - reviewer: recall de defeitos semeados (x/N), precisão (achados que
     não correspondem a defeito real), calibração de severidade, evidência
     executada vs inferida, tempo até o relatório.
   - scouter/researcher: acerto contra gabarito de fatos, citação da função
     de consulta (regra da skill), tempo.
   - documenter: afirmações conferidas contra o código (gabarito), flags ou
     nomes inventados (contagem).
   - specialist: recall/precisão de vulnerabilidades semeadas.
   Depois: Procedure (passo a passo), Recording (formato do registro — o JSON
   do item 3), Reporting rules (observação vs inferência, amostra n,
   limitações, proibido ranking a partir de agregados heterogêneos do
   `stats`), Pilot scope (só implementer e reviewer neste primeiro ciclo;
   os outros papéis ficam definidos e sem piloto).
2. Kit em `evals/` (fora de `skills/`, não é distribuído com a skill):
   - `evals/README.md`: como montar uma execução e rodar as sondas.
   - `evals/fixtures/implementer-prune/`: projeto Node mínimo, sem
     dependências, com `src/prune.mjs` (função a implementar:
     `pruneBackups({ dir, keep, now })` — apaga os backups mais antigos
     além de `keep`, nunca o mais recente, e é segura a crash/retry/relógio
     regressivo), `brief.md` no formato do template da skill (com a seção
     `Failure matrix` e os marcadores `[crash]`, `[retry]`, `[clock]`),
     `MANIFEST.sha256` (sha256 de cada arquivo entregue ao worker), e
     `probes/` com as sondas ocultas (`node:test`) que **não** vão para a
     cópia do worker. O brief define nomes e formatos sem ambiguidade
     (formato dos arquivos de backup, o que é "mais recente", o que conta
     como crash).
   - `evals/fixtures/reviewer-seeded/`: um diff pequeno (`change.diff`) sobre
     um arquivo base (`base/`) com **exatamente 3** defeitos semeados de
     severidades diferentes, `brief.md` de revisão, e `ANSWER-KEY.md`
     (fora da cópia do revisor) com cada defeito: `arquivo:linha`,
     severidade esperada, cenário que o prova.
   - `evals/prepare.mjs <fixture> <dest>`: copia a fixture para `<dest>`
     (fora do repositório; recusa `<dest>` dentro dele), **sem** `probes/`
     nem `ANSWER-KEY.md`, confere o `MANIFEST.sha256` e grava
     `<dest>/.eval-run.json` com `{fixture, prepared_at, manifest_ok}`.
   - `evals/run-probes.mjs <fixture> <dest>`: roda as sondas ocultas contra
     `<dest>` num diretório temporário próprio (nunca dentro de `<dest>`),
     confere violação de escopo pelo manifesto e imprime **um JSON**:
     `{fixture, probes:{passed,total,failed:[nomes]}, scope_violations:[arquivos], manifest_ok}`.
     Exit 0 sempre que conseguiu medir; 2 para uso inválido.
3. Registro de uma execução: `evals/results/<AAAA-MM-DD>-<fixture>-<rotulo>.json`
   com `{fixture, role, kind, model, effort, brief_sha256, dispatched_at,
   first_patch_at|null, report_at|null, amendments, probes|null,
   review:{seeded, found, false_positives, severity_ok}|null,
   cost:"not observable"|{…}, observations:[…], inferences:[…]}` — descreva
   o formato no protocolo; não crie resultados (o piloto é do orquestrador).
4. Testes em `evals/test/kit.test.mjs` (`node:test`, `spawnSync` com
   `timeout`): `prepare` recusa destino dentro do repositório, não copia
   sondas nem gabarito, detecta manifesto adulterado; `run-probes` numa
   cópia com a solução de referência (em `evals/test/fixtures/reference-prune.mjs`)
   passa todas as sondas, numa cópia com o esqueleto original reprova, e
   aponta violação de escopo quando um arquivo fora do Owned muda.
   Cada teste com `// Mutation captured: …` executado.

## Expected result

Protocolo completo com métricas e rubricas por papel; kit que prepara uma
cópia isolada e mede uma execução de implementer por sondas ocultas; fixture
de reviewer com gabarito; testes verdes.

## Acceptance criteria

1. `node --test evals/test/` e `bun test evals/test/` → 0 fail.
2. `node evals/prepare.mjs evals/fixtures/implementer-prune /tmp/<x>` e
   `node evals/run-probes.mjs evals/fixtures/implementer-prune /tmp/<x>` no
   esqueleto → JSON com `passed < total` e `manifest_ok: true` (cole a
   saída).
3. O protocolo tem as cinco subseções de papel, cada uma com métricas e
   rubrica 0–3 (confira com `grep -n '^### ' docs/evaluation-protocol.md`).

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `docs/evaluation-protocol.md` — novo
- `evals/**` — novo

## Forbidden

- Qualquer arquivo em `skills/`, `plugin/`, `README.md`, `AGENTS.md`.
- Rodar modelos, abrir painéis ou despachar agentes.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Sources (local paths, read before editing)

- `skills/herdr-soho/templates/brief.md`, `templates/report.md`
- `skills/herdr-soho/references/agent-profiles.md` (relatos reais, para o
  que medir; não copie rankings)
- `skills/herdr-soho/scripts/lib/commands/stats.mjs` (o que o `stats` já
  mede — e por que agregados não bastam)

## Project rules that apply

- Código e docs em inglês; sem dependências novas; portável.
- Teste de comportamento observável; mutação nomeada e executada.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera.

## Non-goals

Rodar o piloto; ranking de modelos; mudar a skill ou o `stats`.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e mutações
executadas.
