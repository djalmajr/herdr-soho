# Goal
Publicar o backup diário e podar os arquivos antigos sem remover dados ainda necessários.

# Expected result
O backup do dia é publicado e os últimos sete dias são mantidos.

# Owned files
- `src/backup.sh`

# Forbidden
Não alterar os destinos de backup existentes. Não commit/push.

# Report
Relatar as fixtures locais e as provas operacionais pendentes.

## Failure matrix (somente para este fluxo de retenção)
- [crash] uma falha entre publicar e podar: o backup publicado permanece disponível
- [retry] repetir a mesma etapa após falha converge para o mesmo estado
