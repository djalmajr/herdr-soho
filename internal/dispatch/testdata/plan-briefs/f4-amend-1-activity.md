# Emenda 1 — F4: atividade conta da última mudança observada, não da leitura

## Goal

Corrigir a definição de atividade dos itens 1 e 2: ler uma tela agora não
prova atividade. A idade conta da **última mudança do hash normalizado**
observada entre duas sondagens, nunca da primeira leitura nem da sondagem
mais recente.

## Decisions already made

- O hash é o do detector de travamento (`cksumField(normalizeScreen(tela))`):
  contadores e glifos de progresso **não** contam como mudança.
- Novo marcador em `<state>/wait/<agent>.activity-at` (segundos Unix,
  uma linha), gravado **só pelo `wait`**, no mesmo ponto em que o detector
  compara o hash novo com `stuck-hash`:
  - existe `stuck-hash` anterior **e** o hash novo é diferente → grava
    `activity-at = agora` (mudança real observada);
  - não existe `stuck-hash` anterior (primeira observação) → **não** grava
    `activity-at` (a tela foi só lida);
  - hash igual → não mexe em `activity-at`.
  - O dispatch que limpa os marcadores de espera (`dispatch.mjs:~945`,
    lista de sufixos) passa a limpar também `activity-at` — ou seja, uma
    tarefa nova começa sem atividade conhecida. Este é o único trecho de
    `dispatch.mjs` que você pode tocar.
- `activityAgeSeconds(sd, agent, screenText, nowS)` passa a ser:
  - `0` quando existe `stuck-hash` e o hash da tela atual é diferente dele
    (mudança real desde a última sondagem registrada);
  - `nowS - activityAt` quando o hash é igual e `activity-at` é inteiro
    positivo;
  - `null` em qualquer outro caso (sem `stuck-hash`, sem `activity-at`,
    valor inválido).
  `stuck-since` deixa de ser usado para atividade (continua só no
  detector de travamento).
- "Ativo" (item 1) e `activity_s` (item 2) usam essa função; o resto dos
  itens não muda.

## Expected result

Uma tela estática lida pela primeira vez agora não vira checkpoint ativo;
só uma mudança real de hash observada dentro da janela torna o worker ativo.

## Acceptance criteria

1. Fixture "só leitura": worker `working`, tela estática desde o início,
   sem marcadores prévios; dois `wait --timeout` curtos seguidos → exit 9,
   `"checkpoint":false`, `activity_age_s: null` no primeiro e no segundo,
   e o `friction.log` ganha a linha `timeout waiting` — mesmo a tela tendo
   sido lida nas duas esperas.
2. Fixture "mudança real": a tela muda (texto, não só contador) entre
   sondagens do mesmo `wait` → `activity-at` gravado, `"checkpoint":true`,
   sem linha no `friction.log`.
3. Fixture "só contador": a tela muda apenas em dígitos/glifos de
   progresso → tratada como estática (`"checkpoint":false`).
4. `status`: tela estática com `stuck-hash` igual e sem `activity-at` →
   `activity_s` `-`; com `activity-at` há 600 s → `activity_s` ≥ 600;
   hash diferente do `stuck-hash` → `0`.
5. Cada fixture com `// Mutation captured: …` executado — em especial a
   mutação "gravar `activity-at` também na primeira observação" tem de
   derrubar o critério 1.

## Owned files

Os mesmos do brief da F4, mais `skills/herdr-soho/scripts/lib/dispatch.mjs`
**só** na lista de sufixos limpos por dispatch.

## Forbidden

O resto de `dispatch.mjs`; os demais arquivos proibidos no brief da F4.
No commit, push, tag, or PR. The orchestrator owns git.

## Report

O mesmo relatório da F4, cobrindo o brief e esta emenda, por item
`[done]` / `[partial]` / `[skipped]`, com saídas coladas.
