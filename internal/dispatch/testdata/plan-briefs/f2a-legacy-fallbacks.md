# Brief — F2a: leitura legada de ambiente, config e estado

Role: implementer · Agent: build · Report language: pt-BR

Você trabalha na worktree `.worktrees/f2a` (branch `wip/f2a`) do repositório
herdr-soho. A skill `herdr-agents` acabou de ser renomeada para `herdr-soho`
(commit HEAD desta worktree): diretório `skills/herdr-soho/`, CLI
`scripts/herdr-soho`, ambiente `HERDR_SOHO_*`, config de usuário
`~/.config/herdr-soho/config`, config de projeto `.agents/herdr-soho.conf`,
estado `.herdr-soho/`. Projetos e sessões existentes ainda usam os nomes
antigos. Todos os caminhos abaixo são relativos à raiz da worktree.

## Goal

O CLI `herdr-soho` continua funcionando, sem perda de configuração nem de
estado, em máquinas e projetos que só têm os nomes antigos
(`HERDR_AGENTS_*`, `~/.config/herdr-agents/config`,
`.agents/herdr-agents.conf`, `.herdr-agents/`), e o `doctor` diz exatamente o
que ainda é legado.

## Decisions already made

1. **Novo módulo** `skills/herdr-soho/scripts/lib/legacy.mjs`, com exports
   exatamente com estes nomes:
   - `applyLegacyEnv(env)`: para cada variável `HERDR_AGENTS_<RESTO>` com
     valor não vazio, se `env['HERDR_SOHO_<RESTO>']` é `undefined` ou `''`,
     define `env['HERDR_SOHO_<RESTO>'] = valor`. Não apaga a variável antiga.
     Guarda e retorna a lista (ordenada) dos nomes antigos copiados; o getter
     `legacyEnvCopied()` devolve a última lista.
     Exemplos: `HERDR_AGENTS_DIR=x` sem `HERDR_SOHO_DIR` → `HERDR_SOHO_DIR=x`,
     lista `['HERDR_AGENTS_DIR']`. `HERDR_AGENTS_DIR=x` com `HERDR_SOHO_DIR=y`
     → fica `y`, lista vazia. `HERDR_AGENTS_DIR=` (vazio) → nada.
     `HERDR_AGENTSX=1` (sem `_` depois de `AGENTS`) → ignorado.
   - `legacyUserConfigPath(platform, env)`: mesma regra de
     `userConfigPath` (`lib/platform.mjs:30`), trocando o diretório
     `herdr-soho` por `herdr-agents` (XDG, `%APPDATA%`, `AppData\Roaming`,
     `~/.config`).
   - `legacyProjectConfigPath(root)`: `<root>/.agents/herdr-agents.conf`.
   - `effectiveConfigFile(newPath, legacyPath)`: `newPath` se for arquivo
     regular; senão `legacyPath` se for arquivo regular; senão `newPath`.
   - `migrateLegacyConfigFile(dest, env, cwd)`: se `dest` é o caminho novo do
     usuário ou do projeto (compare com `userConfigPath(process.platform, env)`
     e `<projectRoot>/.agents/herdr-soho.conf`), `dest` não existe e o
     legado correspondente é arquivo regular: cria o diretório de `dest`,
     copia o conteúdo byte a byte para um arquivo temporário **no mesmo
     diretório** e renomeia para `dest`, com o mesmo modo do arquivo legado;
     escreve em stderr `herdr-soho: warning: copied legacy config <legado> to <dest>; the legacy file is no longer read`
     e retorna `true`. Em qualquer outro caso não faz nada e retorna `false`.
     Nunca apaga nem altera o arquivo legado.
   - `defaultStateDirName(root)`: `'.herdr-agents'` quando
     `<root>/.herdr-soho` não existe e `<root>/.herdr-agents` é diretório;
     senão `'.herdr-soho'`.
   - `legacyDoctorWarnings({ env, cwd, platform })`: lista de strings (sem o
     prefixo `warn`), na ordem abaixo, só as que se aplicam:
     - por variável copiada: `legacy environment variable HERDR_AGENTS_<R> is read as HERDR_SOHO_<R>; rename it`
     - arquivo de usuário legado é o efetivo: `legacy user config in use: <legado> (the next 'config set --user' copies it to <novo>)`
     - os dois existem: `legacy user config <legado> is no longer read (<novo> exists); remove it once you no longer need it`
     - projeto, mesmas duas frases com `project config`, `.agents/…` absolutos e `'config set'`
     - estado legado em uso (regra de `defaultStateDirName` e nenhum
       `state_dir`/`HERDR_SOHO_DIR` definido): `legacy state dir in use: <root>/.herdr-agents (once no worker is live, rename it to .herdr-soho and ignore .herdr-soho/ in git)`
     - os dois diretórios existem: `legacy state dir <root>/.herdr-agents is no longer used (<root>/.herdr-soho exists); clean it once its reports are no longer needed`
     - `env.HERDR_SOHO_LEGACY_SHIM === '1'`: `called through the legacy herdr-agents script path; use <ENTRY_SCRIPT>` (o `ENTRY_SCRIPT` de `lib/commands/doctor.mjs`; passe-o como argumento se precisar evitar import circular).
