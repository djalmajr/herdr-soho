# Brief — S3: seletor de sessão no plugin do Herdr (busca + copiar referência)

Role: implementer · Agent: build · Report language: pt-BR

Worktree própria, branch `feat/session-picker` (base `feat/session-refs`
`c1596eb`). Caminhos relativos à raiz dela. Plano:
`/work/herdr-soho/.agents/plans/2026-09-28-herdr-soho-refs-e-mensagens.md`.
Pesquisa (leia as seções 3 e 4 e as opções O2/O4):
`/work/herdr-soho/.herdr-soho/w14/reports/soho-s1-20260928T102929.md`.

## Goal

Uma ação do plugin abre, sobre o pane atual, uma lista com busca de todos
os panes (local e máquinas salvas); Enter copia a referência do escolhido
para a área de transferência, para o usuário colar no chat.

## Decisions already made

1. **Fonte dos dados:** o seletor roda o CLI da skill
   (`node <defaultCliScript()> find --json` — reuse `defaultCliScript` de
   `plugin/bridge.mjs`), que outra fatia implementa em paralelo com este
   contrato (uma linha JSON por pane):
   `{"ref":"local/w12:p1","machine":"local","workspace_id":"w12","workspace_label":"appliance","tab_id":"w12:t1","tab_label":"1","pane_id":"w12:p1","name":"orchestrator-10","kind":"claude","status":"working","cwd":"/Users/…","title":"…","focused":false}`
   (`name`/`kind` podem ser `null`). Carregue primeiro `find --json`
   (local) e mostre; em seguida, em paralelo, `find --json --machine <m>`
   para cada máquina `enabled` de `herdr machine list --json`, anexando
   ao chegar (linha de status "carregando windows…"). Nos testes, o CLI é
   um falso (injetável).
2. **Manifesto (`plugin/herdr-plugin.toml`):**
   - `[[panes]]` `id = "picker"`, `title = "herdr-soho: find a session"`,
     `placement = "overlay"`, `platforms = ["linux", "macos", "windows"]`,
     `command = ["node", "picker.mjs"]`;
   - `[[actions]]` `id = "pick"`, título "Find a session and copy its
     reference", `contexts = ["workspace"]`, `command = ["node",
     "bridge.mjs", "pick"]`. A ação só abre o painel: `herdr plugin pane
     open --plugin djalmajr.herdr-soho --entrypoint picker --placement
     overlay --focus` (via `HERDR_BIN_PATH`). Ajuste `bridge.mjs` para
     aceitar `pick` sem mudar `doctor`/`roster`.
3. **Programa `plugin/picker.mjs`** (Node, sem dependências, stdin em
   modo raw):
   - teclas: letras/dígitos/espaço/pontuação filtram (palavras em E, sem
     diferenciar maiúsculas, sobre `ref`, `name`, `kind`, `status`,
     `workspace_label`, `tab_label`, `cwd`, `machine`); Backspace; ↑/↓
     movem a seleção; Enter copia e sai; Esc ou Ctrl-C saem sem copiar;
   - cada linha mostra `ref  name  kind  status  workspace/tab  cwd`
     (cortada à largura do terminal);
   - texto copiado: `<ref> (<name>, <kind>, <status>) <cwd>` — com `-` no
     lugar de nulos. O primeiro token é sempre a referência.
   - depois de copiar: `herdr notification show "herdr-soho" --body
     "copied <ref>" --sound none` (falha da notificação não é erro).
4. **Área de transferência (`plugin/clipboard.mjs`):** ferramenta nativa
   primeiro — macOS `pbcopy`; Windows `powershell -NoProfile -Command
   Set-Clipboard` com o texto no stdin (não use `clip.exe`: estraga
   UTF-8); Linux `wl-copy`, senão `xclip -selection clipboard`, senão
   `xsel --clipboard --input`. Se nenhuma existir ou falhar, escreva OSC
   52 (`ESC ] 52 ; c ; <base64> BEL`) no terminal do seletor (o Herdr
   repassa ao terminal do usuário). Devolve qual caminho usou.
5. **Portabilidade:** Linux, macOS e Windows (como o resto do plugin);
   testes com os fakes por plataforma que `plugin/test/bridge.test.mjs`
   já usa.
6. **Docs:** `README.md` (seção do plugin: a ação `pick`, a tecla para
   ligá-la via `[[keys.command]] … type = "plugin_action"`, o formato
   copiado) e `docs/guide.md` (parágrafo curto).

## Expected result

Seletor e ação funcionando com fakes; tabela no relatório: tecla →
efeito, e caminho de cópia por plataforma.

## Acceptance criteria

1. `plugin/test/picker.test.mjs`: o filtro (palavras em E, maiúsculas),
   a navegação, o texto copiado (com nulos), a carga incremental (local
   primeiro, remota anexada; máquina remota que falha vira linha de
   status, não erro), Esc sem copiar, e `clipboard.mjs` escolhendo a
   ferramenta por plataforma com um `PATH` controlado e caindo no OSC 52
   (bytes exatos). Separe a lógica (estado, filtro, render) da E/S do
   terminal para testar sem TTY. Cada teste com `// Mutation captured: …`
   executado.
2. `bridge.test.mjs` continua passando e ganha o caso `pick` (abre o
   painel com os argumentos exatos, num `herdr` falso).
3. `node --test plugin/test/` e `bun test --timeout 60000 plugin/test/` →
   0 fail (cole).
4. Não abra painéis de verdade nem ligue o plugin: o orquestrador valida
   no Herdr.

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `plugin/picker.mjs`, `plugin/clipboard.mjs` (novos)
- `plugin/bridge.mjs` (só o `pick`), `plugin/herdr-plugin.toml`
- `plugin/test/picker.test.mjs` (novo), `plugin/test/bridge.test.mjs`
- `README.md` (seção do plugin), `docs/guide.md` (parágrafo do seletor)

## Forbidden

- `skills/herdr-soho/**` (outras fatias em paralelo), goldens.
- Abrir/fechar panes de verdade, ligar/desligar o plugin, mudar a config
  do Herdr, mandar prompt ou teclas a panes.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e as mutações
executadas.
