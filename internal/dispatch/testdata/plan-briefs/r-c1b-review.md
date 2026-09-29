# Revisão R-C1b — núcleo de config em Go e os comandos `config`, `config set`, `session`

Role: reviewer · Agent: review-2 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

Revisar o porte das camadas de configuração (defaults, usuário, projeto, sessão, ambiente), da
sessão por workspace e da migração dos nomes antigos, e os três comandos que passaram a ser servidos
pelo Go. Quase todo comando futuro lê a config por aqui: uma chave com a fonte errada, uma precedência
trocada ou uma escrita que perde comentário aparece em tudo.

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-g2b`, agora em
  `9f57c3d` (HEAD destacado; pai `108acd2`). Diff: `git -C <worktree> show 9f57c3d`.
- Brief: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/c1b-go-core.md`.
- Relatório do implementador:
  `/tmp/herdr-soho/w14/reports/build-2-20260928T211819.md`.
- JS de referência: `<worktree>/skills/herdr-soho/scripts/lib/{config,session,legacy}.mjs` e os
  pedaços de `roles.mjs`/`lanes.mjs` que o Go portou.
- O `state.mjs` ficou de fora desta fatia de propósito (vem na C1c).

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. `config`, `config set` e `session set|show|clear`: diferencial seu, Go × JS, em cenários além dos
   do golden — arquivo com comentários e linhas em branco, CRLF, BOM, chave repetida, chave com
   pontos e hífens, valor com `=`, espaço e acento, legado presente e novo ausente, os dois presentes,
   `HERDR_SOHO_*` e `HERDR_AGENTS_*` no ambiente, fora de um workspace Herdr, `HERDR_SOHO_NOWRITE=1`.
   Compare stdout, stderr, código e o arquivo escrito byte a byte.
2. Precedência das camadas e `CfgSource` em todas as combinações das cinco camadas para uma chave
   escalar e uma pontuada (`lane.build.kind`).
3. Escrita atômica e permissões dos arquivos de config; a migração do arquivo legado (copia uma vez,
   deixa o antigo no lugar).
4. Os testes Go matam mutações suas (pelo menos cinco); `go vet`, `go test ./...`, `gofmt -l`,
   `GOOS=windows go vet`.

Ambiente Go: `GOTOOLCHAIN=local GOPROXY=off GOCACHE=/tmp/hs-go/review2/cache GOPATH=/tmp/hs-go/review2/gopath GOTMPDIR=/tmp/hs-go/review2/tmp`.
Nenhum Herdr real (`HERDR_SOCKET_PATH=/tmp/hs-go/review2/none.sock`; herdr falso quando precisar).

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real que escreva. Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
