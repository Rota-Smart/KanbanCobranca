# Plano — RotaSuite Web + Nuvem + Integração 360

> Criado em 2026-11-08. Status: **Etapas 0–6 no código** (2026-11-09). Etapa 1 fecha quando o PC principal apertar ☁️ Enviar dados para a nuvem. Etapa 4 sincroniza o quadro em segundo plano; o pareamento na API ainda não existe (o app já chama o contrato). A Etapa 6 edita o quadro no site (criar, mover, observação, visita, excluir e ações em massa) pela sessão, sem atalho de PC. Acordo e tentativas de contato continuam pendentes: a API não tem a rota de escrita. A API publicada responde 503 `login ainda não configurado` (chave de sessão vazia), então o login real ainda não abre sessão. O site publicado continua na versão anterior (o login da Cloudflare expirou). O backup pelo GitHub continua até validar com dois PCs de verdade — depois desligamos.

## 1. Objetivo

Manter o **app desktop RotaSuite** fazendo tudo o que já faz hoje (inclusive os atalhos) e acrescentar um **site que substitui o uso diário do portal 360** para consulta, mais o kanban do próprio RotaSuite. O site não leva os atalhos do app.

1. **Banco na nuvem** — todos os PCs veem os mesmos dados, sincronizados. A visão pessoal da fila também mora aí, compartilhada entre o site e o app.
2. **Site RotaSuite** — celular e qualquer computador, com **login pelo próprio 360**.
   - Consultar a fila de tickets e a ficha do cliente como no portal.
   - Organizar **só a visão do RotaSuite** (colunas visíveis, filtros salvos, ordem da fila). Isso nunca é gravado de volta no 360.
   - Ver e editar o kanban: mover card, acordo (valores), observação, tentativa de contato e visita.
   - No celular, acompanhar o quadro e abrir o endereço da visita no mapa.
3. **App ligado à conta** — o desktop usa a mesma conta do site. Shift+F2, voz e controle do Chrome continuam só no PC.
4. **Integração 360 no site, somente leitura** — fila, busca de cliente, unidade, financeiro/OS. Mudança oficial de ticket (status, resposta, novo ticket, responsável) continua no 360, por link.
5. **Fidelidade de Ijuí** — na ficha do cliente, aba Financeiro → Contratos, a vigência é a data de assinatura mais o prazo em meses. O filtro lista os clientes da unidade pela data de término mais próxima.
6. **Evolution API (etapa futura)** — filtrar do jeito que for preciso, marcar quem recebe e disparar num botão, com confirmação manual. Não envia sozinho.

## 2. Situação atual (ponto de partida)

| Parte | Hoje |
|---|---|
| App | Wails (Go + React), `rotasuite.exe`, auto-update via repo release |
| Banco | SQLite local por PC; backup no repo privado `Rota-Smart/banco-de-dados` (não é sync real) |
| Acesso ao 360 | Cookies do perfil Chrome dedicado (`perfil-cdp`) via CDP |
| Endpoints 360 já usados (leitura) | `/api/helpdesk/tickets`, `/helpdesk/tickets/<uuid>`, `/api/cliente/buscar`, `/api/cliente/<cod>/dados`, `/api/cliente/<cod>/faturas`, `/api/cliente/<cod>/produtos` |
| Endpoints 360 de escrita | **Inferidos, nunca testados** — proibido testar em produção |
| Só funciona no PC | Shift+F2 (UIA lê a tela), voz whisper, controle do Chrome do 360 |

## 3. Arquitetura alvo

```
 Celular / outro PC                 PC com o app
 ┌──────────────┐                  ┌──────────────────────┐
 │ Site RotaSuite│                  │ rotasuite.exe        │
 │ (navegador)   │                  │ SQLite local (cache) │
 └──────┬───────┘                  └──────────┬───────────┘
        │ HTTPS (login 360)                  │ HTTPS (token do pareamento)
        ▼                                     ▼
 ┌─────────────────────────────────────────────────────────┐
 │  API RotaSuite (Go)  — hospedada na nuvem               │
 │  • autenticação (valida login no 360)                  │
 │  • cards, colunas, acordos, observações, chats         │
 │  • visão pessoal da fila (colunas, filtros, ordem)     │
 │  • proxy de leitura do 360 (tickets, clientes)         │
 └──────────────┬───────────────────────────┬──────────────┘
                ▼                           ▼
        Postgres (Supabase)          360.rotagroup.com.br
```