2. **Ligações** (nada mais muda de comportamento):
   - `skills/herdr-soho/scripts/herdr-soho.mjs`: chamar
     `applyLegacyEnv(process.env)` antes de `loadConfig()` (hoje linha 66).
   - `lib/config.mjs` `loadConfig`: as camadas `user` e `project` leem
     `effectiveConfigFile(novo, legado)`; os rótulos continuam `'user'` e
     `'project'`.
   - `lib/config.mjs` `stateRootPath`: o padrão de `state_dir` passa a ser
     `defaultStateDirName(projectRoot(env, cwd))`.
   - `lib/config.mjs` `cmdConfig` (linhas `user file:` / `project file:`):
     mostram o arquivo efetivo; quando for o legado, acrescentam
     ` (legacy)` ao fim da linha.
   - Escritas: chamar `migrateLegacyConfigFile(dest, env, cwd)` no início de
     `configWritePair` (`lib/config.mjs:222`) e de `applyLaneFile`
     (`lib/lanes.mjs:833`). Assim `config set`, `setup --panes/--lane` e
     `doctor --fix` migram na primeira escrita, sem mudar os chamadores.
   - `lib/commands/setup-plan.mjs:382-390`: o "antes" do plano copia o
     arquivo efetivo (legado quando for o caso) para o temporário; o
     caminho mostrado continua o novo.
   - `lib/commands/doctor.mjs`: `projectIsFirstRun` lê o arquivo de projeto
     efetivo; imprimir `legacyDoctorWarnings(...)` com `s.warn(...)`
     **logo depois** do bloco que imprime `state dir writable` (linhas
     ~730–765). Não edite as linhas 795–830 nem `settingsHasDoctorHook`
     (são de outra fatia).
   - `skills/herdr-soho/scripts/run-tests.sh` (linhas ~164–170): além de
     `HERDR_SOHO_*`, também remover do ambiente dos testes toda variável
     `HERDR_AGENTS_*` do ambiente pai.
3. Sem fixtures nem máquinas reais: os testes usam `HOME`,
   `XDG_CONFIG_HOME` e raiz de projeto temporários.

## Expected result

- Um projeto que só tem `.agents/herdr-agents.conf` e `.herdr-agents/`
  mostra as chaves do arquivo legado em `herdr-soho config` e usa
  `.herdr-agents/` como estado, sem criar `.herdr-soho/` nem mexer no
  `.gitignore`.
- `HERDR_AGENTS_LAYOUT=tab` sem `HERDR_SOHO_LAYOUT` faz `config` mostrar
  `layout tab env`.
- `config set --user k v` com só o arquivo legado cria o novo com todo o
  conteúdo legado + a chave, e o legado fica intacto.
- `doctor` imprime as linhas `warn` exatas acima quando se aplicam e
  nenhuma linha nova num ambiente sem nada legado.

## Acceptance criteria

1. Testes novos em `skills/herdr-soho/scripts/test/legacy.test.mjs`
   (node:test, `spawnSync` sempre com `timeout`) cobrindo: cada exemplo de
   `applyLegacyEnv`; `effectiveConfigFile` nos 3 casos; migração na primeira
   escrita (conteúdo byte a byte, modo preservado, legado intacto, segunda
   escrita não copia de novo); `defaultStateDirName` nos 3 casos (só novo,
   só legado, ambos); CLI de ponta a ponta pelo lançador
   `skills/herdr-soho/scripts/herdr-soho` com `HOME`/`XDG_CONFIG_HOME`
   temporários: `config` com arquivo de usuário legado, `config` com
   `HERDR_AGENTS_LAYOUT=tab`, `doctor` com cada frase de aviso. Cada teste
   com um comentário `// Mutation captured: …` que você **executou** (edite o
   código, veja o teste falhar, desfaça) — cite no relatório os comandos e
   as saídas.
   — provado por `node --test skills/herdr-soho/scripts/test/legacy.test.mjs`
   e `bun test skills/herdr-soho/scripts/test/legacy.test.mjs` → 0 fail.
