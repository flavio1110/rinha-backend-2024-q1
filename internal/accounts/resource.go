package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

type clientResource struct {
	store           AccountsStore
	existingClients map[string]uint64
}

type AccountsStore interface {
	GetAllClients(ctx context.Context) ([]client, error)
	AddTransaction(ctx context.Context, clientID int, transaction transaction) (currentBalance, error)
	GetStatement(ctx context.Context, clientID int) (statement, error)
}

func (s *clientResource) postTransaction(w http.ResponseWriter, r *http.Request) {
	id, err := s.getClientID(r)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var t transaction

	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	defer r.Body.Close()

	if !t.isValid() {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	newBalance, err := s.store.AddTransaction(r.Context(), id, t)

	if err != nil {
		if errors.Is(err, errInsufficientFunds) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}

		log.Err(err).Msg("error adding transaction")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	writeJsonResponse(w, http.StatusOK, newBalance)
}

func (s *clientResource) getStatement(w http.ResponseWriter, r *http.Request) {
	id, err := s.getClientID(r)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	st, err := s.store.GetStatement(r.Context(), id)
	if err != nil {
		log.Err(err).Msg("error getting statement")
		writeResponse(w, http.StatusInternalServerError, "")
		return
	}

	writeJsonResponse(w, http.StatusOK, st)

}

func (s *clientResource) getClientID(r *http.Request) (int, error) {
	idParam := mux.Vars(r)["id"]

	if _, exists := s.existingClients[idParam]; !exists {
		return 0, errors.New("client not found")
	}

	id, err := strconv.Atoi(idParam)
	if err != nil {
		return 0, err
	}

	return id, nil
}

func (s *clientResource) warmup(w http.ResponseWriter, r *http.Request) {
	err := s.loadExistingClients(r.Context())

	if err != nil {
		log.Err(err).Msg("error getting all clients")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *clientResource) loadExistingClients(ctx context.Context) error {
	s.existingClients = make(map[string]uint64)

	clients, err := s.store.GetAllClients(ctx)
	if err != nil {
		return fmt.Errorf("getting all clients: %w", err)
	}

	for _, client := range clients {
		s.existingClients[fmt.Sprintf("%d", client.ID)] = uint64(client.Limit)
	}

	return nil
}

type transaction struct {
	Amount      int64           `json:"valor"`
	Description string          `json:"descricao"`
	Type        TransactionType `json:"tipo"`
	CreateAt    time.Time       `json:"realizada_em"`
	AccountID   int             `json:"-"`
}

func (t *transaction) isValid() bool {
	if t.Amount <= 0 {
		return false
	}
	if len(t.Description) < 1 || len(t.Description) > 10 {
		return false
	}
	if t.Type != Credit && t.Type != Debit {
		return false
	}
	return true
}

type TransactionType string

const (
	Debit  TransactionType = "d"
	Credit TransactionType = "c"
)

type currentBalance struct {
	Limit   int64 `json:"limite"`
	Balance int64 `json:"saldo"`
}

type statement struct {
	Balance      balance       `json:"saldo"`
	Transactions []transaction `json:"ultimas_transacoes"`
}

type balance struct {
	Total int64     `json:"total"`
	Date  time.Time `json:"data"`
	Limit int64     `json:"limite"`
}

type client struct {
	ID      int   `json:"id"`
	Limit   int64 `json:"limite"`
	Balance int64 `json:"saldo"`
}

var errInsufficientFunds = errors.New("insufficient funds")
