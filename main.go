package main

import (
	"context"
	"fmt"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/flavio1110/rinha-de-backend-2024-q1/internal/accounts"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	ctx, cancel := context.WithCancel(context.Background())

	dbConfig, err := getDBConfig()
	if err != nil {
		log.Fatal().Err(err).Msg("configure db")
	}

	store, terminateDBPool, err := accounts.NewAccountsDBStore(dbConfig)
	if err != nil {
		log.Fatal().Err(err).Msg("configure db store")
	}
	defer terminateDBPool()

	isLocal := os.Getenv("LOCAL_ENV") == "true"
	port, err := strconv.Atoi(os.Getenv("HTTP_PORT"))
	if err != nil {
		log.Fatal().Err(err).Msgf("Unable to parse HTTP_PORT %q", os.Getenv("HTTP_PORT"))
	}

	server := accounts.NewServer(port, store, isLocal)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	go func() {

		if err := server.Start(ctx); err != nil {
			log.Fatal().Err(err).Msg("Start http server")
		}
	}()

	log.Info().Msg("Server started - waiting for signal to stop")
	<-sig
	log.Info().Msg("Server shutting down")
	cancel()

	if err := server.Stop(ctx); err != nil {
		log.Fatal().Err(err).Msg("Stop server")
	}

	log.Info().Msg("Server stopped")
}

func getDBConfig() (accounts.DBConfig, error) {
	dbURL := os.Getenv("DB_URL")
	maxConnections, err := strconv.Atoi(os.Getenv("DB_MAX_CONNECTIONS"))
	if err != nil {
		return accounts.DBConfig{}, fmt.Errorf("unable to parse DB_MAX_CONNECTIONS %q", os.Getenv("DB_MAX_CONNECTIONS"))
	}

	minConnections, err := strconv.Atoi(os.Getenv("DB_MIN_CONNECTIONS"))
	if err != nil {
		return accounts.DBConfig{}, fmt.Errorf("unable to parse DB_MIN_CONNECTIONS %q", os.Getenv("DB_MIN_CONNECTIONS"))
	}

	return accounts.DBConfig{
		DbURL:   dbURL,
		MaxConn: int32(maxConnections),
		MinConn: int32(minConnections),
	}, nil
}
