# Prompt — Etapa 5 (site: kanban no celular e no PC)

Você vai implementar a **Etapa 5 do plano** `C:\Rota Smart\docs\plano-rotasuite-web-360.md`. Leia o plano inteiro antes de começar (as seções 1, 3 e 9 importam para o contexto; a 9 traz o que já se sabe do 360).

## Contexto

- **RotaSuite** é um app desktop Wails (Go + React) de Kanban de cobrança. O plano o leva para a web: banco na nuvem (Supabase), API Go na Render e um site para usar o quadro de qualquer aparelho, com login pelo próprio 360.
- O usuário é leigo em termos técnicos. Explique as coisas em frases simples, sem jargão.
- A ordem recomendada do plano é 0 → 1 → 2 → 4 → 3 → 5 → 7 → 6. **Esta etapa pressupõe a Etapa 3 (login) já feita na API.** Se `/auth/eu` ainda não existir ou devolver erro, pare e avise o usuário — sem login o site não pode mostrar dados reais (LGPD).

## O que já existe

- **API:** repo privado `Rota-Smart/Site-Back`, clonado em `C:\Rota Smart\Site\Backend`, publicada em `https://site-back-10cd.onrender.com`. Rotas de dados (`/cards`, `/colunas`, `/acordos`, `/historico`, `GET /sync`) hoje com `Authorization: Bearer <API_TOKEN>`; após a Etapa 3, aceitam também a sessão RotaSuite (cookie `rotasuite_sessao`). O contrato das rotas de auth está em `Site/Backend/API.md`. **Você não mexe no backend** — se faltar algo, anote e negocie com o agente da API.
- **Site (front):** repo `Rota-Smart/Site-Front`, clonado em `C:\Rota Smart\Site\FrontEnd`. Vite + React 18 + Tailwind 4, visual azul-escuro glass (igual ao do app). Já tem: tela de login (e-mail/senha do 360), quadro read-only (rail lateral no PC, barra inferior no celular, Resumo/Compacto, busca, colunas recolhíveis), janela do card (Cliente, observação, acordos, histórico, mover coluna via PATCH), **prévia com dados fictícios em `/?previa`**, noindex (meta + robots + `_headers`). Deploy na Cloudflare em `https://360.foundrycode.com.br` (Worker `site-front`; conferir se Builds já está ligado ao GitHub — na última checagem não estava e o push não publicou).
- **Pegadinha Tailwind v4:** classes próprias (`.campo` etc.) precisam ficar dentro de `@layer components`, senão sobrescrevem utilitários (ex.: `pl-8` ignorado).
- **Dados:** hoje na nuvem há dados de teste vindos do notebook (34 cards). O PC principal ainda vai apertar ☁️ Enviar dados para a nuvem. O visual tem que funcionar com o que houver.

## Regras obrigatórias

1. **Nunca coloque o `API_TOKEN` no front.** O site usa só a sessão do login (cookie, `credentials: 'include'`). Qualquer descoberta de token no JS é bug grave.
2. **Só leitura nesta etapa.** A edição do kanban é a Etapa 6 — não antecipe botões de mover/criar além do que já existe.
3. **Só use os MCPs com `rota` no nome** (`rota-supabase-site`, `rota-render-backend` etc.). Os genéricos (`supabase`, `fcode-*`) apontam para outros projetos.
4. **O site nunca fala com o 360 direto.** Toda leitura do 360 passa pela API (`/360/tickets`, `/360/clientes`, da Etapa 3).
5. Não toque no app desktop (`C:\Rota Smart`, branch `release`) nem no repo `Site-Back`.
6. `noindex` tem que continuar valendo em qualquer página nova (meta + robots + `_headers`).

## O que fazer

1. **Rodar o front local e ligá-lo à API real** (login com a conta real do usuário, senha digitada por ele, nunca salva em arquivo) em vez de usar só a prévia fictícia.
2. **Camada única de API:** garantir que toda chamada passa por um módulo único (ex.: `api.ts`) que já embute `credentials: 'include'`, trata 401 como "sessão terminou → volta ao login" e mostra erros em frases simples.
3. **Responsivo de verdade no celular:** testar no DevTools em iPhone/Android pequeno; colunas em rolagem horizontal ou abas; janela do card em tela cheia; alvos de toque grandes.
4. **Card completo:** valores do acordo (entrada, parcela base, renegociação, soma), observações e visita (data, turno, hora, **endereço com botão que abre no mapa** — Google Maps link com o endereço do cadastro).
5. **Buscar/endereços:** conferir que o endereço do cliente chega pela API (se faltar campo, anotar como pendência do backend, não inventar).
6. **Publicar:** conectar o Builds da Cloudflare ao GitHub (repo `Site-Front`, build `npm run build`, deploy `npx wrangler deploy`) ou pedir ao usuário para conectar/rodar `npx wrangler login`. Conferir o site publicado de ponta a ponta (login → quadro → card → mapa).
7. **Atualizar o plano:** marcar as caixinhas da Etapa 5 e escrever um resumo curto do que ficou pronto e das pendências de backend (se houver).

## Entrega

- Quadro do RotaSuite visível no celular e no PC com dados reais da nuvem, depois do login pelo 360.
- Card mostra acordo, observação e visita, com endereço abrindo no mapa.
- Publicado em `https://360.foundrycode.com.br`, noindex, sem token no código.
