# Emenda — passagem de bastão para outro implementador

Role: implementer · Report language: pt-BR

Mudança de plano do operador: esta tarefa passa para outro implementador
(agy), que vai continuar **na mesma worktree**, sobre o que você já
deixou nela.

## O que fazer agora

1. **Pare de implementar.** Não comece nenhuma mudança nova. Se estiver no
   meio de uma edição, termine só a edição corrente para não deixar um
   arquivo quebrado (sintaxe válida), e pare.
2. Escreva o relatório de passagem, com:
   - por item do brief (e da emenda, se for o caso): `[done]` / `[partial]`
     / `[not started]`, com o que falta em cada `[partial]`;
   - arquivos que você tocou (e se cada um está em estado consistente);
   - testes: o que roda e passa agora (cole a saída de `node --test` do(s)
     arquivo(s) de teste da tarefa), o que falha e por quê;
   - mutações já executadas e as que faltam;
   - armadilhas que você encontrou (o que não funcionou, decisões que
     tomou e por quê, perguntas abertas).
3. Não rode a suíte completa. Nenhum comando git que escreva. No commit,
   push, tag, or PR.

## Report

Relatório em Markdown no caminho do contrato desta emenda.
