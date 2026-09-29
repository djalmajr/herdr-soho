# Emenda — pare a revisão e escreva a passagem de bastão

Role: reviewer · Report language: pt-BR

## Goal

A cota do Claude está no fim. Pare a revisão agora, sem rodar mais nada, e escreva a passagem de
bastão para um revisor Grok que vai continuar do ponto onde você parou.

## Expected result

O relatório, no caminho do contrato, curto:

1. Primeira linha: `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: fail` com os achados que você já
   tem (o `fail` marca a revisão como incompleta).
2. Achados até aqui: prioridade, confiança, arquivo:linha e a prova que você já colheu (cole só o
   essencial).
3. Itens do brief já verificados, com uma linha de resultado cada.
4. Itens que faltam, com o que você ia fazer em cada um.
5. Onde estão as sondas, harness, cópias e logs que você criou (caminhos em `/tmp`), para o próximo
   revisor reaproveitar.

## Forbidden

- Rodar comandos novos, mutações ou testes. Editar arquivos do repositório.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

O relatório acima, no caminho do contrato.
