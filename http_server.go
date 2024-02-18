package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	//_ "net/http/pprof"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

type apiServer struct {
	server   http.Server
	resource *clientResource
}

func newServer(port int, store accountsStore, isLocal bool) *apiServer {
	r := mux.NewRouter()
	addr := fmt.Sprintf(":%d", port)
	if isLocal {
		addr = fmt.Sprintf("localhost:%d", port)
	}

	api := &apiServer{
		server: http.Server{
			Addr:    addr,
			Handler: r,
		},
		resource: &clientResource{store: store},
	}

	r.Use(setJSONContentType)
	r.HandleFunc("/status", statusHandler).Methods(http.MethodGet)
	r.HandleFunc("/warmup", api.resource.warmup).Methods(http.MethodGet)
	r.HandleFunc("/clientes/{id}/transacoes", api.resource.postTransaction).Methods(http.MethodPost)
	r.HandleFunc("/clientes/{id}/extrato", api.resource.getStatement).Methods(http.MethodGet)

	return api
}

func (s *apiServer) Start(ctx context.Context) error {
	err := s.resource.loadExistingClients(ctx)
	if err != nil {
		return fmt.Errorf("loading existing clients: %w", err)
	}
	log.Info().Msg("Existing clients loaded")

	log.Info().Msgf("Listening HTTP on address %s", s.server.Addr)

	if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listening HTTP: %w", err)
	}

	return nil
}

func (s *apiServer) Stop(ctx context.Context) error {
	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown server: %w", err)
	}
	return nil
}

func statusHandler(w http.ResponseWriter, req *http.Request) {
	writeResponse(w, http.StatusOK, "OK")
}

func writeResponse(w http.ResponseWriter, status int, body string) {
	w.WriteHeader(status)
	_, err := w.Write([]byte(body))
	if err != nil {
		log.Err(err).Msg("error writing response")
	}
}

func writeJsonResponse(w http.ResponseWriter, status int, body any) {
	bodyJson, err := json.Marshal(body)
	if err != nil {
		log.Err(err).Msg("error marshalling response")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	_, err = w.Write(bodyJson)
	if err != nil {
		log.Err(err).Msg("error writing response")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func setJSONContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Add("Content-Type", "application/json")
		next.ServeHTTP(w, req)
	})
}
