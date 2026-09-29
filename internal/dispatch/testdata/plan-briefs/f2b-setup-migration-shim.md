# Brief — F2b: migração de bloco/hooks antigos e shim do caminho antigo

Role: implementer · Agent: build-2 · Report language: pt-BR

Você trabalha na worktree `.worktrees/f2b` (branch `wip/f2b`) do repositório
herdr-soho. A skill `herdr-agents` acabou de ser renomeada para `herdr-soho`
(commit HEAD desta worktree). Projetos antigos têm, no `AGENTS.md`,
`CLAUDE.md` ou `CLAUDE.local.md`, o bloco entre `<!-- herdr-agents:start -->`
e `<!-- herdr-agents:end -->`, e no `.claude/settings.json` dois hooks cujo
comando procura `…/herdr-agents/scripts/herdr-agents`. Esses projetos não
serão editados agora; a transição acontece pelo lado da skill. Caminhos
relativos à raiz da worktree.

## Goal

1. `herdr-soho setup` (e `setup --local`) substitui no lugar o bloco e os
   hooks antigos pelos novos, e o `doctor` distingue setup novo de setup
   antigo.
2. Um shim transitório, que **não é skill** (sem `SKILL.md`, fora de
   `skills/`), mantém o caminho antigo
   `~/.agents/skills/herdr-agents/scripts/herdr-agents` executando o CLI novo,
   com instalador que instala, verifica e remove.

## Decisions already made

### Item 1 — setup e doctor

- Em `skills/herdr-soho/scripts/lib/setuptext.mjs`, exports novos:
  `LEGACY_SETUP_START = '<!-- herdr-agents:start -->'`,
  `LEGACY_SETUP_END = '<!-- herdr-agents:end -->'`,
  `legacyHookReminder()` e `legacyHookDoctor()`. Os dois retornam
  exatamente `setupHookReminder()` / `setupHookDoctor()` com cada
  `herdr-soho` trocado por `herdr-agents` — escreva-os como literais (não
  derive em tempo de execução). Prova de que o literal está certo
  (texto real dos projetos): sha256 de `legacyHookReminder()` =
  `76ecb6d012d80693694393dbd61fe423132cbe1cad4e805984ca59a479e57f7e`
  (220 caracteres) e de `legacyHookDoctor()` =
  `aeefff35cf1dded4818f047ede0bbd5c3cc2aa0812c378e913dc694a5ae59535`
  (526 caracteres). Um teste afirma esses dois hashes.
- `setupBlockResult` (`setuptext.mjs:~50-70`): uma linha que contém
  `LEGACY_SETUP_START` é tratada exatamente como uma que contém
  `SETUP_START`, e uma que contém `LEGACY_SETUP_END` exatamente como uma que
  contém `SETUP_END`, inclusive na condição de entrada
  (`content.includes(...)`). Resultado: o bloco antigo é trocado pelo novo
  na mesma posição. Arquivo sem marcador antigo: saída byte a byte igual à
  de hoje.
- `settingsHooksResult` (`setuptext.mjs:85`): ao remover entradas
  existentes, também remove a entrada cujo `command` é igual (igualdade
  exata de string) ao legado do mesmo evento
  (`UserPromptSubmit` ↔ `legacyHookReminder()`, `SessionStart` ↔
  `legacyHookDoctor()`). Outras entradas ficam como estão; a nova entra no
  fim, como hoje. Exemplo que casa: o comando legado exato. Que não casa:
  um comando do usuário que só contém a palavra `herdr-agents`.
- `lib/commands/setup.mjs`: `setupTargetExisting` (linha ~44), a checagem
  `had` (~56) e o `hasBlock` de `CLAUDE.md` (~293) aceitam também
  `LEGACY_SETUP_START`. `lib/setuplocal.mjs`: onde houver teste de
  presença de bloco, o mesmo.
