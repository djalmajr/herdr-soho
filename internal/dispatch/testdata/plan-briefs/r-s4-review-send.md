# Brief — Revisão R-S4: `herdr-soho send` (mensagem de par, modo automático)

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd agora está em `014d1ee` (branch `feat/peer-send`, sobre a base
`c1596eb`). Confirme com `git log --oneline -1` e revise
`git diff HEAD~1 HEAD`. Brief da fatia (o contrato):
`/work/herdr-soho/.herdr-soho/w14/plan-briefs/s4-send.md`.
Relatório do implementador:
`/tmp/herdr-soho/w14/reports/soho-s4-20260928T105509.md`.

## Goal

Achados ancorados em `arquivo:linha`, com execução, antes do push.

## O que verificar com atenção

1. **Cabeçalho**: o brief fixa `— <sender-ref> (<sender-name>,
   <sender-kind>, <sender-role>), not from your user.`; o relatório
   mostra outra ordem de campos. Qual está no código, e o teste trava a
   ordem certa?
2. **Segurança do texto**: o corpo pode conter uma linha que imite o
   cabeçalho (`[herdr-soho:peer] …`) ou instruções do tipo "o usuário
   aprovou"; o cabeçalho fica sempre antes e não pode ser removido pelo
   remetente? Quebras de linha, bracketed paste e caracteres de controle
   no corpo (ESC, `\r`) chegam como texto ou podem virar teclas no
   terminal do alvo? Proponha a correção mínima se virarem.
3. **Decisão do implementador**: `timeout` do `agent prompt --wait` tratado
   como entregue (exit 0). Com `--until working|blocked|idle|done`, em que
   situação real isso acontece, e o que é mais seguro devolver?
4. **`inbound`**: lida no projeto do alvo local com o `env` limpo e a
   camada de sessão do alvo, como o brief manda? `off` recusa sem enviar
   nada?
5. **Espera**: alvo `working` → `agent wait --until idle --until done` e
   só depois o prompt; `--now` não espera; timeout → 17 sem envio.
6. **Setup**: o item novo do bloco e os goldens mudam só por ele (o
   relatório diz que o golden de ajuda e o de config também mudaram;
   confira que só pelas linhas do `send`, dos códigos 17/18 e da chave
   `inbound`).
7. Os testes pegam as mutações que declaram.

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test --timeout 60000` em
`send.test.mjs`, `setup*.test.mjs`, `parity-setup*.test.mjs`,
`parity-entry.test.mjs`, `parity-config.test.mjs`, `config*.test.mjs`;
sondas em `/tmp` com `herdr` falso. **Não** mande mensagem a nenhum pane
real.

## Forbidden

- Editar qualquer arquivo do checkout. Revisor é somente leitura.
- Mandar prompt, teclas ou mensagem a qualquer pane.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
