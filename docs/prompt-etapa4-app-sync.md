# Prompt — Etapa 4 (app conectado à conta: sincronização)

Você vai implementar a **Etapa 4 do plano** `C:\Rota Smart\docs\plano-rotasuite-web-360.md`. Leia o plano inteiro antes de começar (as seções 1, 3 e 6 importam; a regra de conflito "vale a última" está na seção 3).

## Contexto

- **RotaSuite** é o app desktop Wails (Go + React) em `C:\Rota Smart` (module `rota-smart`, branch `main` para código, `release` para distribuição). Banco local SQLite por PC; hoje o "backup" é sync de arquivo no repo privado `Rota-Smart/banco-de-dados`.
- O usuário é leigo em termos técnicos. Explique as coisas em frases simples, sem jargão.
- **A API da nuvem já está no ar** em `https://site-back-10cd.onrender.com` (repo `Rota-Smart/Site-Back`, clonado em `C:\Rota Smart\Site\Backend`). Endpoints de dados: `/cards` (GET/POST/PATCH/DELETE), `/colunas`, `/acordos`, `/historico`, `GET /sync?desde=`. **`POST /sync` é pendência do backend da Etapa 4** — combine com o agente da API (contrato em `Site/Backend/API.md`) antes de implementar o motor de sync no app.
- O banco na nuvem (Supabase) já tem `usuario_id`, `atualizado_em`, `versao`, `apagado` em todas as tabelas sincronizadas (Etapa 1).
- O PC principal ainda vai apertar ☁️ Enviar dados para a nuvem (botão da v1.56.0). Sua etapa **substitui** esse envio único por sync contínuo — mas o botão pode continuar existindo até validar, depois desligamos.

## Regras obrigatórias

1. **O app tem que continuar funcionando sem internet.** SQLite local é a fonte de verdade do dia a dia; a nuvem sincroniza em segundo plano. Nada de travar a UI esperando rede.
2. **Regra de conflito: vale a última alteração** (`atualizado_em` + `versao`). Nunca pergunte ao usuário nada sobre conflito; só aplique a regra e registre no histórico.
3. **Token do pareamento fica no Windows Credential Manager** (go-keyring ou equivalente) — **jamais** no SQLite em texto puro nem em arquivo de config.
4. **Nunca teste escrita no 360** e nunca mande a senha do 360 para a API — o app não lida com senha do 360 nesta etapa, só com o token de pareamento do RotaSuite.
5. **Só use os MCPs com `rota` no nome** (`rota-supabase-site`, `rota-render-backend` etc.). Os genéricos apontam para outros projetos.
6. Não toque no site (`Site/FrontEnd`) nem no backend da API além de negociar o contrato `POST /sync`.
7. Ao publicar versão do app: bump `VersaoApp`, exe na branch `release`, zip refeito, conferir md5 do exe dentro do zip (pegadinhas conhecidas de publicação valem).

## O que fazer

1. **Pareamento:** tela "Conectar à conta" nas Configurações do app. O site gera um código de pareamento de uso único; o usuário digita o código no app; o app troca o código por um token longo via endpoint da API (se o endpoint não existir, anotar como pendência do backend e acordar o contrato antes de codificar).
2. **Motor de sync no app:** fila local de alterações pendentes (tabela de fila no SQLite ou marcação por registro), envio em lote, recebimento via `GET /sync?desde=<último>`, aplicação da regra "vale a última". Sync no início do app + periódico (ex.: a cada 1–2 min) + ao voltar a internet.
3. **Offline:** operações normais gravam só no local e entram na fila; quando a internet volta, reenvio automático com backoff. Erro de rede nunca aparece como erro para o usuário — só um indicador discreto "sincronizando / aguardando internet".
4. **Substituição do backup GitHub:** manter o sync do repo `banco-de-dados` funcionando em paralelo durante a validação (não remover agora); anotar no plano que desligaremos depois.
5. **Teste real:** rodar o app em modo dev com o banco da nuvem e conferir que uma alteração feita numa instância aparece na outra; testar offline (matar rede, mexer no quadro, religar e ver o reenvio).
6. **Atualizar o plano:** marcar as caixinhas da Etapa 4 e escrever resumo do que ficou pronto + pendências de backend.

## Entrega

- Dois PCs com o app mostrando o mesmo quadro, sem o usuário fazer nada além do pareamento uma vez.
- App 100% utilizável offline, sincronizando sozinho quando a internet existe.
- Token guardado no Credential Manager; nenhuma senha do 360 circulando pela API.