Princípios:
- **O app continua funcionando sem internet** (SQLite local vira cache + fila de envio).
- **Regra de conflito:** vale a última alteração (campo `atualizado_em` + `versao` por registro).
- **Senha do 360 nunca é guardada** — só o token/sessão devolvido pelo 360, criptografado.
- **O site nunca grava no 360.** Status, resposta, novo ticket e responsável só mudam no portal oficial. A organização da fila é visão do RotaSuite.
- **Atalhos do app não existem no site** e o site não pede para o PC executá-los.

## 4. Etapas

### Etapa 0 — Descobertas (só leitura, sem risco)
- [ ] Ver como funciona o login do 360 (formulário? token? cookie? existe OAuth?) observando a tela de login no Chrome do app.
- [ ] Verificar se tickets e clientes trazem **unidade** ou **cidade** (para o filtro de Ijuí).
- [ ] Verificar se a conta enxerga só os próprios tickets ou os da unidade toda.
- [ ] Medir tempo de vida da sessão do 360 (quanto tempo até vencer).
- **Entrega:** seção "Descobertas" preenchida neste documento.

### Etapa 1 — Banco na nuvem
- [x] Criar projeto Postgres (Supabase, plano grátis) com o mesmo esquema do SQLite (cards, colunas, acordos, vínculos, chats, chat_auditoria, config…).
- [x] Adicionar `usuario_id`, `atualizado_em`, `versao`, `apagado` (exclusão lógica) em todas as tabelas sincronizadas.
- [x] Ativar RLS (cada usuário só vê os próprios dados / os da unidade).
- [x] Script de migração (`Site/migracao`) **e botão no app v1.56.0** (Configurações → Nuvem → ☁️ Enviar dados para a nuvem). Falta: apertar no PC principal.
- **Entrega:** banco na nuvem com os dados de hoje.

### Etapa 2 — API RotaSuite (servidor Go)
- [x] API Go no repo `Rota-Smart/Site-Back` (pasta `Site/Backend`) — standalone, lib/pq reaproveitando os pacotes Go já existentes (`internal/rota360`, regras de renegociação, acordos…).
- [x] Endpoints REST: `/cards` (GET/POST/PATCH/DELETE), `/colunas`, `/acordos`, `/historico`, `/health`; token Bearer.
- [x] `GET /sync?desde=` e `POST /sync` (lote do app, regra "vale a última" — fechado na Etapa 4).
- [ ] Atualização em tempo real (WebSocket ou Supabase Realtime).
- [~] Render `Site-Back` com build verde; sobe quando o Secret File `secrets.json` for criado.
- **Entrega:** API no ar, testada com dados reais da nuvem.

### Etapa 3 — Login pelo 360
- [x] Sem OAuth: o site envia e-mail e senha, a API confere no 360 e guarda só a sessão devolvida, criptografada (AES-GCM). A senha não fica em lugar nenhum.
- [x] Sessão própria do RotaSuite: cookie `rotasuite_sessao`, httpOnly, Secure, SameSite=None (o site e a API são endereços diferentes), expira em até 20 horas.
- [x] Quando a sessão do 360 vence, a API responde 401 com "sessão do 360 expirou, entre de novo".
- **Entrega:** rotas no ar assim que o Secret File `CHAVE_SESSAO` existir na Render. O contrato está em `Site/Backend/API.md`.

### Etapa 4 — App conectado à conta (sincronização)
- [x] Tela "Conectar à conta" nas Configurações: código de pareamento exibido no site.
- [x] App guarda o token (Windows Credential Manager / keyring).
- [x] Motor de sync no app: envia alterações locais pendentes, recebe as remotas, aplica a regra "vale a última".
- [x] Funciona offline: fila local e reenvio quando a internet voltar.
- [~] Substitui o backup no GitHub — **o envio do repo `banco-de-dados` continua**. Desligamos só depois de ver dois PCs iguais de verdade.
- **Entrega:** dois PCs com o app mostrando o mesmo quadro. O motor está pronto e foi simulado com dois bancos nesta máquina. Falta o pareamento na API e o teste nos dois PCs físicos.

