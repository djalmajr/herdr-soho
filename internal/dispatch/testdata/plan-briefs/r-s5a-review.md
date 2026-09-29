# Revisão R-S5a — `spawn` em Go

Role: reviewer · Report language: pt-BR

## Goal

Revisar o porte do `spawn`. Ele abre um painel para um papel e inicia a CLI do assistente. O kind,
o modelo, o esforço, as aprovações e os `args` saem da resolução da config. Ele também reusa worker
ocioso, nomeia o agente, escolhe onde dividir e espera a CLI ficar pronta, detectando diálogo,
provider e cota. No fim, refaz a grade e os rótulos. Um mapeamento de `--approvals full` errado dá
ao agente permissões que ninguém configurou. Um modelo ou kind trocado, ou um reuso de worker aberto
com outros argumentos, põe o assistente errado para trabalhar.

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-s5a`, em `9b0009e`
  (HEAD destacado; pai `2cf39a8`). Diff: `git -C <worktree> show 9b0009e`.
- Brief: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/s5a-go-spawn.md`.
- Relatório do implementador:
  `/tmp/herdr-soho/w14/reports/build-4-20260929T005716.md`.
  A falha que ele marcou é conhecida e está sendo corrigida noutra fatia: no `friction.log`, o regrid
  automático aparece com a categoria `regrid` em vez de `spawn`. Não precisa reportá-la de novo.
- JS de referência: `<worktree>/skills/herdr-soho/scripts/lib/spawn.mjs`, `lib/kinds.mjs`,
  `lib/models.mjs` e `lib/resolve.mjs`.
- Plano e convenções Go: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md`
  (seção 3).

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. Diferencial Go × JS do argv que o `spawn` monta para cada kind: `claude`, `codex`, `grok`, `agy`,
   `cursor`, `pi`, `opencode` e `gemini`. Cruze cada kind com:
   - cada `--approvals` (`ask`, `edits`, `full`);
   - cada esforço, inclusive acima do teto;
   - modelo por flag, lane, papel e kind;
   - `--kind` sem `--model`;
   - `args.<kind>` e `lane.*.args`;
   - args de retomada recusados (`-c`, `--resume`…).

   Use um `herdr` falso e CLIs falsas que gravam o argv. Compare byte a byte.
2. Reuso: worker ocioso compatível, args mudados, kind ou modelo diferentes e worker ocupado. Nome
   tomado recebe o sufixo, e `--name` tomado dá aviso.
3. Espera da CLI: diálogo de confiança (exit 7), erro de provider, cota (exit 11) e timeout. Confira
   que o `spawn` nunca responde um diálogo sozinho sem `auto_approve`.
4. Pelo menos seis mutações suas, uma delas no mapeamento de `--approvals full`, com o teste e o
   código de saída de cada uma. Rode também `go vet ./...`, `go test ./...`, `gofmt -l cmd internal`
   e `GOOS=windows GOARCH=amd64 go vet ./...`.

Ambiente Go: `GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=mod`, `GOCACHE`, `GOPATH` e `GOTMPDIR` em
`/tmp/hs-go/review/s5a/`. Nenhum Herdr real (`HERDR_SOCKET_PATH=/tmp/hs-go/review/s5a/none.sock`, ids
impossíveis como `w0test:p0a`).

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real: nunca abra um painel de verdade. Os assistentes reais (`claude`, `codex`, `grok`, `agy`,
  `cursor-agent`, `pi`, `opencode`): nunca rode. Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
