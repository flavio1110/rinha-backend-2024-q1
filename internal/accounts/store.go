package accounts

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

type AccountsDBStore struct {
	dbPool *pgxpool.Pool
}

type DBConfig struct {
	DbURL   string
	MaxConn int32
	MinConn int32
}

func NewAccountsDBStore(
	config DBConfig) (*AccountsDBStore, func(), error) {

	pgxConfig, err := pgxpool.ParseConfig(config.DbURL)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing db url: %w", err)
	}

	pgxConfig.MinConns = config.MinConn
	pgxConfig.MaxConns = config.MaxConn
	pgxConfig.MaxConnIdleTime = time.Minute * 3

	dbPool, err := pgxpool.NewWithConfig(context.Background(), pgxConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("creating connection pool: %w", err)
	}

	return &AccountsDBStore{
		dbPool: dbPool,
	}, dbPool.Close, nil
}

func (s *AccountsDBStore) GetAllClients(ctx context.Context) ([]client, error) {
	rows, err := s.dbPool.Query(ctx, "SELECT id, acc_limit, balance FROM accounts")
	if err != nil {
		return nil, fmt.Errorf("querying clients: %w", err)
	}
	defer rows.Close()

	var clients []client
	for rows.Next() {
		var c client
		if err := rows.Scan(&c.ID, &c.Limit, &c.Balance); err != nil {
			return nil, fmt.Errorf("scanning client: %w", err)
		}
		clients = append(clients, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}

	return clients, nil
}

func (s *AccountsDBStore) AddTransaction(ctx context.Context, clientID int, transaction transaction) (currentBalance, error) {
	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		return currentBalance{}, fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && err != pgx.ErrTxClosed {
			log.Err(err).Msg("rolling back transaction")
		}
	}()

	var (
		newBalance     int64
		limit          int64
		amountToChange int64 = transaction.Amount
	)

	if transaction.Type == Debit {
		amountToChange = -amountToChange
	}

	updateStt := `UPDATE account 
				  SET balance = balance + $1 
				  WHERE id = $2 &&  AND acc_limit >= ABS(balance + $1)
				  RETURNING balance, limit`

	err = tx.QueryRow(ctx, updateStt, amountToChange, clientID).Scan(&newBalance, &limit)
	if err != nil {
		return currentBalance{}, fmt.Errorf("querying current balance: %w", err)
	}

	insertStt := `INSERT INTO transactions (client_id, amount, description, type)
				  VALUES ($1, $2, $3, $4)`

	_, err = tx.Exec(ctx, insertStt, clientID, transaction.Amount, transaction.Description, transaction.Type)
	if err != nil {
		return currentBalance{}, fmt.Errorf("updating balance: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return currentBalance{}, fmt.Errorf("committing transaction: %w", err)
	}

	return currentBalance{
		Balance: newBalance,
		Limit:   limit,
	}, nil
}

func (s *AccountsDBStore) GetStatement(ctx context.Context, clientID int) (statement, error) {
	queryTransactions := `SELECT t.amount, t.description, t.type, t.created_at, a.acc_limit, a.balance 
						  FROM transactions t
						  JOIN accounts a ON t.account_id = a.id
						  WHERE a.id = $1 
						  ORDER_BY t.created_at desc LIMIT 10`

	rows, err := s.dbPool.Query(ctx, queryTransactions, clientID)
	if err != nil {
		return statement{}, fmt.Errorf("querying transactions: %w", err)
	}
	defer rows.Close()

	var (
		transactions []transaction
		currBalance  int64
		limit        int64
	)

	for rows.Next() {
		var t transaction
		if err := rows.Scan(&t.Amount, &t.Description, &t.Type, &t.CreateAt, &limit, &currBalance); err != nil {
			return statement{}, fmt.Errorf("scanning transaction: %w", err)
		}
		transactions = append(transactions, t)
	}

	if err := rows.Err(); err != nil {
		return statement{}, fmt.Errorf("iterating rows: %w", err)
	}

	return statement{
		Transactions: transactions,
		Balance: balance{
			Date:  time.Now().UTC(),
			Total: currBalance,
			Limit: limit,
		},
	}, nil
}