2. Nada existente regride: `node --test skills/herdr-soho/scripts/test/parity-config.test.mjs skills/herdr-soho/scripts/test/parity-doctor.test.mjs skills/herdr-soho/scripts/test/config.test.mjs skills/herdr-soho/scripts/test/doctor.test.mjs skills/herdr-soho/scripts/test/state.test.mjs skills/herdr-soho/scripts/test/setup-plan.test.mjs`
   → 0 fail, **sem** regravar goldens. Se um golden mudar, pare e marque
   `[partial]` com o diff.
3. `skills/herdr-soho/scripts/run-tests.sh --env outside test-config-set.sh test-doctor-fix.sh`
   → PASS.

## When the brief does not decide

Não escolha. Marque o item `[partial]`, liste a lacuna e as opções em
"Open questions" e siga com os outros itens. Nunca invente nomes, flags,
caminhos ou requisitos.

## Owned files

- `skills/herdr-soho/scripts/lib/legacy.mjs` — novo
- `skills/herdr-soho/scripts/lib/config.mjs` — ligações acima
- `skills/herdr-soho/scripts/lib/lanes.mjs` — só a chamada em `applyLaneFile`
- `skills/herdr-soho/scripts/lib/commands/setup-plan.mjs` — só o "antes" do plano
- `skills/herdr-soho/scripts/lib/commands/doctor.mjs` — só `projectIsFirstRun` e a inserção após `state dir writable`
- `skills/herdr-soho/scripts/herdr-soho.mjs` — só a chamada de `applyLegacyEnv`
- `skills/herdr-soho/scripts/run-tests.sh` — só a remoção de `HERDR_AGENTS_*`
- `skills/herdr-soho/scripts/test/legacy.test.mjs` — novo

## Forbidden

- `skills/herdr-soho/scripts/lib/setuptext.mjs`, `lib/commands/setup.mjs`,
  `lib/setuplocal.mjs`, `doctor.mjs` linhas 795–830 e
  `settingsHasDoctorHook`, `compat/` — outra fatia.
- Goldens em `scripts/test/golden/` — não regravar.
- SKILL.md, README, docs — o orquestrador escreve a documentação.
- Nenhum comando git que escreva (add, commit, checkout, stash, reset,
  mv). No commit, push, tag, or PR. The orchestrator owns git.
- Não ler nem alterar `~/.config`, `~/.agents` ou qualquer projeto fora da
  worktree; testes só em diretórios temporários.

## Sources (local paths, read before editing)

- `skills/herdr-soho/scripts/lib/config.mjs:60-180` e `:218-260` — camadas, `cfg`, `stateRootPath`, `configWritePair`
- `skills/herdr-soho/scripts/lib/platform.mjs:20-40` — `homeDir`, `userConfigPath`
- `skills/herdr-soho/scripts/lib/lanes.mjs:833` — `applyLaneFile`
- `skills/herdr-soho/scripts/lib/commands/doctor.mjs:600-640`, `:720-770`
- `skills/herdr-soho/scripts/test/config.test.mjs` — padrão de teste com diretórios temporários

## Project rules that apply

- Código, comentários e mensagens em inglês; densidade de comentários igual
  à dos arquivos vizinhos.
- Teste só de comportamento observável; cada teste nomeia a mutação que
  mata, e a mutação foi executada.
- Escritas atômicas: temporário no mesmo diretório + rename; nada é apagado.
- Portável (macOS, Linux, Windows `path.join`); sem dependências novas.

## Checks you may run

- Os comandos dos critérios 1–3, e `node --test <um arquivo>` enquanto itera.
- Não rode a suíte completa nem a matriz inteira (o orquestrador roda na
  integração). Não rode formatador.
- Um arquivo por chamada de ferramenta, algumas centenas de linhas por vez.

## Non-goals

- Migrar blocos de instrução ou hooks antigos, e o shim do caminho antigo
  (outra fatia).
- Mover ou apagar `.herdr-agents/`, arquivos legados ou qualquer estado.

## Report

Escreva o relatório em Markdown no caminho indicado pelo contrato de
relatório, por item: `[done]` / `[partial]` / `[skipped]` + motivo, com as
saídas coladas dos comandos (nunca redigitadas) e as mutações executadas.
