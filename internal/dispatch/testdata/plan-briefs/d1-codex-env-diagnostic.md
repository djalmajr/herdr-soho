# Brief — D1: diagnóstico de Codex no Herdr sem `HERDR_*`

Role: implementer · Agent: build · Report language: pt-BR

Worktree `.worktrees/d1`, branch `fix/codex-env-diagnostic`, base `main`
`0573d1e`. Caminhos relativos à raiz dela.

## Goal

Quando um comando do `herdr-soho` roda sem `HERDR_ENV=1`, ele distingue
"comando de um Codex que está num painel Herdr, mas cujo executor removeu
as variáveis `HERDR_*`" de "fora do Herdr de verdade", dá a causa e a
correção, e continua recusando controlar a sessão. Nada disso usa o foco.

## Causa já provada (não reinvestigue)

- `~/.codex/config.toml` tem `[shell_environment_policy] inherit = "core"`.
  Com isso o Codex passa aos comandos só `HOME LANG LOGNAME PATH SHELL
  USER TMPDIR` (mais `CODEX_SANDBOX*` e as chaves de `set`): todas as
  `HERDR_*` somem. Prova: `env HERDR_ENV=1 HERDR_PANE_ID=wZtest:p0 codex
  sandbox -- /usr/bin/printenv` não mostra nenhuma `HERDR_*`; com
  `-c shell_environment_policy.inherit=all` elas aparecem.
- `include_only` é aplicado **depois** de `set`: chave de `set` fora do
  `include_only` também some (provado).
- Sem `HERDR_PANE_ID`, `herdr pane current` (com ou sem `--current`)
  devolve o painel **focado**, não o chamador (provado: devolveu o painel
  de outro workspace). Isso é proibido como fonte de identidade.
- `herdr pane process-info --pane <id>` devolve
  `result.process_info.foreground_process_group_id` e
  `result.process_info.foreground_processes[]` com `pid`, `name`, `cwd`
  (e campos `argv`/`cmdline` que nunca podem ser impressos nem gravados).

## Decisions already made

1. **Checagem estática (doctor).** O `doctor` lê
   `${CODEX_HOME:-~/.codex}/config.toml`, só a seção
   `[shell_environment_policy]` (chaves `inherit`, `include_only`,
   `exclude`; string, ou array de strings que pode ocupar várias linhas).
   Avisa quando `HERDR_ENV` não passaria: `inherit` é `"core"` ou `"none"`
   e `include_only` não tem padrão que case `HERDR_ENV`; ou
   `include_only` não vazio sem padrão que case `HERDR_ENV`; ou `exclude`
   casa `HERDR_ENV`. Padrões são globs `*` sem diferenciar maiúsculas (como
   o Codex). Texto do aviso:
   `codex: shell_environment_policy drops HERDR_* (<motivo curto>): commands Codex runs cannot see Herdr; see the Codex section of docs/guide.md`.
   Arquivo ausente ou sem a seção: nada. Nunca imprime outro conteúdo do
   arquivo (ele pode ter tokens). Se já existir leitor de TOML no código,
   reuse; senão um leitor mínimo só dessa seção.
2. **Checagem em tempo de execução.** Onde hoje o código conclui "fora do
   Herdr" (`requireEnv` em `lib/herdr.mjs` e a linha `HERDR_ENV` do
   `doctor`), quando `HERDR_ENV != 1`, fora do Windows e com `herdr` no
   `PATH`:
   - suba os ancestrais do processo atual (até 64 níveis) por pid/ppid e
     nome do executável (`ps -o ppid=,comm= -p <pid>`, ou `/proc` no
     Linux); se nenhum se chama `codex`, mensagem de hoje, sem chamar o
     Herdr;
   - com ancestral `codex`: **um** `herdr api snapshot`; para cada painel
     local cujo agente é `codex`, `herdr pane process-info --pane <id>`;
     o painel casa se algum pid ancestral está em
     `foreground_processes[].pid`;
   - exatamente um painel casa → a recusa (exit 2 em `requireEnv`; aviso
     no `doctor`) passa a dizer:
     `this command runs under Codex in Herdr pane local/<pane> (matched by process ancestry), but Codex's shell_environment_policy does not pass HERDR_*; allow them (see herdr-soho doctor) and restart Codex`;
   - zero ou vários → mensagem de hoje mais
     `(a Codex ancestor was found, but no single Herdr pane matched it)`.
   Continua recusando: nunca injeta `HERDR_*`, nunca adota o painel
   encontrado para controlar nada. Falha de `ps`/Herdr → mensagem de hoje.