#### O que ficou pronto (2026-11-08)
- Configurações ganharam "Conectar à conta". A pessoa digita o código do site; o app troca por um token longo e guarda no Credential Manager do Windows. O token não vai para o SQLite nem para arquivo.
- Fila local (`sync_fila`) alimentada por gatilho: criar, mover, acordo, visita, tentativa, vínculo e comissão entram sozinhos. Sem internet a tela grava normal. Quando a rede volta, o app reenvia (a cada ~90s, e a cada 15s enquanto está sem rede). Erro de rede não aparece como erro — só o texto discreto "sincronizando" ou "aguardando internet".
- Conflito: vale a última alteração (`atualizado_em`, depois `versao`). Não pergunta nada. O caso em que a nuvem vence fica em `sync_historico`.
- `POST /sync` na API aplica o lote com essa regra. O gatilho da nuvem preserva o horário do app quando a transação pede (`app.preservar_carimbo`). `GET /sync` também devolve `alteracoes`, inclusive o que foi apagado.
- Cada PC reserva uma faixa de ids para não criar o mesmo número de card ao mesmo tempo.
- Testes automáticos: fila, exclusão em cascata, rede caída, conflito, card baixado, dois bancos locais, código 404 e código aceito (servidor de teste). Não houve escrita no portal 360 e não publicamos versão do app.

#### Pendências de backend
- `POST /auth/parear` **não existe**. O contrato está em `Site/Backend/API.md`. O app já chama. Enquanto responder 404, a tela diz que o site ainda não gera o código.
- O token longo do pareamento ainda não vale como `Authorization: Bearer`. Hoje `/sync` aceita o `API_TOKEN` único ou o cookie do site.
- Sem esses dois pontos a conexão real não fecha. O botão ☁️ Enviar dados para a nuvem continua até lá.
- Teste com dois computadores de verdade não rodou nesta máquina.

### Etapa 5 — Site: kanban no celular e no PC
- [x] Reaproveitar o frontend React atual (mesmo visual, glassmorphism, tema claro/escuro).
- [x] Trocar as chamadas Wails (`wailsjs`) por chamadas HTTP à API (camada de adaptação única).
- [x] Layout responsivo para celular (colunas em abas/rolagem horizontal), para acompanhar o quadro pelo telefone.
- [x] Card mostra os valores do acordo, observações e a visita (data, turno, hora, endereço). O endereço abre no mapa.
- [x] Bloquear indexação (`robots.txt` + `noindex`).
- **Entrega:** quadro, valores e visitas visíveis no celular, com o endereço abrindo no mapa.

#### O que ficou pronto (2026-11-09)
- O site lê a API real (`https://site-back-10cd.onrender.com`) só com o cookie da sessão (`credentials: 'include'`). Não há `API_TOKEN` no JavaScript. Se a sessão cai (401 nas rotas de dados), volta para o login com uma frase simples.
- `GET /auth/eu` existe. Sem cookie responde 401 `não autorizado`, que é o combinado — a etapa não parou por isso. A senha não foi testada daqui (não fica em arquivo). A prévia `/?previa` continua só com nomes fictícios.
- No celular as colunas viram abas (um toque, alvo de 44 px) e a janela do card ocupa a tela inteira. No computador as colunas ficam lado a lado, com rolagem horizontal e coluna recolhível. Conferido no navegador em tela de PC, iPhone (390×844) e Android pequeno (360×640): busca, abrir card, acordo, observação, visita e mapa.
- O card mostra o acordo salvo: entrada, parcela base, renegociação (Nx) e a soma (parcela base + renegociação). Observação em texto. Visita com data, turno e hora. O botão do mapa usa o endereço do cadastro (`endereco` do card). Se a visita tiver outro endereço, há um segundo botão.
- `noindex` segue na meta, no `robots.txt` e no `_headers` do Worker.

