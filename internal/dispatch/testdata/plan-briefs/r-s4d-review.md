# Brief — Revisão R-S4d: prova de chegada e diálogo no `send`

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está em `88c39a7` (branch `feat/peer-send`). Confirme com
`git log --oneline -1` e revise `git diff HEAD~1 HEAD`. Brief (o contrato):
`/work/herdr-soho/.herdr-soho/w14/plan-briefs/s4d-send-arrival-dialog.md`.
Relatório do implementador:
`/tmp/herdr-soho/w14/reports/soho-agy-s4d-20260928T150404.md`.

## Por que existe

Na validação real, o `send` disse `sent` a um Codex que não recebeu nada, e
digitou a mensagem no diálogo "Trust this workspace" de um Cursor (uma
letra respondeu o diálogo). A fatia troca o `agent prompt --wait` por uma
prova de chegada pelo id da mensagem e recusa enviar a uma tela de diálogo.

## O que verificar com atenção

1. **Mensagem duplicada.** O reenvio único acontece quando o id não aparece
   nas 60 linhas recentes. Em que casos reais uma mensagem que **chegou**
   fica sem o id visível e é reenviada? Considere: corpo com mais de ~57
   linhas (o id está na 1ª linha do cabeçalho); agente que responde rápido
   e empurra o cabeçalho para fora das 60 linhas; colagem recolhida pelo
   CLI (Claude Code mostra `[Pasted text #1 +N lines]`, Codex mostra
   `[Pasted Content N chars]`); `agent read` que falha (a função devolve
   `''`, igual a "ausente"). Diga quais são alcançáveis e o que propõe
   (sem inventar flags: proponha, não decida).
2. **Falso diálogo.** `isDialogScreen` testa a tela visível inteira contra
   `[y/N]`, `(y/n)`, `Do you trust` etc. Um histórico de conversa que só
   **cita** esses textos bloqueia o `send` até o `--timeout` (padrão 10
   min) e sai 17? Compare com como os detectores de `dialog.mjs` delimitam
   a região da tela.
3. **Testes existentes enfraquecidos?** `send.test.mjs` mudou ~290 linhas:
   alguma asserção removida, expectativa afrouxada ou caso pulado sem ser
   pela mudança decidida? Cite linha a linha.
4. As variáveis `HERDR_SOHO_SEND_WINDOW_MS`/`HERDR_SOHO_SEND_POLL_MS` não
   estão no brief: servem só a testes? Há precedente de variável de teste
   no código (procure `HERDR_SOHO_` em `lib/`)? Afetam uso real?
5. `appendPeerLog` com assinatura sobrecarregada (id ou env na 6ª
   posição): algum chamador passa o env na posição errada?
6. O cabeçalho passou a ser montado com o texto literal em vez de
   `PEER_PREFIX`: o marcador exportado ainda casa com o que o bloco de
   setup e a documentação dizem?
7. Os testes novos pegam as mutações que declaram (rode ao menos três em
   cópias em `/tmp`).

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test --timeout 60000` em
`send.test.mjs`, `setup*.test.mjs`, `parity-setup.test.mjs`,
`parity-entry.test.mjs`; mutações em cópias em `/tmp`.

## Forbidden

- Editar qualquer arquivo do checkout. Revisor é somente leitura.
- **Nenhuma execução do `send` fora dos testes**, e nenhuma chamada ao
  Herdr real que escreva (`agent prompt`, `send-keys`, painéis, nada):
  sondas só com `herdr` falso no `PATH` **e** `HERDR_SOCKET_PATH`
  apontando para um caminho inexistente, com ids de painel impossíveis
  (`w0test:p0a`). Uma revisão anterior mandou mensagens a um painel real
  de outra equipe; não repita.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