3. **Proibido como identidade:** `pane current` sem `HERDR_PANE_ID`, foco,
   cwd, nome do workspace. Nunca imprimir/gravar `argv`, `cmdline`,
   `argv0` nem linhas de comando de processos (só pid e nome).
4. **Docs.** `docs/guide.md` ganha a seção Codex com a correção
   recomendada, que só acrescenta as `HERDR_*` ao que `core` já passa:
   ```toml
   [shell_environment_policy]
   inherit = "all"
   include_only = ["HOME", "LANG", "LOGNAME", "PATH", "SHELL", "USER", "USERNAME", "TMPDIR", "TEMP", "TMP", "HERDR_*"]
   ```
   com a nota de que toda chave de `set` precisa entrar também em
   `include_only`, e de que o Codex precisa ser reiniciado. No
   `SKILL.md`, junto ao preflight (`test "${HERDR_ENV:-}" = 1 …`): se ele
   falhar e você for o Codex, rode `herdr-soho doctor` antes de concluir
   que está fora do Herdr.

## Acceptance criteria

1. Testes com `herdr` falso no `PATH`, `HERDR_SOCKET_PATH` para caminho
   inexistente, `CODEX_HOME` temporário e ancestrais falsos (injete o
   leitor de ancestrais ou use um `ps` falso no `PATH`): cada caso da
   decisão 1 (core, none, include_only sem/com `HERDR_*`, exclude,
   arquivo ausente, array em várias linhas, outro conteúdo não
   impresso); decisão 2 (sem ancestral codex → nenhuma chamada ao Herdr;
   um painel casa → mensagem com `local/<pane>` e exit 2; dois casam;
   nenhum casa; `argv`/`cmdline` do falso nunca aparecem na saída);
   `pane current` nunca é chamado. Cada um com `// Mutation captured: …`
   executado.
2. `node --test` e `bun test --timeout 60000` dos arquivos de teste novos
   e de `doctor*.test.mjs`, `parity-doctor*.test.mjs` (os que existirem)
   e dos que cobrem `requireEnv` → 0 fail (cole). Goldens que mudarem:
   diff decodificado e o motivo.
3. Uma execução real **só de leitura** do `doctor` nesta máquina (Claude,
   `HERDR_ENV=1`) mostra o aviso da decisão 1 (o `config.toml` real tem
   `inherit = "core"`). Cole só a linha do aviso.

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `skills/herdr-soho/scripts/lib/herdr.mjs` (só `requireEnv` e o que ele
  chamar), `skills/herdr-soho/scripts/lib/commands/doctor.mjs` (só as
  linhas das decisões 1 e 2), um módulo novo em `skills/herdr-soho/scripts/lib/`
  para as duas checagens, testes novos em `skills/herdr-soho/scripts/test/`,
  os goldens que mudarem por isso
- `docs/guide.md` (seção Codex), `skills/herdr-soho/SKILL.md` (nota do
  preflight)

## Forbidden

- Todo o resto. Editar `~/.codex/config.toml` ou qualquer configuração do
  usuário. Mandar prompt, teclas ou texto a painéis reais; chamar
  `pane current`.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera. Não rode a
suíte completa.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e as mutações
executadas.
