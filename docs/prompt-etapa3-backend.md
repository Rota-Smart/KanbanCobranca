# Prompt — Etapa 3 (backend do login pelo 360)

Você vai implementar o **backend da Etapa 3 (login pelo 360)** do plano `C:\Rota Smart\docs\plano-rotasuite-web-360.md`. Leia o plano inteiro antes de começar. A seção 9 ("Descobertas") traz o que já se sabe do login do 360.

## Contexto

- **RotaSuite** é um app desktop Wails (Go + React) de Kanban de cobrança. O plano o leva para a web: banco na nuvem, API, site e login pelo 360.
- O **Rota 360** (`https://360.rotagroup.com.br`) é o sistema da empresa onde o usuário trabalha. Ele não tem API oficial nem OAuth.
- O que já se sabe do login do 360 (seção 9 do plano):
  - É feito com e-mail + senha em `/login`, com a opção "Mantenha-me conectado".
  - A sessão é um **cookie httpOnly**; não há token no localStorage.
  - A conta fica presa à unidade Ijuí.
  - O tempo de vida da sessão ainda não foi medido; o histórico sugere que é preciso logar todo dia.
- O usuário é leigo em termos técnicos. Explique as coisas em frases simples, sem jargão.

## O que já existe (Etapas 0, 1 e 2 prontas)

- **Banco:** projeto Supabase "Rota - Project" (`geucyqohuzbuzumocisy`, us-east-2).
  - 16 tabelas espelham o SQLite do app, todas com `usuario_id`, `atualizado_em`, `versao` e `apagado`.
  - Já existe a tabela `usuarios` (id uuid, email, nome, unidade, criado_em), por enquanto vazia.
  - RLS está ativa. As policies para o papel `authenticated` usam `usuario_id = auth.uid()` ou `usuario_id is null`; o papel `rotasuite_api` tem acesso total.
- **API Go:** repo privado `Rota-Smart/Site-Back`, clonado em `C:\Rota Smart\Site\Backend`.
  - Arquivos: `main.go`, `internal/api/api.go` (handlers) e `internal/repo/repo.go`. Usa `lib/pq` e só a stdlib de HTTP.
  - Rotas: `/health`, `/cards` (GET/POST/PATCH/DELETE), `/colunas`, `/acordos`, `/historico`, `GET /sync`.
  - Hoje toda rota exige `Authorization: Bearer <API_TOKEN>`, um token fixo. **Esse token não pode ir para o site público (LGPD)**, e é isso que a Etapa 3 substitui.
  - CORS libera `https://360.foundrycode.com.br` e `localhost:5173`.
  - Está publicada na Render: serviço `Site-Back` (`srv-dbobu5bl550s7399g5b0`, workspace `tea-danbikmgekts738e05r0`), URL `https://site-back-10cd.onrender.com`. Cada push na `main` faz deploy automático.
- **Segredos:** a API lê Secret Files da Render, um arquivo por valor: `/etc/secrets/DATABASE_URL` e `/etc/secrets/API_TOKEN`. Em desenvolvimento local, lê `secrets.json` (está no gitignore).
  - A conexão do banco (formato pooler IPv4, a única que funciona na Render) está em `C:\Rota Smart\Site\migracao\dsn.txt`.
  - O `secrets.json` local da pasta Backend tem a conexão e o token.
- **Site (front):** `https://360.foundrycode.com.br`, na Cloudflare. Outro agente cuida do visual; você **não** mexe no front.

## Regras obrigatórias

1. **Nunca use variáveis de ambiente para segredos.** Use só Secret Files da Render (um arquivo por valor em `/etc/secrets/<NOME>`). Quando precisar de um segredo novo, **não crie você mesmo**: diga ao usuário exatamente o nome do arquivo e o conteúdo. A única exceção é a `PORT`, que a Render injeta.
2. **Só acesse dados deste projeto, e só pelos MCPs com `rota` no nome** (`rota-supabase-site`, `rota-render-backend` etc.). Os MCPs genéricos (`supabase`, `fcode-*`) apontam para **outros projetos** e não devem ser usados.
3. **Escrita no 360 é proibida.** Nada de responder ticket nem mudar status. O login (POST de credenciais) é permitido porque só autentica. Não faça testes que mudem dados no 360.
4. **Nunca guarde a senha do 360.** Guarde só a sessão (cookie) que o 360 devolve, e **criptografada** no banco.
5. Não toque no app desktop (repo `Rota-Smart/KanbanCobranca`, `C:\Rota Smart`, branch `release`). Se precisar ler código dele, use `git show main:<caminho>`.

## O que fazer

### 1. Descobrir o login (só leitura)
- Abra `https://360.rotagroup.com.br/login` no navegador da sessão. Peça ao usuário que **saia da conta e entre de novo**, e capture a requisição de login:
  - URL, método e formato do corpo (form ou JSON) e nomes dos campos;
  - campos ocultos ou CSRF, se houver;
  - nome e atributos do cookie de sessão devolvido;
  - redirecionamentos.
- Descubra qual endpoint autenticado e barato serve para confirmar que a sessão é válida e para obter nome, e-mail e unidade do usuário (por exemplo, a tela de perfil).
- Registre tudo na seção 9 do plano, em "Login e sessão".

### 2. Implementar no Site-Back
- **`POST /auth/login`** com `{email, senha}`:
  - repassa ao 360 e captura o cookie;
  - valida a sessão buscando o perfil;
  - faz upsert em `usuarios` (por e-mail);
  - guarda o cookie do 360 **criptografado** (AES-GCM, chave num Secret File novo, por exemplo `/etc/secrets/CHAVE_SESSAO`) numa tabela nova, por exemplo `sessoes`, com id, usuario_id, cookie_360_enc, criado_em, expira_em, ultimo_uso;
  - devolve um cookie **próprio do RotaSuite**: httpOnly, Secure, SameSite (escolha considerando que o front é `360.foundrycode.com.br` e a API é `onrender.com`), com expiração.