- `lib/commands/doctor.mjs`, **somente** as linhas ~798–825 (bloco e hooks)
  e `settingsHasDoctorHook` (~660): 
  - bloco novo presente → linhas `ok` de hoje, sem mudança;
  - só bloco antigo no arquivo alvo → `warn legacy herdr-agents instruction block in <basename>: run '<ENTRY_SCRIPT> setup' to replace it in place`
    (com `setup --local` e `CLAUDE.local.md` quando o bloco antigo está em
    `CLAUDE.local.md` ou `setup_target=local`);
  - hook novo presente → `ok` de hoje; senão, hook doctor legado presente →
    `warn legacy herdr-agents hooks in .claude/settings.json: run '<ENTRY_SCRIPT> setup' to replace them`
    (`setup --local` nas mesmas condições acima); senão os `warn` de hoje.
- Não mude nenhum outro texto do `doctor` nem do `setup`.

### Item 2 — shim transitório (fora da skill)

- Arquivos novos: `compat/legacy-shim/herdr-agents` (POSIX `sh`, modo
  0755), `compat/legacy-shim/herdr-agents.cmd` (Windows),
  `compat/install-legacy-shim.sh` (`bash`, modo 0755) e
  `compat/test/legacy-shim.test.mjs`.
- `compat/legacy-shim/herdr-agents`:
  - `dir` = diretório lógico do script (`CDPATH= cd -- "$(dirname -- "$0")" && pwd`,
    sem `-P`, para funcionar pelo symlink de `~/.claude/skills`).
  - Registra uso em `"$dir/../usage.log"`, melhor esforço (falha de escrita
    nunca interrompe): uma linha `<UTC ISO-8601>\t<$PWD>\t<primeiro argumento ou ->`.
    Nunca grave os demais argumentos.
  - Procura o CLI novo, primeiro que for arquivo:
    `"$dir/../../herdr-soho/scripts/herdr-soho"`,
    `"$HOME/.agents/skills/herdr-soho/scripts/herdr-soho"`,
    `"$HOME/.claude/skills/herdr-soho/scripts/herdr-soho"`;
    executa `HERDR_SOHO_LEGACY_SHIM=1 exec sh "<cli>" "$@"`.
  - Nenhum encontrado: stderr
    `herdr-agents: legacy shim cannot find herdr-soho; install it with: npx skills add djalmajr/herdr-soho --skill herdr-soho -g`
    e exit 2.
- `herdr-agents.cmd`: mesma busca (`%~dp0..\..\herdr-soho\scripts\herdr-soho.cmd`,
  `%USERPROFILE%\.agents\skills\herdr-soho\scripts\herdr-soho.cmd`,
  `%USERPROFILE%\.claude\skills\herdr-soho\scripts\herdr-soho.cmd`), define
  `HERDR_SOHO_LEGACY_SHIM=1`, repassa `%*` e o exit code; sem registro de
  uso; mesma mensagem e exit 2 quando não acha.
