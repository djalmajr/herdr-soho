# Brief — S2: `herdr-soho find` (achar uma sessão por máquina / workspace / tab / pane / nome)

Role: implementer · Agent: build · Report language: pt-BR

Worktree própria, branch `feat/session-find` (base `feat/session-refs`
`c1596eb`). Caminhos relativos à raiz dela. Plano:
`/work/herdr-soho/.agents/plans/2026-09-28-herdr-soho-refs-e-mensagens.md`.
Pesquisa (fatos do Herdr 0.9.1, leia as seções 1 e 2):
`/work/herdr-soho/.herdr-soho/w14/reports/soho-s1-20260928T102929.md`.

## Goal

`herdr-soho find [<busca>…]` lista os panes do Herdr (local e, quando
pedido, de outras máquinas) com uma referência que dá para colar no chat,
filtrados pela busca.

## Decisions already made

1. **Referência:** use `lib/sessionref.mjs` (já existe na base:
   `parseRef`, `formatRef`, `herdrMachineArgs`, `LOCAL_MACHINE`). Não
   crie outro formato.
2. **Fonte:** uma chamada `herdr [--machine <m>] api snapshot` por
   máquina (via `runCli` de `lib/platform.mjs`, timeout 30 s por
   máquina). Não use `pane list`/`agent list`.
3. **Módulo novo `lib/sessions.mjs`** exporta:
   - `sessionEntries(snapshot, machine)` → um objeto por **pane** do
     snapshot, juntando pane + agente (mesmo `pane_id`) + workspace + tab:
     `{ ref, machine, workspace_id, workspace_label, tab_id, tab_label,
     pane_id, name, kind, status, cwd, title, focused }` — `name`/`kind`
     `null` quando o pane não tem agente; `kind` = `agent`; `status` =
     `agent_status`; `cwd` = `foreground_cwd` se houver, senão `cwd`;
     `title` = `title` se houver, senão `terminal_title_stripped`, senão
     `null`; `ref` = `formatRef({ machine, paneId })`.
   - `matchEntries(entries, words)` → busca: cada palavra (sem diferenciar
     maiúsculas) precisa aparecer em algum destes campos: `ref`,
     `machine`, `workspace_id`, `workspace_label`, `tab_id`, `tab_label`,
     `pane_id`, `name`, `kind`, `status`, `cwd`, `title` (E entre
     palavras). Uma busca de uma palavra só que é referência (`parseRef`
     não nulo) casa **só** o pane dessa máquina e desse id. Sem palavras:
     tudo.
   - `fetchSessions(machines, { env })` → `{ entries, failures }`, com
     `failures` = `[{ machine, cause }]` das máquinas que falharam.
4. **Máquinas:** padrão só `local`. `--machine <label>` (repetível)
   acrescenta máquinas; `--all` = `local` + cada máquina `enabled` de
   `herdr machine list --json` (label). Uma busca que é referência de
   outra máquina (`windows/w3:p1`) consulta essa máquina mesmo sem
   `--machine`.
5. **Saída:**
   - padrão: TSV, uma linha por entrada, colunas
     `ref	name	kind	status	workspace	tab	cwd` (`workspace` =
     `workspace_label`, `tab` = `tab_label`; vazio vira `-`), ordenado por
     máquina (`local` primeiro, depois as outras na ordem pedida) e pela
     ordem do snapshot;
   - `--json`: uma linha JSON por entrada, com os campos do item 3.
6. **Falhas:** máquina remota que falha → linha em stderr
   `herdr-soho: find: machine '<m>' unavailable: <causa curta>` e segue;
   `local` que falha (Herdr fora do ar) → exit 4. **Exit:** 0 com ao
   menos uma entrada; 1 sem nenhuma (como `grep`); 2 uso errado (flag
   desconhecida, `--machine` sem valor).
7. **Registro do comando:** `scripts/herdr-soho.mjs` (tabela de
   comandos e texto de ajuda), no mesmo padrão de `lint`/`mutation-guard`.
   Documente o comando em `skills/herdr-soho/SKILL.md` na lista de
   comandos (uma linha + um parágrafo curto de uso: colar a referência no
   chat) e em `docs/guide.md` (seção curta).

## Expected result

`find` funcionando com fake `herdr` nos testes; tabela no relatório com os
casos de busca e o que cada um devolve.

## Acceptance criteria

1. Testes em `skills/herdr-soho/scripts/test/find.test.mjs` com um `herdr`
   falso (`writeFakeCli` de `test/fakes.mjs`) que devolve snapshots fixos
   por máquina (`--machine` na linha de argumentos): busca por nome
   parcial, por kind+status (duas palavras), por rótulo de workspace, por
   trecho de cwd, por referência local e remota, sem busca, `--json`,
   `--all` com uma máquina que falha (stderr + exit 0), sem resultado
   (exit 1), local fora do ar (exit 4), uso errado (exit 2). Cada teste
   com `// Mutation captured: …` executado.
2. `node --test` e `bun test --timeout 60000` do teste novo e de
   `sessionref.test.mjs` → 0 fail (cole).
3. Uma execução real no macOS, só leitura: `node
   skills/herdr-soho/scripts/herdr-soho.mjs find soho` e `… find --json
   w14` (cole as primeiras linhas). Não use `--all` nem `--machine` na
   execução real (as máquinas remotas levam 5–20 s e são validadas pelo
   orquestrador).

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `skills/herdr-soho/scripts/lib/sessions.mjs` (novo)
- `skills/herdr-soho/scripts/lib/commands/find.mjs` (novo)
- `skills/herdr-soho/scripts/test/find.test.mjs` (novo)
- `skills/herdr-soho/scripts/herdr-soho.mjs` (só o registro do comando e a
  linha de ajuda)
- `skills/herdr-soho/SKILL.md`, `docs/guide.md` (só o trecho do `find`)

## Forbidden

- `lib/sessionref.mjs` (use como está; lacuna vira pergunta aberta),
  `plugin/*`, `lib/peer.mjs`, `commands/send.mjs` (outras fatias em
  paralelo), goldens, launchers.
- Mandar prompt, teclas ou mensagem a qualquer pane; criar/fechar panes.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera. Não rode a
suíte completa.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e as mutações
executadas.
