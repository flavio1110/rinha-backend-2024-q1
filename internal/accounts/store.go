package accounts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

type AccountsDBStore struct {
	dbPool       *pgxpool.Pool
	chTran       chan transaction
	pumpInterval time.Duration
}

type DBConfig struct {
	DbURL   string
	MaxConn int32
	MinConn int32
}

func NewAccountsDBStore(
	ctx context.Context,
	pumpInterval time.Duration,
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

	store := &AccountsDBStore{
		dbPool:       dbPool,
		chTran:       make(chan transaction, 1000),
		pumpInterval: pumpInterval,
	}

	go store.StartTransactionPump(ctx)

	return store, dbPool.Close, nil
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
	var (
		newBalance     int64
		limit          int64
		amountToChange int64 = transaction.Amount
	)

	if transaction.Type == Debit {
		amountToChange = -amountToChange
	}

	updateStt := `UPDATE accounts 
				  SET balance = balance + $1 
				  WHERE id = $2 AND acc_limit >= ABS(balance + $1)
				  RETURNING balance, acc_limit`

	err := s.dbPool.QueryRow(ctx, updateStt, amountToChange, clientID).Scan(&newBalance, &limit)

	if errors.Is(err, pgx.ErrNoRows) {
		return currentBalance{}, errInsufficientFunds
	}
	if err != nil {
		return currentBalance{}, fmt.Errorf("updating balance: %w", err)
	}

	transaction.AccountID = clientID
	s.chTran <- transaction

	return currentBalance{
		Balance: newBalance,
		Limit:   limit,
	}, nil
}

func (s *AccountsDBStore) GetStatement(ctx context.Context, clientID int) (statement, error) {
	queryTransactions := `SELECT t.amount, t.description, t.type, t.created_at
						  FROM transactions t
						  WHERE t.account_id = $1
						  ORDER BY t.created_at desc LIMIT 10`

	queryBalance := `SELECT a.acc_limit, a.balance
						  FROM accounts a
						  WHERE a.id = $1`

	var (
		transactions []transaction
		currBalance  int64
		limit        int64
	)

	batch := pgx.Batch{}

	batch.Queue(queryTransactions, clientID)
	batch.Queue(queryBalance, clientID)

	results := s.dbPool.SendBatch(ctx, &batch)
	defer results.Close()

	rows, err := results.Query()
	if err != nil {
		return statement{}, fmt.Errorf("querying transactions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var t transaction
		if err := rows.Scan(&t.Amount, &t.Description, &t.Type, &t.CreateAt); err != nil {
			return statement{}, fmt.Errorf("scanning transaction: %w", err)
		}
		transactions = append(transactions, t)
	}

	if err := rows.Err(); err != nil {
		return statement{}, fmt.Errorf("iterating rows: %w", err)
	}

	if transactions == nil {
		transactions = []transaction{}
	}

	if err := results.QueryRow().Scan(&limit, &currBalance); err != nil {
		return statement{}, fmt.Errorf("querying balance: %w", err)
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

func (s *AccountsDBStore) StartTransactionPump(ctx context.Context) {
	log.Ctx(ctx).Info().Msg("starting transaction pump")

	bulk := make([]transaction, 0, 1000)

	pump := func() {
		if len(bulk) == 0 {
			return
		}

		log.Ctx(ctx).Info().Msgf("bulk inserting %d transactions", len(bulk))

		s.bulkInsertTransactions(ctx, bulk)
		bulk = bulk[:0]
	}
	for {
		select {
		case <-ctx.Done():
			pump()
			log.Ctx(ctx).Info().Msg("Stopped transactions pump")
			return
		case t := <-s.chTran:
			bulk = append(bulk, t)

			if len(bulk) == 1000 {
				pump()
			}
		case <-time.After(s.pumpInterval):
			pump()
		}
	}
}

func (s *AccountsDBStore) bulkInsertTransactions(ctx context.Context, bulk []transaction) {
	columns := []string{"account_id", "amount", "description", "type", "created_at"}

	_, err := s.dbPool.CopyFrom(ctx, pgx.Identifier{"transactions"}, columns, pgx.CopyFromSlice(len(bulk), func(i int) ([]any, error) {
		return []any{bulk[i].AccountID, bulk[i].Amount, bulk[i].Description, bulk[i].Type, time.Now().UTC()}, nil
	}))

	if err != nil {
		log.Ctx(ctx).Err(err).Msg("error inserting transactions")
	}
}
