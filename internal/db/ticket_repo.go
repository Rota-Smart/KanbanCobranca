package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const ticketCols = `id, nome_cliente, tipo_ticket, numero_ticket, cod_service, endereco, telefone, coluna_atual, posicao, created_at, updated_at, ticket360_id, ticket360_numero, observacoes, visita_data, visita_turno, visita_hora, visita_endereco`

type TicketRepo struct {
	db *sql.DB
}

func NewTicketRepo(db *sql.DB) *TicketRepo { return &TicketRepo{db: db} }

var ErrTicketNaoEncontrado = errors.New("ticket não encontrado")

// Save insere (ID 0) ou atualiza um ticket. Card novo entra no fim da coluna.
func (r *TicketRepo) Save(ctx context.Context, t *Ticket) (int64, error) {
	if t.ID == 0 {
		res, err := r.db.ExecContext(ctx,
			`INSERT INTO tickets (nome_cliente, tipo_ticket, numero_ticket, cod_service, endereco, telefone, coluna_atual, posicao)
			 VALUES (?, ?, ?, ?, ?, ?, ?,
			   COALESCE((SELECT MAX(posicao) + 1 FROM tickets WHERE coluna_atual = ?), 0))`,
			t.NomeCliente, t.TipoTicket, t.NumeroTicket, t.CodService, t.Endereco, t.Telefone, t.ColunaAtual, t.ColunaAtual)
		if err != nil {
			return 0, fmt.Errorf("inserir ticket: %w", err)
		}
		return res.LastInsertId()
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE tickets SET nome_cliente=?, tipo_ticket=?, numero_ticket=?, cod_service=?, endereco=?, telefone=?, coluna_atual=?, posicao=?, updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now')
		 WHERE id=?`,
		t.NomeCliente, t.TipoTicket, t.NumeroTicket, t.CodService, t.Endereco, t.Telefone, t.ColunaAtual, t.Posicao, t.ID)
	if err != nil {
		return 0, fmt.Errorf("atualizar ticket: %w", err)
	}
	return t.ID, nil
}

// List devolve todos os tickets ordenados por coluna e posição.
func (r *TicketRepo) List(ctx context.Context) ([]Ticket, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+ticketCols+` FROM tickets ORDER BY coluna_atual, posicao, id`)
	if err != nil {
		return nil, fmt.Errorf("listar tickets: %w", err)
	}
	defer rows.Close()
	var out []Ticket
	for rows.Next() {
		var t Ticket
		if err := scanTicket(rows, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Get recupera um ticket pelo ID; devolve ErrTicketNaoEncontrado se não existir.
func (r *TicketRepo) Get(ctx context.Context, id int64) (*Ticket, error) {
	var t Ticket
	err := r.db.QueryRowContext(ctx,
		`SELECT `+ticketCols+` FROM tickets WHERE id = ?`, id,
	).Scan(&t.ID, &t.NomeCliente, &t.TipoTicket, &t.NumeroTicket, &t.CodService,
		&t.Endereco, &t.Telefone, &t.ColunaAtual, &t.Posicao, &t.CreatedAt, &t.UpdatedAt,
		&t.Ticket360ID, &t.Ticket360Numero, &t.Observacoes,
		&t.VisitaData, &t.VisitaTurno, &t.VisitaHora, &t.VisitaEndereco)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTicketNaoEncontrado
	}
	if err != nil {
		return nil, fmt.Errorf("buscar ticket %d: %w", id, err)
	}
	return &t, nil
}

// GetPorCodService devolve o card mais recente com esse código service
// do 360, ou nil se não houver.
func (r *TicketRepo) GetPorCodService(ctx context.Context, cod string) (*Ticket, error) {
	var t Ticket
	err := r.db.QueryRowContext(ctx,
		`SELECT `+ticketCols+` FROM tickets WHERE cod_service = ? ORDER BY id DESC LIMIT 1`, cod,
	).Scan(&t.ID, &t.NomeCliente, &t.TipoTicket, &t.NumeroTicket, &t.CodService,
		&t.Endereco, &t.Telefone, &t.ColunaAtual, &t.Posicao, &t.CreatedAt, &t.UpdatedAt,
		&t.Ticket360ID, &t.Ticket360Numero, &t.Observacoes,
		&t.VisitaData, &t.VisitaTurno, &t.VisitaHora, &t.VisitaEndereco)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("buscar ticket por cod_service: %w", err)
	}
	return &t, nil
}

// Move reordena o card em uma transação: abre espaço na coluna de destino,
// grava o card, fecha o buraco na coluna de origem e registra a movimentação.
// colunaOrigem é a coluna atual do card; posicao é o índice final na coluna de destino.
func (r *TicketRepo) Move(ctx context.Context, id int64, colunaOrigem, colunaDestino string, posicao int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("iniciar transação: %w", err)
	}
	defer tx.Rollback()

	var posicaoAntiga int
	if err := tx.QueryRowContext(ctx,
		`SELECT posicao FROM tickets WHERE id = ?`, id).Scan(&posicaoAntiga); err != nil {
		return fmt.Errorf("obter posição atual: %w", err)
	}

	if colunaOrigem == colunaDestino {
		// Fechar o buraco onde o card estava, depois abrir espaço no destino.
		if _, err := tx.ExecContext(ctx,
			`UPDATE tickets SET posicao = posicao - 1 WHERE coluna_atual = ? AND posicao > ? AND id != ?`,
			colunaOrigem, posicaoAntiga, id); err != nil {
			return fmt.Errorf("fechar espaço na coluna de origem: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE tickets SET posicao = posicao + 1 WHERE coluna_atual = ? AND posicao >= ? AND id != ?`,
			colunaDestino, posicao, id); err != nil {
			return fmt.Errorf("abrir espaço na coluna de destino: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx,
			`UPDATE tickets SET posicao = posicao + 1 WHERE coluna_atual = ? AND posicao >= ? AND id != ?`,
			colunaDestino, posicao, id); err != nil {
			return fmt.Errorf("abrir espaço na coluna de destino: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE tickets SET coluna_atual = ?, posicao = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id = ?`,
		colunaDestino, posicao, id); err != nil {
		return fmt.Errorf("mover ticket: %w", err)
	}

	if colunaOrigem != colunaDestino {
		if _, err := tx.ExecContext(ctx,
			`UPDATE tickets SET posicao = posicao - 1 WHERE coluna_atual = ? AND posicao > ? AND id != ?`,
			colunaOrigem, posicaoAntiga, id); err != nil {
			return fmt.Errorf("fechar espaço na coluna de origem: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO movimentacoes (ticket_id, de_coluna, para_coluna) VALUES (?, ?, ?)`,
		id, colunaOrigem, colunaDestino); err != nil {
		return fmt.Errorf("registrar movimentação: %w", err)
	}

	return tx.Commit()
}

// Delete apaga o ticket; cobranças e movimentações caem por ON DELETE CASCADE.
func (r *TicketRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM tickets WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("excluir ticket %d: %w", id, err)
	}
	return nil
}

// VincularTicket360 grava (ou limpa, com uuid e numero vazios) o vínculo do
// card com um ticket do helpdesk 360. Se o card ainda não tem codService e
// cod vier preenchido (do detalhe do ticket), preenche.
func (r *TicketRepo) VincularTicket360(ctx context.Context, id int64, uuid, numero, cod string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tickets SET
		   ticket360_id = ?,
		   ticket360_numero = ?,
		   cod_service = CASE WHEN ? != '' AND (cod_service IS NULL OR cod_service = '') THEN ? ELSE cod_service END,
		   updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now')
		 WHERE id = ?`,
		uuid, numero, cod, cod, id)
	if err != nil {
		return fmt.Errorf("vincular ticket 360: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrTicketNaoEncontrado
	}
	return nil
}

func scanTicket(rows *sql.Rows, t *Ticket) error {
	return rows.Scan(&t.ID, &t.NomeCliente, &t.TipoTicket, &t.NumeroTicket, &t.CodService,
		&t.Endereco, &t.Telefone, &t.ColunaAtual, &t.Posicao, &t.CreatedAt, &t.UpdatedAt,
		&t.Ticket360ID, &t.Ticket360Numero, &t.Observacoes,
		&t.VisitaData, &t.VisitaTurno, &t.VisitaHora, &t.VisitaEndereco)
}

// SetVisita grava (ou limpa) o agendamento de visita do card.
func (r *TicketRepo) SetVisita(ctx context.Context, id int64, data, turno, hora, endereco string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tickets SET visita_data = ?, visita_turno = ?, visita_hora = ?, visita_endereco = ?,
		   updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now')
		 WHERE id = ?`,
		data, turno, hora, endereco, id)
	if err != nil {
		return fmt.Errorf("salvar visita: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrTicketNaoEncontrado
	}
	return nil
}

// SetObservacoes grava (ou limpa) as observações internas do card.
func (r *TicketRepo) SetObservacoes(ctx context.Context, id int64, obs string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tickets SET observacoes = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id = ?`,
		obs, id)
	if err != nil {
		return fmt.Errorf("salvar observações: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrTicketNaoEncontrado
	}
	return nil
}