#### Pendências de backend
- O endereço **não falta**. `GET /cards` já devolve `endereco` e `visita_endereco`. Na nuvem, os 34 cards de teste têm endereço de cadastro; 1 tem endereço de visita. O corpo HTTP não foi lido daqui porque isso exige o cookie do login.
- `POST /acordos` não existe (só `GET /acordos`). Salvar acordo pelo site responde que ainda não está no servidor. A Etapa 6 precisa desta rota.
- Não existem `/vinculos` nem `/contatos`. As abas Ticket e Contatos já avisam.
- O front pede `GET /clientes360/<código>` (saldo, faturas, contratos em cache) e o servidor responde 404. O que existe é `GET /360/clientes?busca=` (busca ao vivo no 360, outro formato). Sem a rota por código, a aba Financeiro não enche.
- Publicar: o Worker `site-front` existe, mas o `wrangler` desta máquina não está logado (`Not logged in. Your auth token has expired and could not be refreshed`). O MCP da Cloudflare daqui não liga o Builds ao GitHub. O endereço https://360.foundrycode.com.br continua na versão anterior até alguém rodar, na pasta `Site/FrontEnd`:

```
npx wrangler login
npm run build
npx wrangler deploy
```

Build local: `npm run build`, saída `dist`, comando de deploy `npx wrangler deploy` (já está no `wrangler.jsonc`).

### Etapa 6 — Site: editar o kanban (sem atalhos)
- [x] Mover card, criar card, observação, tentativas de contato, acordos (valores), marcar visita, ações em massa. São dados do RotaSuite, não do 360. Acordo e tentativas chamam a API e, quando a rota não existe, a tela avisa em frase simples — o front não calcula a soma nem grava tentativa só na memória fingindo a nuvem.
- [x] Não colocar no site o que depende do PC: Shift+F2, voz, controle do Chrome e qualquer atalho de automação. Sem botão que peça para o app executar isso.
- **Entrega:** uso do quadro pelo site, no mesmo nível do app, sem as automações.

#### O que ficou pronto (2026-11-09)
- Toda escrita passa por `api.ts` com `credentials: 'include'`. Não há `API_TOKEN` no JavaScript. Sem cookie, POST/PATCH/DELETE de `/cards`, `/colunas`, `/acordos` e `/historico` respondem 401 `não autorizado` — a mesma porta da sessão, não um caminho só de Bearer. Um cookie no formato da sessão (e o próprio `POST /auth/login`) responde 503 `login ainda não configurado`: no código isso acontece quando `CHAVE_SESSAO` está vazia, antes de procurar a sessão. A escrita não foi recusada em favor do token; a sessão é que ainda não pode ser criada na API publicada.
- No PC o card se arrasta entre colunas. No celular (aba, uma coluna por vez) o card tem o menu "Mover para…", sem arrastar no toque. O menu abre por cima da página (o card com efeito de hover não prende o painel).
- Botão "+ Novo card": cliente, código service e coluna inicial. A frase da tela deixa claro que isso cria card no quadro do RotaSuite e não abre ticket no 360.
- Observação salva com PATCH e mostra "Salvo". Visita salva data, turno, hora e endereço, e o endereço abre no mapa.
- Seleção por checkbox e barra com mover, observação e excluir, com confirmação. Excluir um card também pede confirmação. Erro da API vira uma frase simples.
- Mover para "Visitas Realizadas" ou "Ticket Fechado" não marca visita realizada no front: o PATCH do card na API não faz isso sozinho.
- Conferido na prévia `/?previa` (memória, sem escrever no 360): criar card, arrastar no PC, menu no celular (390 px), observação, visita, tentativa listada com data na prévia, confirmação de exclusão (cancelar), mover 2 cards com confirmação. A tela de login sem prévia continua com `noindex`. Login real não foi exercido (senha não fica em arquivo).

#### Pendências de backend
- `POST /acordos` não existe (só `GET /acordos`). A aba Acordo deixa os campos visíveis e diz: "O servidor ainda não grava acordo." A soma e a renegociação só aparecem quando a API devolver o acordo calculado. O front não recalcula.
- Não existe rota de tentativas de contato (`/contatos` ou `/tentativas`). `POST /sync` grava `tentativas_contato` só no lote do app, e o site não usa isso. A aba diz: "O servidor ainda não grava tentativas de contato. Nada foi salvo." `GET /cards` não traz as datas, então o telefone do card não lista tentativas até existir a rota.
- Não há POST/PATCH/DELETE em `/historico`. Mover o card não grava histórico no tratador da API.
- Ao mudar a coluna para "Visitas Realizadas" ou "Ticket Fechado", a API não preenche `visita_realizada_em`. Fica pendente no servidor.
- `GET /clientes360/<código>` continua 404 (Etapa 5). O que existe é `GET /360/clientes?busca=`.
- `POST /auth/parear` continua ausente (Etapa 4).
- A API publicada não abre sessão enquanto `CHAVE_SESSAO` estiver vazia (503). Sem isso a Etapa 7 também não autentica.
- Publicar: `npx wrangler whoami` nesta máquina devolve `Not logged in. Your auth token has expired and could not be refreshed`. https://360.foundrycode.com.br segue na versão anterior. Na pasta `Site/FrontEnd`, quando alguém estiver no terminal interativo:

```
npx wrangler login
npm run build
npx wrangler deploy
```

### Etapa 7 — Integração 360 no site (somente leitura + visão pessoal)
- [x] Tela **Tickets**: fila aberta da conta, busca por texto, filtros (status, data, solicitante, unidade/cidade). Leitura do `/api/helpdesk/tickets` (e da pesquisa avançada quando o filtro é data, solicitante ou status do ticket).
- [x] **Visão pessoal**, salva no RotaSuite: colunas visíveis, filtros salvos e ordem da fila. Tabelas `visao_fila` e `visao_filtros` no Supabase, com `usuario_id` e RLS. Nenhuma dessas ações chama endpoint de escrita do 360. O app desktop ainda não lê essas tabelas.
- [x] Tela **Clientes**: busca por nome/código, filtro por unidade (Ijuí), ficha com dados, equipamentos, OS e faturas.
- [x] **Fidelidade de Ijuí:** listar a unidade e ler `GET /api/cliente/<cod>/contratos` de cada um. Término = `DataAssinatura` convertida para Brasília antes de somar `Prazo` meses. Ordenar pelos mais próximos. `TérminoContrato` nulo é ignorado. Cache de 15 minutos.
- [x] Botão "Criar card" gera um card no kanban do RotaSuite a partir de um ticket/cliente. Não abre nem altera o ticket no 360.
- [x] Botão "Abrir no 360" (link direto) para qualquer mudança oficial.
- [x] Cache curto (alguns minutos) para não sobrecarregar o 360.
- **Entrega:** consultar e organizar a própria visão da fila e do cliente sem gravar no 360.

#### O que ficou pronto (2026-11-09)
- Rotas novas de leitura na API: `GET /360/tickets` (filtros e 25 por página), `GET /360/ticket/<uuid>`, `GET /360/clientes?busca=`, `GET /360/cliente/<cod>` (dados, faturas, produtos, contratos e OS abertas) e `GET /360/fidelidade`. Visão pessoal: `GET/POST /visao`, `POST /visao/filtros`, `DELETE /visao/filtros/<id>`. Sem cookie, essas rotas respondem 401. POST em `/360/tickets` não existe (404).
- O site não fala com o 360. "Abrir no 360" é um link: ticket em `/helpdesk/tickets/<uuid>`, cliente em `/clientes?cod=<cod>&aba=cadastro`.
- Telas no mesmo site (sem página HTML nova; `noindex` continua no `index.html`, `robots.txt` e `_headers`). No celular a fila vira cartões; no PC, tabela.
- Conferido na prévia `/?previa`: fila, busca vazia, detalhe, criar card (aparece no quadro ao voltar), ficha, fidelidade ordenada (exemplo 22/02/2026 + 24 meses = 22/02/2028, faltam 15 meses em 09/11/2026), erro com `?falha=`, e o quadro da Etapa 6. Sem sessão, `/` mostra o login. Login real não foi exercido: a API publicada responde 503 `login ainda não configurado` enquanto o Secret File `CHAVE_SESSAO` estiver vazio.
- O site publicado não foi atualizado. `npx wrangler whoami` devolveu token expirado (`Not logged in`). Quando alguém estiver no terminal interativo, na pasta `Site/FrontEnd`: `npx wrangler login`, `npm run build`, `npx wrangler deploy`.

### Fora de escopo
- Fila de pedidos do site para o PC executar atalhos do app.
- Qualquer escrita no 360 (responder, mudar status, criar ticket, trocar responsável). Link "Abrir no 360" cobre esse caso.

### Etapa 8 — Evolution API (futura, depois do site utilizável)
- [ ] A partir de uma lista já filtrada (fila, clientes ou vigência de contrato perto de vencer), marcar quem recebe.
- [ ] Um botão dispara a mensagem pela Evolution API (`Site/Whatsapp`), só depois de confirmação manual na tela.
- [ ] Sem envio sozinho e sem agendamento que dispare sem alguém confirmar.
- **Entrega:** disparo semi-automático, com a pessoa escolhendo os destinatários e confirmando.

