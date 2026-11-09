# Prompt — Etapa 8 (Evolution API: disparo semi-automático de WhatsApp)

Você vai implementar a **Etapa 8 do plano** `C:\Rota Smart\docs\plano-rotasuite-web-360.md`. Leia o plano inteiro antes de começar (seções 1 e 4 importam; a Etapa 8 vem **depois do site utilizável**, ou seja, só comece se as Etapas 5, 6 e 7 estiverem prontas — se não estiverem, pare e avise o usuário).

## Contexto

- **RotaSuite** é um app desktop Wails (Go + React) de Kanban de cobrança, com site web (login pelo 360) e API Go na Render (`https://site-back-10cd.onrender.com`, repo `Rota-Smart/Site-Back` em `C:\Rota Smart\Site\Backend`). O banco na nuvem é o Supabase.
- O usuário é leigo em termos técnicos. Explique as coisas em frases simples, sem jargão.
- **Objetivo desta etapa:** a partir de uma lista já filtrada (fila de tickets, busca de clientes ou vigência de contrato da fidelidade de Ijuí perto de vencer), o usuário **marca quem recebe**, aperta **um botão** e a mensagem sai pela **Evolution API** (WhatsApp), **sempre depois de confirmação manual na tela**.
- A pasta `Site/Whatsapp` é o lugar previsto para isso (ver plano).

## Regras obrigatórias (o coração desta etapa)

1. **Nada é enviado sozinho. Sem agendamento que dispare sem alguém confirmar.** Não existe cron, job de fundo, disparo automático por vencimento ou qualquer caminho que envie mensagem sem um clique humano de confirmação na tela, naquela sessão.
2. **Confirmação dupla:** tela de revisão mostra destinatário, número e o texto final de cada mensagem antes de sair; o botão de disparo só existe nessa tela.
3. **A lista de partida é sempre um filtro que o usuário escolheu** (fila, clientes, contratos perto de vencer) — nunca "todo mundo". Marcação individual (checkbox) por destinatário.
4. **Credenciais da Evolution API ficam só no backend** (Secret File/variável de ambiente na Render). Nada de URL/chave da Evolution no front.
5. **Só use os MCPs com `rota` no nome** (`rota-supabase-site`, `rota-render-backend` etc.).
6. **Nenhuma escrita no 360** nesta etapa. O disparo é WhatsApp, não ticket.
7. Logar cada disparo (destinatário, texto, resultado, data, quem confirmou) — para auditoria, igual ao espírito do `chat_auditoria` do app.

## O que fazer

1. **Descobrir/definir a integração Evolution API:** instância própria (URL + chave + instância, fornecidas pelo usuário), endpoint de envio de texto, e conexão do número (QR code) — documentar em `Site/Whatsapp/README.md` num jeito que o usuário entenda.
2. **Backend:** rota na API (ex.: `/whatsapp/enviar`, lote) que recebe a lista confirmada de destinatários + textos e dispara na Evolution, um a um, com tratamento de erro por destinatário (falha de um não para os demais). Rota também para `GET /whatsapp/status` (conectado ou não).
3. **Front (site):** nas telas de lista já existentes (fila da Etapa 7, clientes, fidelidade de Ijuí), botão "Enviar WhatsApp" que abre a tela de revisão: lista com checkbox, número do destinatário (buscar telefone do cadastro do cliente; se faltar, mostrar e deixar o usuário decidir), texto editável por destinatário (com modelo padrão), botão "Confirmar e enviar" + barra de progresso + resultado final (enviados / falhados, com motivo).
4. **Modelos de mensagem:** modelos simples editáveis com variáveis (nome, mês, valor) — mas o texto final confirmado é sempre o que sai.
5. **Limites de bom senso:** pausa entre envios (ex.: 2–5 s por mensagem) para não tomar bloqueio do WhatsApp; aviso claro se a lista for grande (ex.: >50).
6. **Testar:** com número de teste do próprio usuário primeiro. Nunca disparar para clientes reais sem o usuário pedir e confirmar na tela.
7. **Atualizar o plano:** marcar as caixinhas da Etapa 8 e escrever resumo do que ficou pronto.

## Entrega

- Do filtro à mensagem: usuário filtra, marca, revisa, confirma e envia — com log de tudo.
- Zero envio sem clique humano de confirmação; zero credencial da Evolution no front.
- Documentado em `Site/Whatsapp/README.md` (como conectar o número, como usar).