- `compat/install-legacy-shim.sh --install|--check|--remove [--force] [--home DIR]`
  (`--home` padrão `$HOME`; `T=$H/.agents/skills/herdr-agents`,
  `L=$H/.claude/skills/herdr-agents`):
  - `--install`: exit 3 com
    `install-legacy-shim: <T>/SKILL.md exists: a managed herdr-agents skill is still installed; remove it first: npx skills remove herdr-agents -g -y`
    se houver `SKILL.md`; exit 3 com
    `install-legacy-shim: herdr-soho is not installed at <H>/.agents/skills/herdr-soho; install it first`
    se faltar `<H>/.agents/skills/herdr-soho/scripts/herdr-soho`. Senão
    cria `<T>/scripts/`, copia os dois arquivos do shim (0755 no `sh`),
    escreve `<T>/SHIM` com uma linha
    `herdr-agents legacy shim (not a skill), installed by herdr-soho compat/install-legacy-shim.sh on <UTC ISO-8601>`,
    e cria `L` como symlink relativo `../../.agents/skills/herdr-agents`
    somente se `<H>/.claude/skills` existe e `L` não existe (se `L` existe e
    não é esse symlink: não toca e avisa em stderr). Idempotente.
  - `--check`: imprime linhas `shim: installed|absent`,
    `skill_md: absent|present`, `cli: <caminho>|missing`,
    `claude_link: ok|absent|other`, `usage_calls: <N>`,
    `usage_last: <data>|none`, e uma linha
    `usage_project: <N> <cwd>` por cwd distinto do `usage.log`
    (ordenado por contagem decrescente). Exit 0 se shim instalado e CLI
    encontrado; 1 senão.
  - `--remove`: exit 3 se não houver `<T>/SHIM` ou houver `<T>/SKILL.md`.
    Imprime o resumo do `--check`; exit 4 com
    `install-legacy-shim: the shim was used in the last 7 days; migrate those projects or pass --force`
    se `usage.log` tem entrada dos últimos 7 dias e não há `--force`.
    Senão remove apenas `<T>/scripts/herdr-agents`,
    `<T>/scripts/herdr-agents.cmd`, `<T>/SHIM`, `<T>/usage.log`, depois
    `rmdir` de `<T>/scripts` e `<T>` (se sobrar arquivo desconhecido, não
    apaga: avisa e sai 5), e remove `L` só se for o symlink
    `../../.agents/skills/herdr-agents`. Nunca `rm -r`.
- Nunca imprima variáveis de ambiente nem linhas de comando de processos.

### Teste do comportamento real dos hooks antigos (obrigatório)

Em `compat/test/legacy-shim.test.mjs` (node:test; todo `spawnSync` com
`timeout`), com `HOME` temporário `H`:
1. Copie `skills/herdr-soho` para `H/.agents/skills/herdr-soho` e rode
   `compat/install-legacy-shim.sh --install --home H`.
2. Projeto temporário `P` com `AGENTS.md` contendo o bloco antigo
   (`LEGACY_SETUP_START`…`LEGACY_SETUP_END`) e `.claude/settings.json` com
   os dois hooks legados (`legacyHookReminder()`, `legacyHookDoctor()`).
3. Execute o comando legado do `SessionStart` como o Claude Code executa:
   `spawnSync('/bin/sh', ['-c', legacyHookDoctor()], { cwd: P, env: { PATH, HOME: H, HERDR_ENV: '1', CLAUDE_PROJECT_DIR: P, TMPDIR } })`.
   Afirme: exit 0; toda linha não vazia do stdout começa com
   `herdr-agents doctor: `; uma delas contém
   `legacy herdr-agents hooks in .claude/settings.json`; `usage.log` ganhou
   uma linha com `P` e `doctor`.
4. Sem o shim (outro `HOME` só com a skill nova): o mesmo comando imprime
   `herdr-agents doctor: skill script not found` e sai 0.
5. `herdr-soho setup` em `P` (pelo lançador novo, `HOME=H`) deixa o
   `AGENTS.md` com o bloco novo no lugar do antigo e o `settings.json` sem
   os hooks legados; um `doctor` em seguida não imprime mais linhas
   `legacy herdr-agents`.
6. `--check` e `--remove` (com e sem uso recente, com `--force`, com
   `SKILL.md` presente → exit 3).

## Expected result

Arquivos e comportamentos acima; testes verdes; nenhum golden alterado.

## Acceptance criteria

1. `node --test compat/test/legacy-shim.test.mjs` e
   `bun test compat/test/legacy-shim.test.mjs` → 0 fail.
2. Testes novos em `skills/herdr-soho/scripts/test/setup-legacy.test.mjs`
   para `setupBlockResult` (bloco antigo trocado na mesma posição; arquivo
   sem marcador antigo idêntico), `settingsHooksResult` (legado exato
   removido; comando do usuário com a palavra `herdr-agents` mantido) e os
   dois hashes → `node --test` e `bun test` do arquivo → 0 fail. Cada teste
   com `// Mutation captured: …` executado (cite comando e saída).
