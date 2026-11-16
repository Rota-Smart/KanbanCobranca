# Etapa 1 — Banco na nuvem (o que falta / o que setar onde)

> Status 2026-11-08: esquema criado, RLS ativa, migração rodou a partir deste PC.
> O que resta você faz manualmente nos pontos listados abaixo. **Nada de variáveis
> de ambiente: tudo é secret/arquivo.**

## O que já está feito

1. **Esquema Postgres** no projeto Supabase "Rota - Project" (`geucyqohuzbuzumocisy`):
   16 tabelas espelhando o `rotasmart.db` + colunas de sincronização
   (`usuario_id`, `atualizado_em`, `versao`, `apagado`) em todas.
2. **RLS ativa**: usuários logados só veem dados sem dono ou com dono = eles;
   o papel `rotasuite_api` (usado pela API e pela migração) tem acesso total.
3. **Gatilho**: todo UPDATE incrementa `versao` e atualiza `atualizado_em`
   (regra "vale a última" já sai pronta).
4. **Script de migração**: `migra_sqlite_supabase.py` (idempotente — pode rodar
   quantas vezes quiser; sobrescreve linha a linha preservando os ids).

## ⚠️ IMPORTANTE — envie os dados do PC principal (pelo app, sem script)

A partir do **app v1.56.0** (chega pela atualização automática): Configurações →
seção **Nuvem (site RotaSuite)** → cole a linha de conexão abaixo → **Salvar e testar**
→ **☁️ Enviar dados para a nuvem**. Pronto — leva uns segundos.

### Alternativa: script (se preferir)

O `rotasmart.db` deste notebook está **desatualizado** (o banco de verdade é o do
outro PC). No PC principal, copie a pasta `Site\migracao` e rode:

```
python migra_sqlite_supabase.py --sqlite "%APPDATA%\RotaSmart\rotasmart.db" --dsn-arquivo dsn.txt
```

(O `dsn.txt` precisa estar na mesma pasta. Quem rodar por último "vence" — depois
disso a sincronização da Etapa 4 assume.)

## SECRETS — o que setar em cada lugar

A ÚNICA chave nova da etapa é a conexão do Postgres:

```
postgresql://rotasuite_api.geucyqohuzbuzumocisy:Rst7-kvw2-Xq94-mPLa-8zJe-5Tn3-Bqhu-2Wdf@aws-0-us-east-2.pooler.supabase.com:5432/postgres
```

| Onde | O que setar | Como |
|---|---|---|
| **Este PC / PC principal** (script de migração) | A linha acima | Cole em `Site\migracao\dsn.txt` (já está; arquivo fora do git) |
| **Render — serviço Site-Back** (Etapa 2) | A linha acima | Dashboard do serviço → **Secret Files** → crie `secrets.json` com `{"DATABASE_URL": "<linha acima>"}`. NÃO usar a aba "Environment". |
| **Supabase** | Nada a fazer — o papel `rotasuite_api` já existe com essa senha | Se um dia quiser trocar a senha: SQL Editor → `ALTER ROLE rotasuite_api PASSWORD 'nova-senha-forte';` (e atualize o dsn.txt/secrets.json) |

Segurança da senha: ela vale só para o projeto "Rota - Project", tem permissão
apenas de dados (não de schema) e está nos dois lugares acima. Se vazar, troque
com o ALTER ROLE.

## Checklist para fechar a Etapa 1

- [x] Esquema igual ao SQLite (16 tabelas)
- [x] `usuario_id`/`atualizado_em`/`versao`/`apagado` em todas
- [x] RLS ativa
- [x] Script de migração testado e rodado (465 linhas deste PC)
- [ ] **Rodar o script no PC principal** (dados reais)
- [ ] Conferir no Supabase (Table Editor) que o quadro apareceu com os cards de lá

Depois disso: Etapa 2 (API). O serviço Render "Site-Back" já existe mas o build
está falhando — conserto fica para a Etapa 2.
