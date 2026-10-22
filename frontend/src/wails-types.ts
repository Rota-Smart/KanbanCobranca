// Alias de tipos: o Wails gera as structs em namespaces (db.*, service.*),
// mas o app usa os nomes soltos. Mantém os imports do frontend estáveis.
import type { db, service, main, rota360, atualizacao } from '../wailsjs/go/models';

export type Ticket = db.Ticket;
export type Cobranca = db.Cobranca;
export type Proposta = service.Proposta;
export type ReparcelamentoInput = service.ReparcelamentoInput;
export type ReparcelamentoResult = service.ReparcelamentoResult;

// ---- Integração Rota 360: aliases dos tipos gerados pelo Wails ----

export type ResumoCliente360 = rota360.ResumoCliente;
export type Financeiro360 = rota360.Financeiro360;
export type Avaliacao = service.Avaliacao;
export type ResultadoCobranca = main.ResultadoCobranca;
export type ParametroNegociacao = db.ParametroNegociacao;
export type ConfigApp = main.ConfigApp;
export type Rota360Status = main.Rota360Status;
export type AcaoDoAgente = service.AcaoDoAgente;
export type ChatResultado = service.ChatResultado;
export type MensagemDeChat = service.MensagemDeChat;
export type InfoAtualizacao = atualizacao.Info;

// Sincronização GitHub
export interface SyncInfo {
  configurado: boolean;
  repo: string;
  temRemoto: boolean;
  remotoQuando: string;
  pendenteBaixar: boolean;
  localAlterado: boolean;
}
export interface ResultadoSync {
  acao: 'enviado' | 'recebido' | 'em-dia' | 'conflito' | 'configurar';
  mensagem: string;
}
