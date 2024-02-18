package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func Test_Endpoints(t *testing.T) {
	ctx := context.Background()

	tcs := map[string]bool{
		"withBatchInserts":    true,
		"withoutBatchInserts": false,
	}
	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {

			connString, terminateDB, err := startTestDB(ctx)
			require.NoError(t, err)
			defer terminateDB(t)

			store, terminateDBPool, err := newAccountsDBStore(ctx,
				time.Microsecond*1,
				dbConfig{dbURL: connString, maxConn: 10, minConn: 5, useBatchInserts: tc})

			require.NoError(t, err, "configure db store")
			defer terminateDBPool()

			if err := migrateDB(ctx, connString); err != nil {
				require.NoError(t, err, "migrate DB")
			}

			api := newServer(8888, store, true)
			ts := httptest.NewServer(api.server.Handler)
			defer ts.Close()

			t.Run("status", func(t *testing.T) {
				resp, err := ts.Client().Get(ts.URL + "/status")
				assert.NoError(t, err)
				assert.Equal(t, 200, resp.StatusCode)
			})

			t.Run("warmup", func(t *testing.T) {
				resp, err := ts.Client().Get(ts.URL + "/warmup")
				assert.NoError(t, err)
				assert.Equal(t, 200, resp.StatusCode)
			})

			t.Run("get statement for client 1 before transactions", func(t *testing.T) {
				resp, err := ts.Client().Get(fmt.Sprintf("%s/clientes/%d/extrato", ts.URL, 1))
				assert.NoError(t, err)
				assert.Equal(t, 200, resp.StatusCode)

				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				defer func(Body io.ReadCloser) {
					err := Body.Close()
					require.NoError(t, err)
				}(resp.Body)

				var s statement
				err = json.Unmarshal(body, &s)
				require.NoError(t, err)

				assert.Equal(t, int64(100000), s.Balance.Limit)
				assert.Equal(t, int64(0), s.Balance.Total)
				assert.Len(t, s.Transactions, 0)
			})

			t.Run("post transaction", func(t *testing.T) {
				// ClientID 1 has a limit of 100000
				tcs := []tcPost{
					{
						name:               "client not found - not found",
						clientID:           6,
						body:               `{"valor":1000,"tipo":"c","descricao":"+ 1000"}`,
						expectedStatusCode: http.StatusNotFound,
					},
					{
						name:               "empty description",
						clientID:           1,
						body:               `{"valor":1000,"tipo":"c","descricao":""}`,
						expectedStatusCode: http.StatusUnprocessableEntity,
					},
					{
						name:               "null description",
						clientID:           1,
						body:               `{"valor":1000,"tipo":"c","descricao":null}`,
						expectedStatusCode: http.StatusUnprocessableEntity,
					},
					{
						name:               "big description",
						clientID:           1,
						body:               `{"valor":1000,"tipo":"c","descricao":"description bigger thatn 10 chars"}`,
						expectedStatusCode: http.StatusUnprocessableEntity,
					},
					{
						name:               "zero amount",
						clientID:           1,
						body:               `{"valor":0,"tipo":"c","descricao":"zero"}`,
						expectedStatusCode: http.StatusUnprocessableEntity,
					},
					{
						name:               "negative amount",
						clientID:           1,
						body:               `{"valor":-1,"tipo":"c","descricao":"negative"}`,
						expectedStatusCode: http.StatusUnprocessableEntity,
					},
					{
						name:               "decimal amount",
						clientID:           1,
						body:               `{"valor":1.2,"tipo":"c","descricao":"decimal"}`,
						expectedStatusCode: http.StatusUnprocessableEntity,
					},
					{
						name:               "credit 1000 - ok",
						clientID:           1,
						body:               `{"valor":1000,"tipo":"c","descricao":"+ 1000"}`,
						expectedStatusCode: http.StatusOK,
						expectedResponse:   `{"limite":100000, "saldo":1000}`,
					},
					{
						name:               "credit 2000 - ok",
						clientID:           1,
						body:               `{"valor":2000,"tipo":"c","descricao":"+ 2000"}`,
						expectedStatusCode: http.StatusOK,
						expectedResponse:   `{"limite":100000, "saldo":3000}`,
					},
					{
						name:               "debit 103000 - ok",
						clientID:           1,
						body:               `{"valor":103000,"tipo":"d","descricao":"- 103000"}`,
						expectedStatusCode: http.StatusOK,
						expectedResponse:   `{"limite":100000, "saldo":-100000}`,
					},
					{
						name:               "debit 1 - Not enough balance",
						clientID:           1,
						body:               `{"valor":1,"tipo":"d","descricao":"descricao"}`,
						expectedStatusCode: http.StatusUnprocessableEntity,
					},
				}
				for _, tc := range tcs {
					t.Run(tc.name, func(t *testing.T) {
						resp, err := ts.Client().Post(fmt.Sprintf("%s/clientes/%d/transacoes", ts.URL, tc.clientID), "application/json", strings.NewReader(tc.body))
						assert.NoError(t, err)
						assert.Equal(t, tc.expectedStatusCode, resp.StatusCode)

						if tc.expectedResponse != "" {
							body, err := io.ReadAll(resp.Body)
							require.NoError(t, err)
							assert.JSONEq(t, tc.expectedResponse, string(body))
						}
					})
				}
			})

			// wait for the pump to process the transactions
			time.Sleep(time.Millisecond * 100)
			t.Run("get statement for client 1", func(t *testing.T) {
				resp, err := ts.Client().Get(fmt.Sprintf("%s/clientes/%d/extrato", ts.URL, 1))
				assert.NoError(t, err)
				assert.Equal(t, 200, resp.StatusCode)

				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				defer func(Body io.ReadCloser) {
					err := Body.Close()
					require.NoError(t, err)
				}(resp.Body)

				var s statement
				err = json.Unmarshal(body, &s)
				require.NoError(t, err)

				assert.Equal(t, int64(100000), s.Balance.Limit)
				assert.Equal(t, int64(-100000), s.Balance.Total)
				assert.Len(t, s.Transactions, 3)

				assert.Equal(t, int64(103000), s.Transactions[0].Amount)
				assert.Equal(t, "- 103000", s.Transactions[0].Description)
				assert.Equal(t, Debit, s.Transactions[0].Type)

				assert.Equal(t, int64(2000), s.Transactions[1].Amount)
				assert.Equal(t, "+ 2000", s.Transactions[1].Description)
				assert.Equal(t, Credit, s.Transactions[1].Type)

				assert.Equal(t, int64(1000), s.Transactions[2].Amount)
				assert.Equal(t, "+ 1000", s.Transactions[2].Description)
				assert.Equal(t, Credit, s.Transactions[2].Type)
			})

			t.Run("get statement for client 6 - Not found", func(t *testing.T) {
				resp, err := ts.Client().Get(fmt.Sprintf("%s/clientes/%d/extrato", ts.URL, 6))
				assert.NoError(t, err)
				assert.Equal(t, http.StatusNotFound, resp.StatusCode)
			})
		})
	}
}

