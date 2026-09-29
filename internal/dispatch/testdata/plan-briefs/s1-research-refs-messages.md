# Brief — S1: pesquisa para referências de sessão e mensagens entre agentes

Role: researcher · Agent: build · Report language: pt-BR

Seu cwd é o checkout do herdr-soho (`main`). Plano:
`.agents/plans/2026-09-28-herdr-soho-refs-e-mensagens.md` (leia).

## Goal

Fatos verificados, com o comando que prova cada um, para desenhar três
coisas: `herdr-soho find` (achar uma sessão por máquina / workspace / tab
/ pane / nome), um seletor com busca no plugin do Herdr que copia a
referência, e `herdr-soho send` (mensagem automática para um agente de
qualquer CLI).

## Questions

1. **Estado ao vivo.** `herdr api snapshot` (e `herdr api schema`): que
   campos há para workspaces, tabs, panes e agentes (id, rótulo, cwd,
   kind/agente detectado, nome, status, foco)? Cole um trecho real
   (encurte), e diga se ele dispensa `workspace/tab/pane/agent list`.
2. **Outras máquinas.** `herdr machine list` e `herdr --machine <label>
   api snapshot` (ou `agent list`): funciona para as máquinas salvas
   (`windows`, `linux`)? Tempo de resposta de cada uma. Não crie,
   feche nem mude nada nelas.
3. **Plugin.** Pelo manifesto `plugin/herdr-plugin.toml`, pelo código do
   plugin e por `herdr plugin …--help`: uma ação pode abrir um painel de
   plugin (`herdr plugin pane open`) que roda um programa interativo
   (lista com busca) e depois fecha? Como a ação recebe o contexto
   (workspace/pane focado)? Há notificação (`herdr notification show`)?
4. **Área de transferência.** Como copiar texto para a área de
   transferência a partir de um painel do Herdr, local e via ssh: OSC 52
   passa pelo Herdr? `pbcopy` / `wl-copy` / `xclip` / `clip.exe`? Prove
   o que der para provar localmente (sem instalar nada).
5. **Entrega a um agente ocupado.** `herdr agent prompt` (`--wait`,
   `--until`, `agent_blocked`, `agent_prompt_stalled`) e `herdr agent
   wait`: o que acontece se o alvo está `working`? O texto é enfileirado
   pela CLI do agente ou se mistura à entrada? Responda por kind
   (claude, codex, cursor-agent, grok, agy, pi, opencode) com o que a
   documentação/ajuda de cada CLI diz e o que o `herdr-soho` já faz hoje
   em `dispatch` (leia `skills/herdr-soho/scripts/lib/dispatch.mjs`,
   `herdr.mjs`). Não mande prompt a nenhum agente que não seja seu.
6. **Entrada nativa de mensagens.** Além do Claude Code (`SendMessage`,
   retenção por classe de modo, `crossSessionInbound`), alguma dessas
   CLIs tem mecanismo próprio de mensagem entre sessões (socket, inbox,
   hook)? Cite a fonte (`--help`, docs locais, skill do Herdr em
   `~/.agents/skills/herdr`).
7. **Riscos.** Texto injetado num painel chega ao agente como se fosse do
   usuário: o que cada CLI oferece para marcar a origem (nada, hook,
   prefixo)? Liste o que o cabeçalho da mensagem precisa dizer.

## Expected result

Relatório com uma seção por pergunta: fato, comando executado, saída
colada (encurtada), e o que isso implica para `find`, o seletor e `send`.
Termine com "Opções de desenho" para os pontos em aberto (com prós e
contras), sem escolher.

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções e siga.
Nunca invente comandos, flags ou campos: só o que você executou ou leu.

## Forbidden

- Editar qualquer arquivo; mandar prompt, teclas ou mensagem a painéis
  que não são seus; criar/fechar workspaces, tabs ou panes; mudar config
  do Herdr, de máquinas ou das CLIs; instalar pacotes.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas.
