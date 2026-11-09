# Prompt — Etapa 7 (integração 360 no site, somente leitura + visão pessoal)

Você vai implementar a **Etapa 7 do plano** `C:\Rota Smart\docs\plano-rotasuite-web-360.md`. Leia o plano inteiro antes de começar — em especial a **seção 9 ("Descobertas")**, que já mapeou as telas do 360, os endpoints e o cálculo da fidelidade de Ijuí.

## Contexto

- **RotaSuite** (app desktop Wails, Go + React) está indo para a web: Supabase, API Go na Render (`https://site-back-10cd.onrender.com`, repo `Rota-Smart/Site-Back`, pasta `C:\Rota Smart\Site\Backend`) e site na Cloudflare (`https://360.foundrycode.com.br`, repo `Rota-Smart/Site-Front`, pasta `C:\Rota Smart\Site\FrontEnd`).
- O **Rota 360** (`https://360.rotagroup.com.br`) é o sistema da empresa. Não tem API oficial. **Escrita no 360 é proibida** — mudança oficial de ticket (status, resposta, novo ticket, responsável) continua no portal; o site oferece link "Abrir no 360".
- O usuário é leigo em termos técnicos. Explique em frases simples.
- Esta etapa pressupõe as Etapas 3 (login) e 5 (kanban no site) feitas. A etapa 5 criou a camada única de chamadas à API (`api.ts`) — use-a.

## O que já existe

- **Login:** sessão RotaSuite (cookie `rotasuite_sessao`); a API guarda a sessão do 360 do usuário, criptografada, e pode ler o 360 em nome dele.
- **Proxy de leitura na API** (criado na Etapa 3, "se der tempo"): `GET /360/tickets` e `GET /360/clientes?busca=`. **Confira antes se existem e o que devolvem**; se não existirem ou não cobrirem o que esta etapa precisa, é você quem cria as rotas novas no Site-Back (esse caso é exceção às regras: mexa só no que faltar, seguindo as regras de segredos e publicação do backend).
- **Endpoints de leitura do 360 conhecidos** (seção 2 e 9 do plano): `/api/helpdesk/tickets`, `/helpdesk/tickets/<uuid>`, `/api/cliente/buscar`, `/api/cliente/<cod>/dados`, `/api/cliente/<cod>/faturas`, `/api/cliente/<cod>/produtos`, `GET /api/cliente/<cod>/contratos`.
- **Fidelidade de Ijuí (já descoberto):** término do contrato = `DataAssinatura` convertida para o fuso `America/Sao_Paulo` **antes** de somar + `Prazo` meses (ex.: assinatura `2026-02-23T00:00:00Z` = 22/02/2026 em Brasília + 24 meses = 22/02/2028). `TérminoContrato` veio `null` na amostra. Não há endpoint que devolva a unidade ordenada por vencimento — é preciso listar os clientes e consultar `/contratos` de cada um (com cache).
- **Site:** quadro kanban pronto e responsivo (Etapa 5), tema azul-escuro glass, claro/escuro, noindex. Pegadinha Tailwind v4: classes próprias em `@layer components`.

## Regras obrigatórias

1. **Escrita no 360 é proibida.** Toda ação desta etapa é leitura ou organização da **visão pessoal no RotaSuite** (colunas visíveis, filtros salvos, ordem). Nada chama endpoint de escrita do 360, nem por engano.
2. **Só leitura do 360 pela API**, com o cookie/sessão do usuário logado. O site nunca fala com o 360 direto.
3. **Cache curto (alguns minutos)** nas rotas de proxy do 360, para não sobrecarregar o sistema deles — e um botão/indicação de "atualizar" claro.
4. **Sessão do 360 vencida:** a API responde 401 com mensagem clara; o site mostra "sessão do 360 expirou, entre de novo" e volta ao login.
5. **Segredos:** só Secret Files da Render (um arquivo por valor em `/etc/secrets/<NOME>`); nunca variável de ambiente; se precisar de segredo novo, diga ao usuário o nome e conteúdo exatos (exceção: `PORT`).
6. **Só MCPs com `rota` no nome.** Os genéricos apontam para outros projetos.
7. Não toque no app desktop (`C:\Rota Smart`, branch `release`).
8. `noindex` em qualquer página nova.

## O que fazer

### 1. API (só o que faltar no proxy)
- `GET /360/tickets` com filtros (busca por texto, status, data, solicitante, **unidade** — a conta é presa a Ijuí, mas o filtro de unidade existe), paginação como no 360 (25/pág).
- `GET /360/clientes?busca=` (nome/código) e `GET /360/cliente/<cod>` agregando `/dados`, `/faturas`, `/produtos`, `/contratos`.
- `GET /360/fidelidade` — clientes da unidade Ijuí ordenados pela vigência mais próxima (calculada como acima), com cache (ex.: 15 min) porque são várias consultas.
- `GET /360/ticket/<uuid>` para o detalhe do ticket (descrição + interações), se a tela de tickets precisar.

### 2. Site — tela **Tickets**
- Fila aberta da conta com a leitura do proxy: Nº, título (+ código service e cliente embaixo), status, setor, responsável, unidade, categoria, SLAs, criado em.
- Busca por texto e filtros (status, data, solicitante, unidade). Paginação.
- **Visão pessoal do RotaSuite** (salva na nuvem, compartilhada com o app futuro): colunas visíveis, filtros salvos, ordem da fila — tudo em tabelas novas do Supabase com `usuario_id` e RLS (migration, se você for quem mexe no banco; senão, negocie com o agente do backend).
- Botão **"Abrir no 360"** em cada ticket (link direto ao portal).
- Botão **"Criar card"** (a partir de um ticket/cliente) que gera um card no kanban do RotaSuite — dado do RotaSuite, não do 360; usa os endpoints de cards que já existem.

### 3. Site — tela **Clientes**
- Busca por nome/código com filtro de unidade (Ijuí).
- Ficha: dados do cadastro, equipamentos, OS ativas, faturas em aberto, produtos, **contratos**.
- **Seção "Fidelidade de Ijuí"** (pode ser uma tela própria): clientes da unidade ordenados pela data de término mais próxima, com quantos meses faltam.

### 4. Testar e publicar
- Teste local com o login real do usuário (senha digitada por ele, nunca salva).
- Publicar no site e na API (push na main; conferir deploy pelos logs do MCP `rota-render-backend`).
- Atualizar as caixinhas da Etapa 7 no plano e escrever um resumo do que ficou pronto.

## Entrega

- Telas Tickets e Clientes no site consultando o 360 **só em leitura**, via API, com cache curto.
- Visão pessoal da fila salva e compartilhável.
- Fidelidade de Ijuí calculada por assinatura + prazo (fuso de Brasília) e ordenada por término.
- "Abrir no 360" para mudanças oficiais; nada gravado no 360.
