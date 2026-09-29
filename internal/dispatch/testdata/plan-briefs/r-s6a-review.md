# Revisão R-S6a — `setup --plan`, `--detect`, `--probe` e `--plan --local` em Go

Role: reviewer · Report language: pt-BR

## Goal

Revisar as leituras do `setup` portadas para Go: o plano do que o `setup` escreveria, a detecção de
assistentes e o revisor recomendado, a sondagem de um assistente e as recusas de segurança do
`--plan --local`. Um plano que mostra algo diferente do que o `setup` faria, uma recusa que não
dispara ou uma recomendação de revisor da mesma família do implementador enganam o usuário antes de
ele escrever no próprio repositório.

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-s6a`, em `a451ed8`
  (HEAD destacado). Os dois commits a revisar:
  - `8163236` (S6a: `--plan`, `--detect`, `--probe`, `ownproviders`, `resolvedRoleKind`);
  - `a451ed8` (S6a-b: `--plan --local`, legado, timeout do `--detect`).

  Diffs: `git -C <worktree> show 8163236` e `git -C <worktree> show a451ed8`.
- Briefs:
  - S6a:
    `/work/herdr-soho/.herdr-soho/w14/plan-briefs/s6a-go-setup-read.md`;
  - S6a-b:
    `/work/herdr-soho/.herdr-soho/w14/plan-briefs/s6a-b-setup-local.md`;
  - emenda da S6a-b: `s6a-b-amend.md`, na mesma pasta.
- Relatórios:
  - S6a:
    `/work/herdr-soho/.herdr-soho/w14/reports/build-2-20260928T231246.current.md`;
  - S6a-b:
    `/tmp/herdr-soho/w14/reports/build-20260929T000123.md`.
- JS de referência: `<worktree>/skills/herdr-soho/scripts/lib/commands/setup-{plan,detect,probe}.mjs`,
  `lib/setuplocal.mjs`, `lib/ownproviders.mjs`, `lib/legacy.mjs`.
- Plano e convenções Go: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md`
  (seção 3).

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. Diferencial Go × JS do `--plan` (com e sem `--local`, `--panes`, `--lane`, `--set`,
   `--user-set`, `--session-set`, `--target`, `--no-hooks`). Varie os arquivos existentes: novo,
   legado, os dois, bloco antigo, CRLF, BOM. Compare stdout, stderr, código e confirme que nada é
   escrito.
2. Cada recusa do `--plan --local` com um caso que a dispara e um vizinho que não dispara. As
   recusas: symlink do alvo, do exclude e de `.git/info`; estado no repositório por alias ou
   diretório interior; raiz do worktree; espaço no fim; `HERDR_SOHO_DIR` inseguro; worktree ligado.
3. `--detect`: providers próprios do pi e do opencode, lanes customizadas, recomendação de revisor
   de outra família e ordem dos modelos (o mais novo primeiro). `--probe`: timeout, "not
   authenticated" e cota. Os assistentes são sempre falsos (nunca o CLI real).
4. Mutações suas (pelo menos seis, uma por recusa e uma na recomendação de revisor). Rode também
   `go vet ./...`, `go test ./...`, `gofmt -l cmd internal` e `GOOS=windows GOARCH=amd64 go vet ./...`.

Ambiente Go: `GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=mod`, `GOCACHE`, `GOPATH` e `GOTMPDIR` em
`/tmp/hs-go/review/s6a/`. Nenhum Herdr real (`HERDR_SOCKET_PATH=/tmp/hs-go/review/s6a/none.sock`).
Suíte Node contra o binário, se quiser: a partir de
`/work/herdr-soho/.worktrees/gate/skills/herdr-soho`, com
`HERDR_SOHO_TEST_BIN=<binário>`.

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Executar um assistente real (`claude`, `codex`, `grok`, `agy`, `cursor-agent`, `pi`, `opencode`).
  Herdr real. Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
