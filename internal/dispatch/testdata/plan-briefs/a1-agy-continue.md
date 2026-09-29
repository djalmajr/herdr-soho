# Brief — A1 (continuação): chegada confiável do prompt a um agente recém-aberto

Role: implementer · Agent: build · Report language: pt-BR

Worktree `.worktrees/a1`, branch `fix/dispatch-arrival` (base `main`
`a138983`, ainda sem nenhuma mudança). Caminhos relativos à raiz dela.

Você continua uma tarefa que outro implementador começou e passou adiante
sem código (só leitura). Leia, nesta ordem:
1. O brief da tarefa (o contrato — decisões, critérios, arquivos):
   `/work/herdr-soho/.herdr-soho/w14/plan-briefs/a1-dispatch-arrival.md`
2. A passagem de bastão (onde está cada ponto no código, armadilhas,
   esboço da espera, goldens que mudam):
   `/tmp/herdr-soho/w14/reports/soho-a1-20260928T123206.md`

## Respostas às perguntas abertas da passagem

1. A espera de assentamento vale **sempre** (chave própria
   `prompt_settle_seconds`, `0` desliga), também com
   `prompt_check_seconds=0`.
2. O golden `test/golden/parity-config.json` pode mudar pela chave nova
   (linha na tabela do `config`); mostre o diff decodificado.
3. O aviso fica `prompt to '<agent>' did not settle in <n>s; sending anyway`.
4. As microdecisões da passagem (espera logo antes das leituras H0/preSeq,
   igualdade literal das duas leituras de tela) estão aceitas.

## Goal, critérios, arquivos permitidos e proibidos

Os do brief da tarefa (item 1 acima), sem mudança.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e as mutações
executadas.
