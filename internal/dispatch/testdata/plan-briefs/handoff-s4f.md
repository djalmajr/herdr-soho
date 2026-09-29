# Handoff S4f — retomada

## Feito e verificado
- Revisei o `git diff` herdado em `send.mjs`, `peer.mjs` e `send.test.mjs`; mantive e completei as partes compatíveis com o contrato.
- Decisão 1: prova por `state_change_seq` exige preStatus `idle`/`done`, novo status `working`/`blocked` e tela sem diálogo; caso `--now` já trabalhando não prova chegada. Testes de falso positivo e prova válida por estado cobertos.
- Decisão 2: antes do Enter relê tela e status; diálogo dá exit 17, leitura visível falha dá 15 `unverified`, e só envia Enter se `#<id>` normalizado estiver nas últimas 15 linhas não vazias. Teste do marcador com espaços/desenho de caixa.
- Decisão 3: normalizador remove espaços e U+2500–U+257F; prova (b) exige id no histórico, tela alterada, footer normalizado ausente e id ausente da tela (evita confundir viewport cortada). Casos acima de 15 linhas, viewport e footer quebrado não retornam `sent`; caso válido (b) permanece.
- Decisão 4: tela de diálogo é avaliada com status recém-lido, inclusive após wait; teste de pergunta que vira `blocked` não envia prompt.
- Documentação de `send` atualizada em `skills/herdr-soho/SKILL.md` e `docs/guide.md`.
- Matriz prescrita: `node --test ...send.test.mjs ...setup*.test.mjs ...parity-setup.test.mjs ...parity-entry.test.mjs` — 230/230 passou.
- Matriz Bun equivalente (`bun test --timeout 60000 ...`) — 226/226 passou, 0 falhas.

## Falta / próximo passo
- Mutação final ainda incompleta; revisar a seção abaixo e executar novamente em cópia isolada, depois conferir diff final sem descartar mudanças herdadas.
- Não rodei a matriz Bash (o brief limita os checks aos critérios Node/Bun; deixá-la para o orquestrador).
- O relatório autoritativo ainda não foi escrito: `/tmp/herdr-soho/w14/reports/build-20260928T171145.md`.

## Onde parei; mutações
- Cópia `/tmp/s4f-mutation.jnUTzC/project`; `herdr-soho mutation-guard` passou (`ok copy-outside-source`, `ok no-symlink-into-source`, `ok build-env`, `ok cargo-config`). A cópia foi restaurada ao fim; o worktree não foi mutado pelos testes de mutação.
- Mutantes `dialog-a`, `dialog-b`, `preenter-read`, `stale-status` e `viewport-id`: testes direcionados falharam como esperado (detectaram cada regressão).
- `working-seq`: não executou o teste; `node` tratou padrão iniciado por `--now` como argumento ausente (exit 9). Repetir com padrão iniciado por `arrival:`.
- `proof-b-disabled`: execução deu exit 0 porque o padrão regex não selecionou o teste (arquivo contado como 1 teste). Repetir com padrão correto, por exemplo `proof \\(b\\) alone` validado antes.
- Mutação `input-id-normalization` sobreviveu porque selecionei o teste antigo de caixa sem espaços no ID; usar `arrival: id only in input box`, que contém marcador normalizado.
- Mutação da normalização não rodou: âncora Python não encontrada. Aplicá-la em cópia e rodar `normalizeScreen removes whitespace`.

## Armadilhas
- O brief-base está no checkout principal em `.herdr-soho/w14/plan-briefs/s4f-proof-tighten.md`; no worktree essa pasta não existe. Leia também o review R11 em `/tmp/herdr-soho/w14/reports/soho-grok-r11-20260928T162634.md`.
- Testes usam Herdr falso, ids impossíveis e socket isolado; não chamar `send` contra painel real.
- Não sobrescrever o diff parcial com checkout/restore; os arquivos de código/teste e os dois docs já têm alterações intencionais.
