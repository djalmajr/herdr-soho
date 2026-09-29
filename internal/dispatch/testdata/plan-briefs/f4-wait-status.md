# Brief — F4: checkpoints neutros no wait (#3) e idades no status (#4)

Role: implementer · Agent: build · Report language: pt-BR

Worktree própria (o orquestrador informa o caminho na mensagem de envio);
caminhos relativos à raiz dela. O CLI se chama `herdr-soho`
(`skills/herdr-soho/scripts/herdr-soho`). Issues:
https://github.com/djalmajr/herdr-soho/issues/3 e /4 (textos resumidos abaixo;
não precisa abri-las).

## Goal

1. (#3) Um `wait --timeout` curto que vence enquanto o worker está
   `working` e com atividade observável é um **checkpoint neutro**: não
   escreve no friction log. Timeout de worker parado continua sinalizando.
2. (#4) `status` mostra a duração da tarefa atual e a idade da última
   mudança de tela observada, sem nenhum conteúdo do terminal.

## Decisions already made

### Sinal de atividade (compartilhado pelos dois itens)

- Fonte única: os marcadores que o detector de travamento já grava em
  `<state>/wait/<agent>.stuck-hash` e `<agent>.stuck-since`
  (`lib/wait.mjs:320-350`), com o mesmo hash de tela
  (`cksumField(normalizeScreen(tela visível))`).
- Extraia uma função exportada em `lib/wait.mjs`:
  `activityAgeSeconds(sd, agent, screenText, nowS)` que devolve:
  - `null` quando `stuck-hash` ou `stuck-since` não existe, está vazio ou
    `stuck-since` não é inteiro positivo;
  - `0` quando o hash da tela atual difere de `stuck-hash` (mudou desde a
    última sondagem);
  - `nowS - stuckSince` quando o hash é igual.
  Não escreve arquivo nenhum.
- "Ativo" = estado `working` **e** `activityAgeSeconds` não nulo **e**
  menor que a janela de travamento (`stuck_warn_minutes` × 60; quando a
  chave é `0` ou não numérica, 20 × 60).

### Item 1 — `wait` (#3)

- No ramo de timeout (`lib/wait.mjs:~716-731`), para cada agente pendente:
  - JSON: acrescente, **depois** de `state`, as chaves
    `checkpoint` (`true` se ativo, senão `false`) e `activity_age_s`
    (número ou `null`). Nada mais muda na linha.
  - Ativo: **não** chame `warn(...)` (que grava friction). Escreva só em
    stderr, sem friction:
    `herdr-soho: checkpoint: '<a>' is still working (screen changed <N>s ago); wait again: herdr-soho wait <a> --timeout <tm>`
    (sem ` --timeout <tm>` quando o timeout não é numérico).
  - Não ativo: exatamente o `warn(...)` de hoje (texto inalterado).
- Exit continua 9 em ambos os casos; o relatório continua sendo o único
  sinal de conclusão.

### Item 2 — `status` (#4)

- Toda linha TSV de `status` ganha duas colunas no fim, nesta ordem:
  `task_s` e `activity_s` — inteiros em segundos, ou `-` quando
  desconhecido. As três primeiras colunas não mudam.
- `task_s`: início = `mtime` de `<state>/last-report-<agent>` (gravado a
  cada dispatch, `lib/dispatch.mjs:943`); fim = `mtime` do relatório
  quando ele existe e não está vazio, senão agora. `-` sem
  `last-report-<agent>`.
- `activity_s`: só quando o estado consultado é `working`: o valor de
  `activityAgeSeconds` com a tela visível lida uma vez; `-` em qualquer
  outro estado ou quando a função devolve `null`. `status` continua
  somente leitura (não grava marcadores).
- As linhas JSON que `status` já emite (quota, provider-error, capacity,
  question, not-received) ganham `task_s` e `activity_s` (número ou
  `null`) como **últimas** chaves.
- Nenhum texto de tela vai para a saída.

## Expected result

`wait` com worker ativo termina em 9 sem linha nova no `friction.log`;
com worker parado, termina em 9 com a linha de hoje. `status` imprime
`agent<TAB>state<TAB>report<TAB>task_s<TAB>activity_s`.

## Acceptance criteria

1. Fixture ativa: dois `wait --timeout` curtos seguidos com a tela
   mudando entre sondagens → exit 9, JSON com `"checkpoint":true`,
   `friction.log` sem linha `timeout waiting` — provado por teste em
   `skills/herdr-soho/scripts/test/wait-checkpoint.test.mjs`.
2. Fixture parada (tela igual por mais que a janela, `stuck-since`
   antigo) → exit 9, `"checkpoint":false`, uma linha `timeout waiting`
   no `friction.log` — mesmo arquivo de teste.
3. `status`: fixture de atividade (hash diferente → `activity_s` 0) e
   fixture de estagnação (hash igual, `stuck-since` há 600 s →
   `activity_s` ≥ 600) dão idades diferentes; `task_s` com e sem relatório;
   `-` sem telemetria — em
   `skills/herdr-soho/scripts/test/status-ages.test.mjs`.
4. Use os fakes existentes (`skills/herdr-soho/scripts/test/fakes.mjs`,
   o herdr falso dos testes de `wait`/`status`). Cada teste com
   `// Mutation captured: …` executado (cite comando e saída).
5. Goldens: `parity-wait` e `parity-status` vão mudar só pelas colunas/
   chaves novas. Regrave apenas esses dois com
   `HERDR_SOHO_GOLDEN=update node --test skills/herdr-soho/scripts/test/parity-wait.test.mjs skills/herdr-soho/scripts/test/parity-status.test.mjs`
   e cole no relatório `git diff --stat` e um trecho do diff mostrando que
   a única mudança são as colunas/chaves novas.
6. `node --test` e `bun test` dos arquivos
   `wait.test.mjs status.test.mjs wait-checkpoint.test.mjs status-ages.test.mjs parity-wait.test.mjs parity-status.test.mjs`
   → 0 fail; `skills/herdr-soho/scripts/run-tests.sh --env outside test-status.sh test-quota.sh`
   → PASS (se o `test-status.sh` depender das 3 colunas, ajuste o teste
   para as 5 colunas e diga no relatório).

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `skills/herdr-soho/scripts/lib/wait.mjs`
- `skills/herdr-soho/scripts/lib/commands/status.mjs`
- `skills/herdr-soho/scripts/test/wait-checkpoint.test.mjs` — novo
- `skills/herdr-soho/scripts/test/status-ages.test.mjs` — novo
- `skills/herdr-soho/scripts/test/wait.test.mjs`, `status.test.mjs`, `test-status.sh` — só ajustes de expectativa pelas colunas/chaves novas
- `skills/herdr-soho/scripts/test/golden/parity-wait.json`, `parity-status.json` — só pela regravação do critério 5

## Forbidden

- `lib/dispatch.mjs`, `lib/commands/collect.mjs`, qualquer comando que não
  seja `wait`/`status`; `SKILL.md`, `references/`, `roles/`, `templates/`,
  README, docs (o orquestrador documenta — proponha o texto no relatório).
- Outros goldens.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.
- Nunca imprimir texto de tela, variáveis de ambiente ou linhas de comando
  de processos.

## Sources (local paths, read before editing)

- `skills/herdr-soho/scripts/lib/wait.mjs:130-360`, `:555-787`
- `skills/herdr-soho/scripts/lib/commands/status.mjs` (inteiro)
- `skills/herdr-soho/scripts/test/wait.test.mjs`, `status.test.mjs`, `fakes.mjs`

## Project rules that apply

- Código e mensagens em inglês; comentários na densidade dos vizinhos.
- Teste de comportamento observável; mutação nomeada e executada.
- Nenhum `spawnSync` sem `timeout`.

## Checks you may run

Os do critério 6 e `node --test <um arquivo>` enquanto itera. Não rode a
suíte completa nem a matriz inteira; não rode formatador.

## Non-goals

Caminho estável de relatório entre emendas (#8, outra fatia);
qualquer mudança no contrato de conclusão pelo relatório.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas (nunca redigitadas),
as mutações executadas e uma seção "Texto proposto para SKILL.md" com as
frases que descrevem os campos novos.