## 5. Assistente (IA) na web
- Fase 1: assistente continua só no app.
- Fase 2: assistente no site rodando no servidor (mesmo código Go do agente Groq), com as mesmas regras de economia de tokens (pré-busca, modo econômico). Também não executa atalho do PC e não grava no 360.

## 6. Riscos

| Risco | Mitigação |
|---|---|
| 360 não tem API oficial; mudanças no site quebram telas | Isolar chamadas em `internal/rota360`, erros claros, ajuste rápido |
| Tentação de "completar" o portal gravando no 360 | Decisão de produto: leitura e visão pessoal apenas; escrita fica no link "Abrir no 360" |
| Login do 360 não permitir validação externa | Plano B: app pareado envia a sessão do Chrome (cookie) para a API |
| Sessão do 360 vencendo | Aviso claro + relogin |
| Conflito de edição simultânea | "Vale a última" + histórico de alterações |
| Dados de clientes na nuvem (LGPD) | RLS, HTTPS, criptografia de tokens, só usuários da Rota |
| Kaspersky/antivírus com o app conectando à nuvem | Manter nome RotaSuite; avaliar assinatura de código (Certum) |

## 7. Custos estimados
- Supabase: grátis até 500 MB / pago ~US$ 25/mês se crescer.
- Hospedagem da API: grátis (Render free, dorme quando parado) ou ~US$ 7/mês sempre ligado.
- Domínio (opcional): ~R$ 40/ano.

## 8. Ordem recomendada
0 → 1 → 2 → 4 (sync entre PCs já entrega valor) → 3 → 5 → 7 → 6. A fidelidade de Ijuí faz parte da Etapa 7. A Etapa 8 (Evolution) só depois do site utilizável. Escrita no 360 não entra na fila.

## 9. Descobertas

_Mapeamento feito em 2026-11-08, logado como Luis Henrique (Operação, Comercial Ijuí), navegador do ZCode. Rota 360 v1.127.0._

### Login e sessão
- **Login = e-mail + senha, sem OAuth/SSO** (`/login`). Campos: E-mail, Senha, checkbox "Mantenha-me conectado", link "Esqueci minha senha".
- **Contrato visto no código do site (v1.127.0), em 2026-11-08.** O JavaScript manda `POST https://360.rotagroup.com.br/api/auth/login` com JSON `{"email","password","lembrar"}`. A caixa "Mantenha-me conectado" começa desmarcada (`lembrar: false`). Não há campo oculto nem CSRF. O cliente usa cookie (`withCredentials`). Se a senha não confere, o JSON traz `error`.
- **Confirmar a sessão:** `GET https://360.rotagroup.com.br/api/auth/me`. Devolve a pessoa (e-mail, nome, e a unidade em `unidade` ou `unidades_codigos`). Sem sessão, responde 401 `{"error":"Token não fornecido"}`.
- **Sessão é cookie httpOnly** — nada de token em localStorage/sessionStorage nem cookie visível ao JS. O nome do cookie só aparece no `Set-Cookie`, que o JavaScript não consegue ler. A API anota o nome (nunca o valor) no log do primeiro login que der certo.
- **Perfil → Segurança mostra "Histórico de acessos"** (últimos 5 logins: IP, dispositivo, data) e botão "Encerrar todas as sessões" (desabilitado). O histórico mostra logins diários (celular + PC) → sessão parece curta; confirmar tempo exato de vida com teste posterior.
- Alterar senha pelo próprio perfil (senha atual + nova, mín. 8).
- **Leitura usada pelo proxy:** `GET /api/helpdesk/tickets` e `GET /api/cliente/buscar?q=`.

### Escopo da conta (responde Etapa 0)
- **Conta presa à unidade Ijuí**: no Painel SLA (`/tickets`) o filtro "Filtrar por unidade" vem **desabilitado com "Ijuí" selecionado**. Nas outras telas (fila, relatórios) o filtro de unidade existe com todas as opções, mas a fila "Em aberto" só mostra tickets da Comercial Ijuí.
- **Vê tickets da unidade inteira, não só os próprios**: "Meus em aberto" = 38 = "Em aberto" (todos são dele por ser o único responsável da unidade); o Painel SLA tem "Performance por Setor" e lista de consultores/vendedores da unidade.
- **Unidade/cidade existem como campo próprio** de ticket e cliente → **filtro de Ijuí é viável** (coluna "Unidade" na tabela da fila, valores: Corporativo, Cruz Alta, Ijuí, Rastreamento, Santo Ângelo, São Borja, São Luiz Gonzaga, Órgão Público).

