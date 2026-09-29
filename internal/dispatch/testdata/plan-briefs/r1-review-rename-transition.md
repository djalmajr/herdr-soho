# Brief — Revisão R1: renomeação herdr-agents → herdr-soho e camada de transição

Role: reviewer · Agent: review · Report language: pt-BR

Repositório: a raiz do seu cwd (worktree de `herdr-soho` fixa no commit
`2360abc`, topo de `feat/rename-herdr-soho`). Revise o intervalo
`3754071..2360abc` (`git diff 3754071 2360abc`, `git log 3754071..2360abc`;
`3754071` é o `main` atual). Dois commits: a renomeação mecânica e a camada
de transição.

## Goal

Achados concretos, ancorados em `arquivo:linha`, sobre correção,
compatibilidade e segurança da migração, antes do push.

## Contexto e decisões já tomadas (não reabra)

- Renomeação completa: skill `skills/herdr-soho`, CLI `scripts/herdr-soho`,
  plugin `djalmajr.herdr-soho`, `~/.config/herdr-soho/config`,
  `.agents/herdr-soho.conf`, `HERDR_SOHO_*`, estado `.herdr-soho/`,
  marcadores `<!-- herdr-soho:start/end -->`. `.agents/herdr-roles/` fica.
- Transição sem editar projetos: o CLI lê `HERDR_AGENTS_*` (quando o novo
  está vazio), os arquivos `herdr-agents` (enquanto o novo não existe; a
  primeira escrita copia), e `.herdr-agents/` (enquanto `.herdr-soho/` não
  existe); `setup` troca bloco e hooks antigos no lugar; `doctor` emite
  `legacy …`.
- Sem shim: a troca global é direta (`bunx skills remove herdr-agents`,
  `add … --skill herdr-soho`) e cada projeto legado roda `herdr-soho setup`
  uma vez; até lá o hook antigo imprime "skill script not found" (exit 0).
- Golden `parity-config` regravado só por alinhamento de coluna.

## O que verificar (em ordem de prioridade)

1. **Camada de transição** (`scripts/lib/legacy.mjs`, `config.mjs`,
   `lanes.mjs`, `commands/doctor.mjs`, `commands/setup.mjs`,
   `commands/setup-plan.mjs`, `setuptext.mjs`, `herdr-soho.mjs`): perda de
   configuração, estado ou escrita em projeto legado que não deveria
   acontecer; precedência errada entre nomes novos e antigos; cópia não
   atômica ou que altera o legado; caso em que um projeto legado ganha
   `.herdr-soho/` ou uma linha nova no `.gitignore` sem ação do usuário.
2. **Migração por `setup`** (`setuptext.mjs`, `commands/setup.mjs`,
   `test/legacy-migration.test.mjs`): o bloco antigo é trocado no lugar e o
   resto do arquivo fica; só os hooks antigos exatos saem; hooks e campos do
   projeto ficam; `setup --local` com bloco antigo em `CLAUDE.local.md`;
   um projeto com bloco antigo em `AGENTS.md` e `CLAUDE.md` separados.
3. **Renomeação**: nome antigo esquecido onde deveria ser novo, ou nome
   novo onde o antigo é necessário para compatibilidade; caminhos quebrados
   (lançadores, plugin `bridge.mjs`, `run-tests.sh`, `.cmd`); goldens
   alterados além do alinhamento; `README`/`SKILL.md`/`docs/guide.md`
   descrevendo algo que o código não faz.

## Checks you may run

- Leitura, `git diff`, `git log`, `git show`, `rg`/`grep`.
- Testes, sem editar nada: `node --test <arquivo>` e `bun test <arquivo>`
  em `skills/herdr-soho/scripts/test/`;
  `skills/herdr-soho/scripts/run-tests.sh --env outside <suite>`.
- Sondas em cópia descartável fora do repositório (`/tmp`), com `HOME`
  temporário. Nunca rode nada contra `~/.agents`, `~/.claude`, `~/.config`
  reais nem contra outros projetos.
- Antes de afirmar que um teste ou comando falha, rode-o e cite a saída;
  se não puder rodar, diga e reduza a confiança.

## Forbidden

- Editar qualquer arquivo do repositório. Revisor é somente leitura.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.
- Imprimir variáveis de ambiente ou linhas de comando de processos.

## Expected result

Relatório com a primeira linha exata
`findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` (em inglês),
depois um achado por item: severidade, `arquivo:linha`, cenário concreto
(entrada → resultado errado), evidência (saída colada) e correção sugerida.
`fail` quando restar P0 ou P1.

## Report

Relatório em Markdown no caminho do contrato, pt-BR, itens com
`[done]` / `[partial]` / `[skipped]` para cada uma das três áreas acima.
