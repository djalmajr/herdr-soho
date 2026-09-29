# Revisão R-C1a — `internal/herdr`, `internal/codexenv` e o `fakecli`

Role: reviewer · Agent: review · Report language: pt-BR

Continuação da sua revisão da G1b (mesmo porte; esta fatia usa o `RunCli` que você acabou de
revisar).

## Goal

Revisar o porte do cliente do `herdr` — por onde passa toda chamada da skill ao Herdr (estado do
agente, leitura de tela, prompt, teclas, painéis, abas) —, o do diagnóstico do ambiente do Codex, e o
`fakecli`, o CLI falso que os testes Go de todas as fatias seguintes vão usar.

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-g2`, agora em
  `deac93f` (HEAD destacado; pai `bbf39cd`). Diff: `git -C <worktree> show deac93f`. Atenção: esta
  base ainda não tem a G1b; o `RunCli` daqui é o do G1. Julgue o `herdr` supondo o `RunCli` já
  corrigido (o relatório do implementador diz onde dependia dele).
- Brief: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/c1a-go-herdr.md`.
- Relatório do implementador:
  `/tmp/herdr-soho/w14/reports/build-20260928T205925.md`.
- JS de referência: `<worktree>/skills/herdr-soho/scripts/lib/herdr.mjs` e `lib/codex-env.mjs`.
- Resultado no Windows dos testes destes pacotes (falham por prazos curtos contra o arranque do
  `.exe` e por `ps` no win32): `/tmp/hs-go/gowin2-failures.txt`. Já há uma fatia (GW2) nisso; não
  repita, a menos que ache defeito de produto.

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. `AgentState` contra o JS: classificação (`gone`, `unavailable`, `working`…), as pausas de nova
   tentativa, processo morto por sinal (exit ≥ 128) e a mensagem, JSON malformado, seq não inteiro.
   Use o `fakecli` e um herdr falso seu nos dois lados.
2. `RequireEnv` e o diagnóstico do Codex: mesmo texto e mesmo código do JS nos cenários de
   `codex-env.test.mjs`; `ParseCodexPolicy`/`EvaluateCodexPolicy` num diferencial seu com TOML
   hostil (tabelas em linha de várias linhas, aspas e escapes, comentários, CRLF).
3. As funções que devolvem JSON do herdr: a ordem das chaves preservada (`jsonjs.Parse`) onde o JS
   reescreve os dados.
4. O `fakecli`: seguro (não vaza para processos fora do teste, não acha o `herdr` real por engano),
   funciona no Windows sem `.cmd`, e a API serve às próximas fatias.
5. Mutações suas (pelo menos cinco); `go vet`, `go test ./...`, `gofmt -l`, `GOOS=windows go vet`.

Ambiente Go como antes. Nenhum Herdr real (`HERDR_SOCKET_PATH=/tmp/hs-go/review/none.sock`).

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real que escreva. Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