### Telas do "Meu Trabalho" (o site deve replicar a leitura destas)

**1. Central de Tickets / Fila de Tickets (`/helpdesk`)** — a tela principal.
- Views prontas: Novos, Em aberto, Pendentes, Meus em aberto, Em aberto nos meus grupos, Mencionados + listas personalizadas (ex.: "Comercial CA"). Paginação (25/pág).
- Tabela: Nº, Título (+ código service e nome do cliente embaixo), Status, Setor, Responsável, **Unidade**, Categoria, SLA 1ª resposta, SLA resolução (prazo ou "Atrasado Xd"), SLA aplicado, Anexos, Criado em. Colunas configuráveis ("Colunas (10)").
- Filtros: busca por texto, Setor (31 opções), **Unidade**, Responsável, Criador, Categoria (14), Canal de origem (Manual, Fila de Cobrança, E-mail, Telefone, Migração Octadesk, etc.), Situação do SLA (Atrasado/Em atenção/No prazo), SLA aplicado, "Somente sem responsável".
- Subtelas: Painel (`/helpdesk/painel`), Relatórios, Pesquisa avançada, **Configuração** (`/helpdesk/configuracao` — só para quem pode), Novo ticket. Abas de tickets abertos ficam em localStorage (sobrevivem ao reload).
- Endpoint conhecido pelo app: `/api/helpdesk/tickets` — a fila é esse endpoint com filtros.

**2. Painel SLA Octadesk (`/tickets`)**
- Métricas: Total, Em aberto, parados +7 dias, % Dentro do Prazo (meta 90% da Configuração do Helpdesk), Tickets Críticos (SLA >100%).
- Semáforo de SLA (Saudáveis <80%, Alerta 80–100%, Críticos >100%), Performance por Setor (clicável), Top Tickets Críticos, busca de tickets (número, cliente, unidade, situação, período, consultores).
- Filtro de unidade **travado em Ijuí** (escopo da conta).
- Regra de horas: monitoramento 24/7, demais em horas úteis (08–12h / 13:30–18h).

**3. Novo Ticket (`/helpdesk/novo`)**
- Título*, descrição rica, anexos (máx. 5 × 5 MB: imagens, PDF, Office, áudio), colar print.
- Lateral: Formulário (7 tipos: Atendimento Padrão, Débito em Conta, Informação de LEAD, Programar Bonificação, Solicitação de Agendamento, Solicitação de Rondas, Troca de Titularidade), Solicitante* (busca por Nome/Unidade/ID Service/Endereço/Chave Sowil), Código Service*, Contato* (depende do código service), Unidade*, Categoria*, Assunto* (depende da categoria), Responsável (opcional, "Sou eu", automação se vazio), Tags.
- Botão "Importar conversa do Octadesk". Modelos de abertura (Afazeres de Viagem, Informação de LEAD). Rascunho salvo em localStorage.

**4. Pesquisa avançada (`/helpdesk/pesquisa`)**
- **Filtra TODOS os tickets já criados (abertos e fechados)** — critérios combináveis: Número, Título, Texto (título ou thread), Solicitante/Organização, Código Service, CodCliente, Unidade, Categoria, Assunto, Grupo/Setor, Responsável, Criador, Status, Tag, Data de abertura, Data de resolução. Operador "contém". Exporta resultado (XLSX/CSV). Critérios persistem entre buscas (opcional).

**5. Relatórios (`/helpdesk/relatorios`)**
- Recorte único (Fila de Cobrança only, período, Setor, Unidade, Categoria, Assunto, Canal, SLA) + 5 relatórios em XLSX/CSV: Produtividade por agente, Cumprimento de SLA, Volume por categoria, Abertos/fechados por assunto, Lista de tickets (exportação bruta).

