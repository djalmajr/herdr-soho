# Revisão R-C1c — `state` e `tasks` em Go, e `roster`, `friction`, `title`, `clean`, `feedback`

Role: reviewer · Agent: review-2 · Report language: pt-BR

Continuação da sua revisão da C1b (mesmo pacote `internal/core`).

## Goal

Revisar o porte do diretório de estado — id do workspace, roster e o lock dele, friction, ponteiros de
relatório, título de tarefa — e dos cinco comandos que passaram ao Go. O roster e o lock são
compartilhados por orquestradores do mesmo workspace: uma linha corrompida ou um lock que não protege
aparece como agente perdido ou duplicado.

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-g2b`, agora em
  `db09469` (HEAD destacado; pai `77070c7`). Diff: `git -C <worktree> show db09469`.
- Brief: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/c1c-go-state.md`.
- Relatório do implementador:
  `/tmp/herdr-soho/w14/reports/build-2-20260928T214622.md`
  (ele marcou como `[skipped]` vários casos unitários cobertos só pela caixa-preta: diga se a
  cobertura basta).
- JS de referência: `<worktree>/skills/herdr-soho/scripts/lib/{state,tasks}.mjs` e
  `lib/commands/{roster,friction,title,clean,feedback}.mjs`.

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. Diferencial Go × JS dos cinco comandos e do estado (roster com linhas antigas de 8 colunas e novas
   de 15, colunas vazias, CRLF, nome com caracteres estranhos; friction com texto hostil; `clean` com
   agentes vivos e mortos; `feedback send` com `feedback_dir` inválido e `feedback_to` ausente), com
   o `herdr` falso. Compare stdout, stderr, código e os arquivos do estado.
2. O lock do roster: dois processos Go, um Go e um JS ao mesmo tempo, lock velho, lock que não dá para
   remover. Nenhuma linha perdida ou duplicada.
3. `workspaceId` e a raiz do estado nos casos da B2 (worktree ligado, `HERDR_SOHO_DIR`).
4. Mutações suas (pelo menos cinco); `go vet`, `go test ./...`, `gofmt -l`, `GOOS=windows go vet`.

Ambiente Go como antes. Nenhum Herdr real.

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real que escreva. Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
