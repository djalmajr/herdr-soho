# Brief — F5: lint prévio de brief e checkpoint de gates (#9), matriz de falhas condicional (#2)

Role: implementer · Agent: build-2 · Report language: pt-BR

Worktree própria (o orquestrador informa o caminho na mensagem de envio);
caminhos relativos à raiz dela. O CLI se chama `herdr-soho`
(`skills/herdr-soho/scripts/herdr-soho`).

## Goal

1. (#9) Um comando `lint` roda antes do envio o mesmo diagnóstico de brief
   que o `dispatch` roda, sem criar dispatch nem tocar estado; os papéis
   de edição ganham a regra de checkpoint depois dos gates.
2. (#2) Briefs de fluxos que publicam, retêm, podam ou apagam estado ganham
   uma seção opcional `Failure matrix` com três itens fixos; o lint confere
   a seção quando ela existe; implementer e reviewer seguem a matriz só
   quando a seção existe.

## Decisions already made

### Item 1 — `lint` (#9)

- Refatore em `skills/herdr-soho/scripts/lib/dispatch.mjs` (só a região do
  lint, linhas ~140–265): nova função exportada
  `briefLintFindings(brief, ctx, env, opts)` que devolve
  `{ mode, warnings, missingMessage }`: `warnings` = os textos (sem prefixo)
  que `lintBrief` imprime hoje com `warn(...)`, na mesma ordem (aliases
  ignorados, linhas de código vazio, "… and N more line(s)", e os achados
  da matriz do item 2); `missingMessage` = o texto
  `brief <path> is missing sections: … — …` ou `''`. `lintBrief` passa a
  usá-la e mantém **exatamente** a saída e o exit de hoje (warn → `warn()`;
  strict + seções faltando → `dieFriction(`${missingMessage} (brief_lint=strict)`, 2)`).
- Comando novo em `skills/herdr-soho/scripts/lib/commands/lint.mjs`,
  registrado em `skills/herdr-soho/scripts/herdr-soho.mjs` (não é comando
  "living": funciona fora do Herdr) e listado em `lib/usage.mjs`:
  `herdr-soho lint <brief.md> [--role <role>]`.
  - `--role` padrão `implementer`; `readOnly = !roleIsEdit(role)` (igual ao
    dispatch, `dispatch.mjs:780`). Papel inexistente → stderr
    `herdr-soho: lint: unknown role '<r>'`, exit 3. Sem argumento →
    `usage: lint <brief.md> [--role <role>]`, exit 2. Arquivo inexistente →
    `herdr-soho: lint: brief not found: <path>`, exit 2. `--role` sem valor
    ou com outro flag no lugar → exit 2 com o usage.
  - Saída: cada item de `warnings`, e `missingMessage` se não vazio, em
    stderr como `herdr-soho: warning: <texto>` — o mesmo texto do dispatch.
    **Não** grava friction, não cria diretório de estado, não escreve
    arquivo nenhum.
  - Exit: `brief_lint=off` → stdout `brief <path>: lint off (brief_lint=off)`, exit 0.
    Sem achados → stdout `brief <path>: ok`, exit 0. Strict com
    `missingMessage` → stderr `herdr-soho: <missingMessage> (brief_lint=strict)`, exit 2.
    Outros achados → exit 1.
- Regra de checkpoint depois dos gates, em `roles/implementer.md`,
  `roles/tasker.md` e `roles/designer.md` (um parágrafo curto + exemplo,
  em inglês, mesmo tom dos arquivos):
  "When every check the brief lists passes, stop proving. A failure in a
  file you do not own, or one caused by an external limit (no network, a
  shared harness, a service you may not start), is a `[partial]` item with
  its evidence, not a reason to keep debugging. A test that would widen
  the scope goes to Open questions. Write the report and let the
  orchestrator decide." Exemplo com três linhas rotuladas:
  `required gate` (o check do brief, precisa passar), `optional proof`
  (prova extra que você pode oferecer sem rodar), `[partial] external limit`
  (falha fora do seu controle, com a evidência).

### Item 2 — matriz de falhas condicional (#2)

- Seção opcional nos briefs, cabeçalho de nível 1–3 que começa com
  `Failure matrix` (sem distinção de caixa). Dentro dela (até o próximo
  cabeçalho de nível igual ou maior), três marcadores fixos, em qualquer
  idioma do resto do texto: `[crash]`, `[retry]`, `[clock]`.
- Achado do lint quando a seção existe e falta marcador (um só texto,
  marcadores na ordem crash, retry, clock):
  `brief <path> failure matrix is missing: [clock] — a clock that goes backwards is not covered`
  Motivos: `[crash]` → `a crash between publish and prune/delete is not covered`;
  `[retry]` → `a repeated or retried step is not covered`;
  `[clock]` → `a clock that goes backwards is not covered`; vários motivos
  unidos por `; `. É aviso em `warn` e em `strict` (não é fatal), silenciado
  só por `brief_lint=off`. Brief sem a seção: nenhum achado da matriz.
- `templates/brief.md`: nova seção opcional logo depois de
  "Acceptance criteria":

  ```markdown
  ## Failure matrix (only when the slice publishes, retains, prunes or deletes state)

  <!-- Delete this section for any other slice. -->
  One executable fixture per step boundary of publish → prune/delete → retry:

  - [crash] a crash between publish and prune/delete: what must survive
  - [retry] the same step repeated or retried after a failure: the result converges
  - [clock] the clock goes backwards between runs: nothing still needed is pruned

  Say which items have local proof (fixtures) and which need operational proof.
  ```
- `roles/implementer.md` e `roles/reviewer.md`: regra condicional curta:
  com a seção `Failure matrix` no brief, o implementer escreve uma fixture
  executável por marcador (dados necessários preservados, retry converge)
  e o reviewer sonda cada marcador com uma fixture independente; os dois
  separam prova local de prova operacional. Sem a seção, não montam a
  matriz.
- `references/orchestration-contract.md`, seção "Brief checklist": dois
  bullets novos (matriz de falhas quando o fluxo publica/retém/apaga
  estado; checkpoint depois dos gates).
- Goldens: se `parity-dispatch` mudar só porque o corpo dos papéis mudou,
  regrave apenas ele com
  `HERDR_SOHO_GOLDEN=update node --test skills/herdr-soho/scripts/test/parity-dispatch.test.mjs`
  e cole um trecho do diff que prove isso. Qualquer outra mudança de golden:
  pare e marque `[partial]`.

## Expected result

`herdr-soho lint brief.md` imprime os mesmos avisos que o `dispatch`
imprimiria, sem efeito colateral; briefs com `Failure matrix` incompleta
recebem o aviso da matriz; os papéis e o template carregam as duas regras.

## Acceptance criteria

1. `skills/herdr-soho/scripts/test/lint.test.mjs` (node:test, `spawnSync`
   com `timeout`, `HOME`/estado temporários) prova:
   a) brief incompleto: o texto do aviso do `lint` é igual ao que
      `lintBrief` emite no dispatch para o mesmo arquivo; nenhum arquivo
      criado no diretório de estado nem `friction.log`;
   b) exemplo realista de retenção (fixture
      `skills/herdr-soho/scripts/test/fixtures/briefs/retention.md`: publicar
      backup diário e podar mantendo 7 dias, em pt-BR, com a seção sem
      `[clock]`) → aviso da matriz com `[clock]`; a mesma fixture com os três
      marcadores → sem aviso da matriz;
   c) brief comum sem retenção (`fixtures/briefs/plain.md`) → nenhum aviso
      da matriz;
   d) `--role reviewer` não exige `Owned files`; exits 0/1/2/3 dos casos
      acima.
   Cada teste com `// Mutation captured: …` executado (cite comando e saída).
