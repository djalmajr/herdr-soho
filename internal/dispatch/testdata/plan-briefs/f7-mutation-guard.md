# Brief — F7: isolamento de build em mutation checks (#7)

Role: implementer · Agent: build-2 · Report language: pt-BR

Worktree própria (o orquestrador informa o caminho na mensagem de envio);
caminhos relativos à raiz dela. O CLI se chama `herdr-soho`
(`skills/herdr-soho/scripts/herdr-soho`).

## Goal

1. (#7) Um mutation check numa cópia descartável não reaproveita nem suja
   os artefatos de build da árvore principal: um comando de guarda recusa
   a cópia antes da mutação quando ela compartilha artefatos, e o papel do
   implementer manda usá-lo.

## Decisions already made

### `mutation-guard` (#7)

- Comando novo `herdr-soho mutation-guard <copy-dir> [--source <dir>] [--env NAME]…`
  em `skills/herdr-soho/scripts/lib/commands/mutation-guard.mjs`,
  registrado em `skills/herdr-soho/scripts/herdr-soho.mjs` (não "living",
  funciona fora do Herdr) e listado em `lib/usage.mjs`.
- `--source` padrão: a raiz do projeto (`projectRoot`). Caminhos
  comparados por `realpath`; "dentro de" = igual ou descendente.
- Verificações, nesta ordem, cada uma imprime em stdout `ok <nome>` ou
  `fail <nome>: <detalhe>`:
  1. `copy-outside-source`: a cópia não está dentro da fonte e a fonte não
     está dentro da cópia.
  2. `no-symlink-into-source`: percorre a cópia sem seguir links e sem
     entrar em `.git`; todo symlink cujo alvo resolvido está dentro da
     fonte falha (`symlink <relativo> -> <alvo>`); link quebrado é ignorado.
  3. `build-env`: para `CARGO_TARGET_DIR`, `CARGO_BUILD_TARGET_DIR` e cada
     `--env NAME`, se a variável está definida com caminho absoluto dentro
     da fonte → `fail build-env: <NAME> points into the source tree`
     (não imprima o valor).
  4. `cargo-config`: em `<copy>/.cargo/config.toml` e `<copy>/.cargo/config`,
     uma linha `target-dir = "<caminho>"` com caminho absoluto dentro da
     fonte → `fail cargo-config: <arquivo relativo> sets target-dir inside the source tree`.
- Exit 0 se tudo `ok`; 1 se algum `fail`; 2 para uso inválido (sem
  `<copy-dir>`, cópia inexistente ou não diretório, `--env`/`--source` sem
  valor ou com outro flag no lugar). Não escreve, não apaga, não executa
  build.
- `skills/herdr-soho/roles/implementer.md`, no bullet do mutation check
  (linha 21): acrescente, em inglês, que a cópia tem a própria saída de
  build (por exemplo `CARGO_TARGET_DIR=<copy>/target`), que
  `herdr-soho mutation-guard <copy>` roda antes da mutação e que, se falhar,
  não se muta; e que limpar cache compartilhado ou a saída de build da
  árvore principal nunca é passo automático de recuperação — relate.
  A regra vale só quando o brief pede mutation checks.
- `skills/herdr-soho/references/troubleshooting.md`: nova entrada logo
  depois de "A mutation check in the shared tree breaks another worker's
  tests", no mesmo formato (Symptom / Cause / Now / Do), sobre a cópia que
  reaproveitou o target da árvore principal.

## Expected result

`mutation-guard` recusa cópias que
compartilham artefatos e aceita cópias isoladas.

## Acceptance criteria

1. `skills/herdr-soho/scripts/test/mutation-guard.test.mjs`: fixture com
   `target` symlink para a fonte → exit 1 **antes** de qualquer mutação;
   fixture com `CARGO_TARGET_DIR` dentro da fonte → exit 1; `.cargo/config.toml`
   com `target-dir` na fonte → exit 1; cópia isolada → exit 0; e um fluxo
   completo de mutação na cópia isolada (edita um arquivo da cópia, "builda"
   escrevendo em `<copy>/target`) deixa os sha256 dos arquivos e do
   diretório de build da fonte iguais antes e depois.
2. Cada teste com `// Mutation captured: …` executado (cite comando e saída).
   `node --test` e `bun test` de
   `mutation-guard.test.mjs parity-entry.test.mjs roles.test.mjs`
   → 0 fail. O golden `parity-entry` muda **só** pela linha nova do
   `mutation-guard` no help: regrave apenas ele com
   `HERDR_SOHO_GOLDEN=update node --test skills/herdr-soho/scripts/test/parity-entry.test.mjs`
   e cole o trecho do diff. Qualquer outro golden: pare e marque `[partial]`.

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `skills/herdr-soho/scripts/lib/commands/mutation-guard.mjs` — novo
- `skills/herdr-soho/scripts/herdr-soho.mjs` — só o registro do comando
- `skills/herdr-soho/scripts/lib/usage.mjs` — só a linha do comando
- `skills/herdr-soho/roles/implementer.md` — só o bullet do mutation check
- `skills/herdr-soho/references/troubleshooting.md` — só a entrada nova
- `skills/herdr-soho/scripts/test/mutation-guard.test.mjs` — novo
- `skills/herdr-soho/scripts/test/golden/parity-entry.json` — só pela linha do help

## Forbidden

- `dispatch.mjs`, `wait.mjs`, `collect.mjs`, `status.mjs`, `stats.mjs`;
  `SKILL.md`, README, docs (o orquestrador documenta — proponha o texto).
- Outros goldens.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.
- Nunca imprimir valores de variáveis de ambiente nem linhas de comando de
  processos.

## Sources (local paths, read before editing)

- `skills/herdr-soho/roles/implementer.md`
- `skills/herdr-soho/references/troubleshooting.md:485-506`

## Project rules that apply

- Código, mensagens, papel e referência em inglês; comentários na densidade
  dos vizinhos.
- Teste de comportamento observável; mutação nomeada e executada — numa
  cópia fora da worktree, verificada com o próprio `mutation-guard`.
- Portável (`path`, `realpath`, sem `readlink -f`); sem dependências novas.

## Checks you may run

Os do critério 3 e `node --test <um arquivo>` enquanto itera. Não rode a
suíte completa nem a matriz; não rode formatador.

## Non-goals

Detectar outros sistemas de build além dos listados; limpar caches.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas (nunca redigitadas),
as mutações executadas e uma seção "Texto proposto para SKILL.md".
