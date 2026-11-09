# Prompt — Etapa 6 (site: editar o kanban, sem atalhos)

Você vai implementar a **Etapa 6 do plano** `C:\Rota Smart\docs\plano-rotasuite-web-360.md`. Leia o plano inteiro antes de começar (seções 1, 3 e o item "Fora de escopo" da seção 4 são obrigatórios).

## Contexto

- **RotaSuite** é um app desktop Wails (Go + React) de Kanban de cobrança. O plano o leva para a web: banco na nuvem (Supabase), API Go na Render e um site para usar o quadro de qualquer aparelho, com login pelo próprio 360.
- O usuário é leigo em termos técnicos. Explique as coisas em frases simples, sem jargão.
- **Esta etapa pressupõe as Etapas 3 (login), 4 (sync) e 5 (site kanban read-only) prontas.** Se o login não funcionar no site publicado ou os endpoints de escrita da API não aceitarem a sessão RotaSuite, pare e avise o usuário.
- A API já expõe escrita: `/cards` (POST/PATCH/DELETE), `/colunas`, `/acordos`, `/historico`. O contrato está em `Site/Backend/API.md`. **Você não mexe no backend** — se faltar algo (ex.: tentativas de contato, visita), anote e negocie com o agente da API.

## O que já existe

- **Site (front):** repo `Rota-Smart/Site-Front`, clonado em `C:\Rota Smart\Site\FrontEnd`. Vite + React 18 + Tailwind 4, visual azul-escuro glass (igual ao do app). Já tem: login, quadro read-only responsivo (rail lateral no PC, barra inferior no celular), janela do card com mover coluna via PATCH, prévia fictícia em `/?previa`, noindex. Deploy na Cloudflare em `https://360.foundrycode.com.br` (Worker `site-front`).
- **App desktop (referência de comportamento, não de código):** mover card (drag no quadro, inclusive comandos de chat), criar card, observação, tentativas de contato (badge 📞 com datas), acordos (entrada, parcela base, renegociação, soma — regras de renegociação em Go), visitas (data, turno, hora), ações em massa (seleção múltipla + barra mover/observação/excluir).
- **Pegadinha Tailwind v4:** classes próprias (`.campo` etc.) precisam ficar dentro de `@layer components`.

## Regras obrigatórias

1. **Nunca coloque o `API_TOKEN` no front.** Toda escrita usa a sessão do login (cookie, `credentials: 'include'`).
2. **Nada do que depende do PC entra no site:** sem Shift+F2, sem voz, sem controle do Chrome, sem atalho de automação, **e sem nenhum botão que peça ao app para executar algo** ("fila de pedidos ao PC" está fora de escopo). Se um recurso do app precisar do PC, ele não entra.
3. **Nenhuma escrita no 360.** Os dados que você edita aqui são só do RotaSuite (cards, colunas, acordos, observações, tentativas, visitas). Status/resposta de ticket continuam só no 360.
4. **Regra de conflito: vale a última** — a API cuida disso; o front só precisa tratar erros de forma clara.
5. **Só use os MCPs com `rota` no nome** (`rota-supabase-site`, `rota-render-backend` etc.).
6. `noindex` tem que continuar valendo em qualquer página nova.
7. Ao mover um card para "Visitas Realizadas" ou "Ticket Fechado", replicar o comportamento do app (auto-marcação de visita realizada) só se a regra já estiver na API; senão anotar como pendência — não reimplementar regra de negócio no front.

## O que fazer

1. **Mover card** no quadro (drag-and-drop no PC; no celular, menu do card com "mover para…" ou tocar e escolher coluna — drag em touch é ruim, prefira menu).
2. **Criar card** (botão +, formulário simples: cliente, código service, coluna inicial).
3. **Observação** editável na janela do card (salvar via PATCH, com indicador de "salvo").
4. **Tentativas de contato:** registrar tentativa (botão 📞) e listar datas, igual ao app.
5. **Acordos:** editar valores (entrada, parcela renegociada) na aba Acordo; a soma/renegociação calculada deve vir da API (regras de renegociação já em Go no backend) — o front não recalcula regra de negócio por conta própria.
6. **Visitas:** marcar/agendar visita (data, turno, hora) e exibir com endereço abrindo no mapa.
7. **Ações em massa:** seleção múltipla (checkbox) + barra com mover/observação/excluir, igual ao app, adaptada para toque.
8. **Confirmações:** excluir card e ações em massa pedem confirmação; erros da API aparecem em frases simples.
9. **Publicar** na Cloudflare e testar de ponta a ponta no celular e no PC: mover, criar, observar, acordo, visita, ações em massa — e conferir que aparecem no app (ou no quadro de outro usuário logado).
10. **Atualizar o plano:** marcar as caixinhas da Etapa 6 e escrever resumo + pendências de backend.

## Entrega

- Uso completo do quadro pelo site (celular e PC), no mesmo nível do app, exceto as automações que dependem do PC.
- Nenhuma escrita no 360, nenhum token no código, sem fila de comandos ao PC.
- Publicado em `https://360.foundrycode.com.br`.