2. `node --test` e `bun test` de
   `lint.test.mjs dispatch.test.mjs parity-dispatch.test.mjs parity-entry.test.mjs roles.test.mjs`
   → 0 fail.
3. `skills/herdr-soho/scripts/run-tests.sh --env outside test-friendly.sh`
   → PASS.

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `skills/herdr-soho/scripts/lib/dispatch.mjs` — só a região do lint (~140–265)
- `skills/herdr-soho/scripts/lib/commands/lint.mjs` — novo
- `skills/herdr-soho/scripts/herdr-soho.mjs` — só o registro do comando
- `skills/herdr-soho/scripts/lib/usage.mjs` — só a linha do `lint`
- `skills/herdr-soho/roles/implementer.md`, `roles/tasker.md`, `roles/designer.md`, `roles/reviewer.md`
- `skills/herdr-soho/templates/brief.md`
- `skills/herdr-soho/references/orchestration-contract.md` — só a "Brief checklist"
- `skills/herdr-soho/scripts/test/lint.test.mjs`, `scripts/test/fixtures/briefs/*.md` — novos
- `skills/herdr-soho/scripts/test/golden/parity-dispatch.json` — só se o critério permitir

## Forbidden

- `lib/wait.mjs`, `lib/commands/status.mjs`, o resto de `dispatch.mjs`
  (fora da região do lint), `SKILL.md`, README, docs (o orquestrador
  documenta — proponha o texto no relatório).
- Outros goldens.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Sources (local paths, read before editing)

- `skills/herdr-soho/scripts/lib/dispatch.mjs:120-265`, `:770-785`
- `skills/herdr-soho/scripts/herdr-soho.mjs` (despacho de comandos)
- `skills/herdr-soho/scripts/lib/usage.mjs`
- `skills/herdr-soho/roles/implementer.md`, `reviewer.md`
- `skills/herdr-soho/templates/brief.md`
- `skills/herdr-soho/references/orchestration-contract.md` ("Brief checklist")
- `skills/herdr-soho/scripts/test/dispatch.test.mjs` — padrão de teste do lint

## Project rules that apply

- Código, mensagens, papéis e template em inglês; fixtures de brief em
  pt-BR (como os briefs reais); densidade de comentários dos vizinhos.
- Teste de comportamento observável; mutação nomeada e executada.
- Portável; sem dependências novas.

## Checks you may run

Os dos critérios e `node --test <um arquivo>` enquanto itera. Não rode a
suíte completa nem a matriz inteira; não rode formatador.

## Non-goals

Detectar automaticamente fluxos de retenção pelo texto do brief; mudar o
contrato de relatório; mexer em `wait`/`status`.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas (nunca redigitadas),
as mutações executadas e uma seção "Texto proposto para SKILL.md".
