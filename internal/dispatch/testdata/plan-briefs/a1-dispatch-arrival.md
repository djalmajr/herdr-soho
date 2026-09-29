# Brief — A1: chegada confiável do prompt a um agente recém-aberto

Role: implementer · Agent: build · Report language: pt-BR

Worktree própria, branch `fix/dispatch-arrival` (base `main` `a138983`).
Caminhos relativos à raiz dela.

## Problema observado (hoje, 2026-09-28 ~12:20 local)

Dois revisores agy foram abertos com `spawn` e, em seguida, recebidos com
`dispatch … --no-wait`. O `dispatch` devolveu `wait_status: submitted` e
o sidecar da tentativa ficou `{"submission":"accepted"}` sem marca de
`not-received`: a conferência de chegada (`prompt_check_seconds=15`,
`lib/dispatch.mjs` ~1038-1180) concluiu que o prompt tinha chegado. Mas os
dois agy ficaram `idle`, com a caixa de entrada vazia e sem relatório
(o operador viu as telas). Um `dispatch` repetido minutos depois chegou.
Leitura do código: a regra 1 aceita `working`/`blocked` sem exigir que o
estado tenha mudado **depois** do envio, e a regra 4 aceita qualquer
mudança da tela visível. Um agente que ainda está desenhando a própria
tela de abertura (e pode piscar `working`) satisfaz as duas sem ter
recebido nada.

## Goal

O `dispatch` (e `--amend`) só dá o prompt como recebido com prova ligada
a este envio; um prompt perdido termina em reenvio único e, se ainda
perdido, `not-received` (exit 15), inclusive logo depois de `spawn`.

## Decisions already made

1. **Espera de assentamento antes de enviar.** Antes do `agent prompt`,
   espere o alvo estar pronto: `interactive_ready` verdadeiro no
   `herdr agent get` (quando o campo existir) **e** duas leituras
   consecutivas da tela visível idênticas com 500 ms entre elas. Limite
   `prompt_settle_seconds` (chave nova, padrão `20`, `0` desliga; valor
   inválido = padrão), validada como as outras em `lib/config.mjs` e
   documentada em `config.defaults`. Passou do limite → segue com o envio
   e um `warn` (`… did not settle in <n>s; sending anyway`).
2. **Regra 1 mais estrita.** `working`/`blocked` só conta como chegada
   quando o `state_change_seq` do `agent get` é diferente do lido antes
   do envio (`preSeq`), ou quando o relatório não vazio existe (como
   hoje). Um `working` que já estava lá antes do envio não prova nada.
3. **Regra 4 mais estrita.** Uma tela que mudou só conta como chegada se
   o caminho do prompt composto (`composed`, único por envio) aparece na
   tela recente (`agent read --source recent-unwrapped --lines 40`) **fora**
   das últimas 3 linhas não vazias (as da caixa de entrada). Sem isso, a
   tela mudada é tratada como a tela idêntica de hoje (regra 3): um
   reenvio único do mesmo texto e uma janela nova só com as regras 1 e 4
   estritas; ainda nada → `not-received` (exit 15), como hoje.
4. As regras 2 (texto parado na caixa → um Enter) e o resto do fluxo
   (sidecar, `not-received`, códigos, mensagens) não mudam.
5. Documente a espera e as regras novas no comentário do bloco em
   `lib/dispatch.mjs` e em `skills/herdr-soho/SKILL.md` (trecho do
   `dispatch`/arrival check).

## Expected result

Um agente que ainda redesenha a tela de abertura, ou que já estava
`working`, não é mais dado como tendo recebido o prompt sem prova.

## Acceptance criteria

1. Testes com `herdr` falso (padrão de `dispatch.test.mjs`):
   (a) alvo que pisca `working` com o mesmo `state_change_seq` e muda a
   tela sozinho, e **perde** o primeiro prompt → um reenvio; perde de novo
   → `not-received` exit 15; (b) o mesmo alvo que recebe o reenvio (seq
   muda) → recebido, um reenvio; (c) alvo normal → recebido sem reenvio;
   (d) a espera de assentamento espera `interactive_ready` e duas telas
   iguais antes do primeiro `agent prompt`, e respeita
   `prompt_settle_seconds=0`; (e) tela mudada com o caminho do prompt
   visível fora das últimas 3 linhas → recebido sem reenvio. Cada teste
   com `// Mutation captured: …` executado.
2. `node --test` e `bun test --timeout 60000` de `dispatch.test.mjs`,
   `parity-dispatch.test.mjs`, `task-report.test.mjs`,
   `for-released.test.mjs` e do teste novo → 0 fail (cole). Goldens de
   paridade que mudarem: regrave com `HERDR_SOHO_GOLDEN=update` e mostre o
   diff decodificado (só o que a mudança explica).

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `skills/herdr-soho/scripts/lib/dispatch.mjs` (só a espera e as regras
  de chegada)
- `skills/herdr-soho/scripts/lib/config.mjs` (só a chave nova),
  `skills/herdr-soho/config.defaults` (só a chave)
- `skills/herdr-soho/scripts/test/dispatch-arrival.test.mjs` (novo) e os
  testes existentes que mudarem por causa disso
- `skills/herdr-soho/SKILL.md` (só o trecho da chegada)
- goldens de paridade que mudarem pela espera ou pelas regras

## Forbidden

- Outros arquivos de `lib/`. Mandar prompt, teclas ou mensagem a panes
  reais.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera. Não rode a
suíte completa.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e as mutações
executadas.