3. Nada regride, sem regravar goldens:
   `node --test skills/herdr-soho/scripts/test/setup.test.mjs skills/herdr-soho/scripts/test/setup-local.test.mjs skills/herdr-soho/scripts/test/parity-setup.test.mjs skills/herdr-soho/scripts/test/parity-setup-plan.test.mjs skills/herdr-soho/scripts/test/parity-doctor.test.mjs skills/herdr-soho/scripts/test/doctor.test.mjs`
   → 0 fail; `skills/herdr-soho/scripts/run-tests.sh --env outside test-setup.sh test-setup-plan.sh` → PASS.

## When the brief does not decide

Não escolha. Marque o item `[partial]`, liste a lacuna e as opções em
"Open questions" e siga com os outros itens. Nunca invente nomes, flags,
caminhos ou requisitos.

## Owned files

- `skills/herdr-soho/scripts/lib/setuptext.mjs`
- `skills/herdr-soho/scripts/lib/commands/setup.mjs`
- `skills/herdr-soho/scripts/lib/setuplocal.mjs` — só testes de presença de bloco
- `skills/herdr-soho/scripts/lib/commands/doctor.mjs` — só linhas ~798–825 e `settingsHasDoctorHook`
- `skills/herdr-soho/scripts/test/setup-legacy.test.mjs` — novo
- `compat/legacy-shim/herdr-agents`, `compat/legacy-shim/herdr-agents.cmd`, `compat/install-legacy-shim.sh`, `compat/test/legacy-shim.test.mjs` — novos

## Forbidden

- `lib/config.mjs`, `lib/platform.mjs`, `lib/lanes.mjs`, `herdr-soho.mjs`,
  `setup-plan.mjs`, `run-tests.sh`, e o resto de `doctor.mjs` — outra fatia.
- Qualquer `SKILL.md` (não crie `SKILL.md` em `compat/`), README, docs —
  o orquestrador escreve.
- Goldens em `scripts/test/golden/` — não regravar.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.
- Não tocar `~/.agents`, `~/.claude`, `~/.config` reais nem projetos fora da
  worktree: tudo em `HOME` temporário.

## Sources (local paths, read before editing)

- `skills/herdr-soho/scripts/lib/setuptext.mjs` (inteiro, ~130 linhas)
- `skills/herdr-soho/scripts/lib/commands/setup.mjs:30-120`, `:280-300`
- `skills/herdr-soho/scripts/lib/commands/doctor.mjs:655-675`, `:795-825`
- `skills/herdr-soho/scripts/test/setup.test.mjs` — padrão de teste
- `skills/herdr-soho/scripts/herdr-soho` — padrão do lançador POSIX

## Project rules that apply

- Código, comentários e mensagens em inglês; densidade de comentários igual
  à dos arquivos vizinhos.
- Teste de comportamento observável; mutação nomeada e executada.
- Escritas atômicas (temporário no mesmo diretório + rename); nada é apagado
  além do que o item 2 lista.
- Portável: `sh` POSIX no shim (sem bashismos), `bash` no instalador; nada de
  `readlink -f`, `sed -i`, `grep -P`.

## Checks you may run

- Os comandos dos critérios; `node --test <um arquivo>` enquanto itera.
- Não rode a suíte completa nem a matriz inteira; não rode formatador.
- Um arquivo por chamada de ferramenta, algumas centenas de linhas por vez.

## Non-goals

- Fallback de ambiente, arquivos de config e diretório de estado legados
  (outra fatia define `HERDR_SOHO_LEGACY_SHIM` no `doctor`).
- Instalar o shim na máquina real, editar projetos reais, documentação.

## Report

Escreva o relatório em Markdown no caminho indicado pelo contrato de
relatório, por item: `[done]` / `[partial]` / `[skipped]` + motivo, com as
saídas coladas dos comandos (nunca redigitadas) e as mutações executadas.
