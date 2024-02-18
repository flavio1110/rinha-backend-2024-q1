#!/bin/bash

export LOCAL_ENV=true
export HTTP_PORT=9999
export DB_URL="postgres://user:super-secret@localhost:5432/accounts?sslmode=disable"
export DB_MAX_CONNECTIONS=90
export DB_MIN_CONNECTIONS=5
export DB_USE_BATCH_INSERTS=false

CGO_ENABLED=0 go build  -gcflags="all=-N -l"  -o ./bin/rinha-backend-2024-q1 .

./bin/rinha-backend-2024-q1
