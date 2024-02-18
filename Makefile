.PHONY: RUN
run:
	@./scripts/run.sh

.PHONY: lint
lint:
	@golangci-lint run --fix

.PHONY: tests
tests:
	@go test ./...

.PHONY: install-dependencies
install-dependencies:
	@go get ./...

.PHONY: tidy
tidy:
	@go mod tidy

.PHONY: prepare-commit
prepare-commit: lint tests
	@echo lint and testing passed

.PHONY: down-deps
down-deps:
	@docker-compose -f docker-compose.yml -p "flavio1110-rinha-2024-q1" down

.PHONY: up-deps
up-deps:
	@docker-compose -f ./deploy/docker-compose.yml -p "flavio1110-rinha-2024-q1" up -d --force-recreate postgres  

.PHONY: build-docker
build-docker:
	@./scripts/build-image.sh

.PHONY: compose-up
compose-up:
	@./scripts/build-image.sh
	@docker-compose -f ./deploy/docker-compose.yml -p "flavio1110-rinha-2024-q1" up -d --force-recreate --renew-anon-volumes

.PHONY: compose-complete-down
compose-down:
	@docker-compose -f ./deploy/docker-compose.yml -p "flavio1110-rinha-2024-q1" down

.PHONY: prepare-load-test
prepare-load-test:
	@rm -rf rinha-original
	@mkdir rinha-original
	@git clone --single-branch --quiet https://github.com/zanfranceschi/rinha-de-backend-2024-q1 rinha-original
	@wget https://repo1.maven.org/maven2/io/gatling/highcharts/gatling-charts-highcharts-bundle/3.9.5/gatling-charts-highcharts-bundle-3.9.5-bundle.zip -P rinha-original
	@unzip rinha-original/gatling-charts-highcharts-bundle-3.9.5-bundle.zip -d rinha-original
	@cd ..

.PHONY: load-test
load-test:
	@./scripts/build-image.sh
	@docker-compose -f ./deploy/docker-compose.yml -p "flavio1110-rinha-2024-q1" up -d --force-recreate --renew-anon-volumes
	
	@./rinha-original/gatling-charts-highcharts-bundle-3.9.5/bin/gatling.sh -rm local -s RinhaBackendCrebitosSimulation -rd "DESCRICAO" -rf $$WORKSPACE/user-files/results -sf $$WORKSPACE/user-files/simulations -rsf $$WORKSPACE/user-files/resources