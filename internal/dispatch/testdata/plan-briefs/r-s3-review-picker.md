# Brief — Revisão R-S3: seletor de sessão no plugin do Herdr

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está em `733d50d` (branch `feat/session-picker`, sobre a base
`c1596eb`). Confirme com `git log --oneline -1` e revise
`git diff HEAD~1 HEAD`. Brief da fatia (o contrato):
`/work/herdr-soho/.herdr-soho/w14/plan-briefs/s3-picker.md`.
Nesta branch ainda não existe o comando `find` (ele está noutra branch);
o seletor o chama como subprocesso e os testes usam um CLI falso.

## Goal

Achados ancorados em `arquivo:linha`, com execução, antes do push.

## O que verificar com atenção

1. **Terminal**: o seletor restaura o modo do terminal (raw off, cursor)
   em toda saída — Enter, Esc, Ctrl-C, erro de carga, `SIGTERM` do
   `herdr plugin pane close`? Entrada estranha (colar texto longo, setas
   em sequência, UTF-8 com acento) não quebra o estado.
2. **Injeção**: rótulos, títulos e cwd vêm dos panes (texto de terceiros);
   caracteres de controle ou escapes ANSI num título podem mexer no
   terminal ao serem desenhados? E no texto copiado?
3. **Clipboard**: a escolha por plataforma e o fallback OSC 52 (bytes
   exatos, texto grande); o PowerShell recebe o texto por stdin sem
   reinterpretar aspas ou `$`.
4. **Ação `pick`**: abre o painel com os argumentos do contrato; não muda
   `doctor`/`roster`.
5. **Carga**: máquina remota lenta ou que falha não trava a lista local;
   subprocessos terminam quando o seletor fecha (sem órfãos).
6. Os testes pegam as mutações que declaram.

## Checks you may run

Leitura, `git diff/log/show`; `node --test plugin/test/` e
`bun test --timeout 60000 plugin/test/`; sondas em `/tmp` (sem abrir
painéis de verdade e sem ligar o plugin).

## Forbidden

- Editar qualquer arquivo do checkout. Revisor é somente leitura.
- Abrir/fechar painéis de verdade, ligar/desligar o plugin, mandar prompt
  ou teclas a panes.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