**6. Consulta Clientes (`/clientes`)**
- Busca por Todos os campos/Cód. Cliente/CPF-CNPJ/Unidade/Endereço/Chave Sowil/Telefone; checkboxes "Mostrar inativos" e "Mostrar cortesia".
- Ficha do cliente (abre ao clicar no resultado) — abas: Visão Geral, Cadastro Geral, Atendimento, Financeiro, Monitoramento (+ IA "Perguntar a Lena"):
  - Financeiro: saldo devedor, faturas em aberto (doc, valor, vencimento, status)
  - OS ativas, SAC pendente, Segurança Sowil (armado/desarmado, alarmes, deslocamentos)
  - Atendimento Octadesk: contadores abertos/pendentes/resolvidos + tickets recentes
  - Score de Saúde, Produtos (ativos com valores), Orçamentos, Comunicações (histórico), Tags
- Endpoints já usados pelo app cobrem boa parte: `/api/cliente/buscar`, `/api/cliente/<cod>/dados`, `/faturas`, `/produtos`.
- **Contrato / fidelidade** (visto em 2026-11-08 na ficha de GILBERTO LUIZ SPERONI, `#26618`, cidade IJUI): aba **Financeiro → Contratos**. O cartão "Serviço de Monitoramento" mostra Assinatura `22/02/2026`, Mensalidade `R$ 205,00` dia 10 e **Prazo: 24 meses**. A vigência começa na assinatura; a tela não mostra a data final.
- Endpoint: `GET /api/cliente/<cod>/contratos`. No exemplo: `DataAssinatura` `2026-02-23T00:00:00.000Z` (meia-noite UTC, que em Brasília é `22/02/2026`), `Prazo` `24`, `DiaVcto` `10`, `ValorNF`/`ValorME` `205`, `Situacao` `Ativo`, `TérminoContrato` `null`. Converter a assinatura para `America/Sao_Paulo` antes de somar os meses; senão o dia vira 23. Término deste caso: `22/02/2028`.

**7. Notificações (`/notificacoes`)**
- Central completa: estados (Todas/Não lidas/Lidas/Arquivadas), busca, filtro por Módulo (Tickets, Prospecção, Caixa de Sugestões, Chamados GLPI), período, ações em lote, "marcar todas como lidas". Sino no header mostra não lidas (99+).

**8. Sugestões (`/sugestoes`)**
- Caixa de ideias: Nova sugestão, lista com nº (#110), status (Implementada etc.), categoria, votos. Pouco relevante para o site (menção só por completude).

### Fora do escopo pedido (existe no 360, não mapeado a fundo)
- Sucesso do Cliente: Painel de Cancelamentos; Operações: Ordens de Serviço (`/os`); Comercial; Financeiro: Recebimentos e Inadimplência (`/financeiro`); Melhoria e Estratégia; Chat Interno; Chamados internos; Planos de Ação (`/planos-acao`); Prospecção (`/prospeccao`); IA "Lena" integrada na ficha do cliente.

### Conclusões para o plano
1. **Login do site**: sem OAuth — o site RotaSuite valida usuário/senha direto no 360 (POST de login) e guarda a sessão devolvida; ou recebe a sessão do app pareado (Plano B). Não guardar senha.
2. **Filtro Ijuí: viável e já é o escopo natural da conta** (conta vem presa a Ijuí no painel).
3. **Leitura de tickets completa é possível** (Pesquisa avançada cobre todos, inclusive fechados, com exportação).
4. **Ficha do cliente no site** pode replicar a Visão Geral com os endpoints já conhecidos + complementos a descobrir (Sowil, Score de Saúde, orçamentos, comunicações).
5. **Tempo de vida da sessão**: ainda a medir (histórico sugere login diário). Teste em momento oportuno: logar, esperar e ver quando pede de novo.
6. **Escrita no 360 está fora por decisão de produto** (2026-11-08), não só por falta de endpoint confirmado. O site organiza a visão no RotaSuite e edita o kanban local; o portal oficial segue sendo o lugar de alterar ticket.
7. **Fidelidade = vigência do contrato** na aba Financeiro → Contratos. Término = data de assinatura (fuso de Brasília) + prazo em meses. O filtro de Ijuí ordena por esse término. A lista da unidade inteira ainda depende de enumerar os clientes (a busca já existe) e ler `/contratos` de cada um; não há, nesta descoberta, um endpoint único que já devolva a unidade ordenada por vencimento.