func startTestDB(ctx context.Context) (string, func(t *testing.T), error) {
	var envVars = map[string]string{
		"POSTGRES_USER":     "user",
		"POSTGRES_PASSWORD": "super-secret",
		"POSTGRES_DB":       "people",
		"PORT":              "5432/tcp",
	}

	getConnString := func(host string, port nat.Port) string {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
			envVars["POSTGRES_USER"],
			envVars["POSTGRES_PASSWORD"],
			host,
			port.Port(),
			envVars["POSTGRES_DB"])
	}

	req := testcontainers.ContainerRequest{
		Image:        "postgres:15-alpine",
		ExposedPorts: []string{envVars["PORT"]},
		Env:          envVars,
		WaitingFor:   wait.ForSQL(nat.Port(envVars["PORT"]), "pgx", getConnString).WithStartupTimeout(time.Second * 15),
	}
	pgC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return "", nil, fmt.Errorf("failed to start db container :%w", err)
	}
	port, err := pgC.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return "", nil, fmt.Errorf("failed to get mapped port :%w", err)
	}
	host, err := pgC.Host(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("failed to get host :%w", err)
	}

	connString := getConnString(host, port)

	terminate := func(t *testing.T) {
		if err := pgC.Terminate(ctx); err != nil {
			t.Fatalf("failed to terminate container: %s", err.Error())
		}
	}
	return connString, terminate, nil
}

func migrateDB(ctx context.Context, connString string) error {
	dbPool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return fmt.Errorf("creating connection pool: %w", err)
	}

	initContent, err := os.ReadFile("deploy/initdb.sql")
	if err != nil {
		log.Fatal("read init DB file: ", err)
	}

	_, err = dbPool.Exec(ctx, string(initContent))
	if err != nil {
		return fmt.Errorf("failed to migrate DB: %w", err)
	}
	return nil
}

type tcPost struct {
	name               string
	clientID           int
	body               string
	expectedStatusCode int
	expectedResponse   string
}