- **`POST /auth/logout`** e **`GET /auth/eu`**, que devolve o usuário logado.
- **Middleware:** toda rota de dados aceita a sessão RotaSuite.
  - Mantenha o `API_TOKEN` só para o app desktop e para scripts; o site **nunca** o usa.
  - Grave `usuario_id` nas criações e filtre os dados pelo usuário. Linhas com `usuario_id null` continuam visíveis para os usuários da unidade, porque vieram da migração.
- **Sessão do 360 vencida:** se o 360 recusar a sessão, a API responde 401 com uma mensagem clara ("sessão do 360 expirou, entre de novo").
- **Proteção contra força bruta** no `/auth/login`: limite por IP e por e-mail, guardando só o hash do IP.
- Mudanças de esquema entram como **migration** (`apply_migration` do MCP `rota-supabase-site`), sempre com RLS ativa nas tabelas novas.
- **Proxy de leitura do 360** (base da Etapa 7, se der tempo): rotas `GET /360/tickets` e `GET /360/clientes?busca=` que usam o cookie do usuário logado e só leem. Os endpoints de leitura conhecidos estão na seção 2 do plano.

### 3. Testar e publicar
- Teste localmente com o login real do usuário (ele digita a senha; você não a guarda em nenhum arquivo) e confira as rotas com o cookie.
- Faça push na `main` do Site-Back e confira o deploy pelos logs do MCP `rota-render-backend` (`list_logs`, `list_deploys`).
- Informe ao usuário os Secret Files novos que ele precisa criar na Render (nome e conteúdo exatos).
- Ao final, atualize as caixinhas da Etapa 3 no plano e escreva um resumo curto para o agente do visual: as rotas de auth, o formato das respostas, como o front deve chamar (com `credentials: 'include'`) e as mensagens de erro.

## Entrega
- Login pelo 360 funcionando na API publicada.
- Senha nunca armazenada; sessão do 360 criptografada.
- Rotas de dados protegidas pela sessão RotaSuite.
- Plano atualizado e contrato da API documentado em `C:\Rota Smart\Site\Backend\API.md`.

## Adendo — o que o site (front) já espera (2026-11-08)

O site já está pronto em `C:\Rota Smart\Site\FrontEnd` (repo `Rota-Smart/Site-Front`) e chama a API assim. Siga este contrato:
- `POST /auth/login` com o corpo `{"email","senha"}`: em caso de sucesso, devolve `{"id","email","nome","unidade"}` e grava o cookie de sessão. Em caso de erro, devolve `{"erro":"mensagem para o usuário"}` com 401 ou 400.
- `GET /auth/eu` devolve `{"id","email","nome","unidade"}`, ou 401 se não houver sessão.
- `POST /auth/logout` devolve 204.
- Todas as chamadas do site usam `fetch(..., { credentials: 'include' })` a partir de `https://360.foundrycode.com.br` para `https://site-back-10cd.onrender.com`, ou seja, **domínios diferentes**. Por isso:
  - o CORS precisa do header `Access-Control-Allow-Credentials: true` (hoje ele falta em `main.go`) e manter `Access-Control-Allow-Origin` com a origem exata;
  - o cookie precisa ser `SameSite=None; Secure; HttpOnly`.
- O site trata qualquer 401 nas rotas de dados como "sessão terminou" e volta para o login.

## Adendo 2 — endpoints que as abas do card no site usam (2026-11-08)

O card do site tem as mesmas 9 abas do app: Acordo, Proposta, Cliente, Ticket, Visita, Contatos, Observações, Financeiro e Histórico. Hoje, se uma rota não existe (404), a aba mostra "ainda não ligado no servidor". Para funcionar de verdade, faltam estas rotas, todas protegidas pela sessão e com `usuario_id` gravado:

| Rota | Corpo / retorno |
|---|---|
| `POST /acordos` | `{ticket_id,total_aberto_cents,entrada_cents,numero_vezes,parcela_base_cents,status}`. O servidor **recalcula** com a mesma regra do app (`internal/service/reparcelamento.go`), desativa o acordo ativo anterior do card e devolve o `Acordo` completo (mesmo JSON de `GET /acordos`). |
| `GET /vinculos?ticket_id=` | `[{id,ticket_id,ticket360_id,ticket360_numero,assunto,criado_em}]` (tabela `tickets_vinculos`) |
| `POST /vinculos` | `{ticket_id,ticket360_id,ticket360_numero,assunto}` → devolve o vínculo |
| `DELETE /vinculos/{id}` | 204 (exclusão lógica: `apagado=true`) |
| `GET /contatos?ticket_id=` | `[{id,ticket_id,canal,nota,created_at}]`, mais recente primeiro (tabela `tentativas_contato`) |
| `POST /contatos` | `{ticket_id,canal,nota}` → devolve o contato |
| `DELETE /contatos/{id}` | 204 (exclusão lógica) |
| `GET /clientes360/{cod_service}` | `{cod_service,nome,saldo_cents,faturas_json,contratos_json,synced_at}` (tabela `clientes_360`; os `*_json` vão como texto) ou 404 |

`PATCH /cards/{id}` já aceita `visita_data`, `visita_turno`, `visita_hora`, `visita_endereco` e `visita_realizada_em` (a aba Visita agenda e marca como realizada por ele).

Ficam para depois do login pelo 360: "Gerar renegociação" (puxar o total do 360 ao vivo), detalhe do ticket (descrição e última interação) e "Polir com IA".
