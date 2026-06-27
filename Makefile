COMPOSE ?= docker compose

.PHONY: up start build down stop nuke clean logs ps sync-migrations help

help: ## Show this help
	@grep -E '^[a-zA-Z_ -]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

up start: ## Build images and start the full local stack (http://localhost:8080)
	$(COMPOSE) up -d --build
	@echo "pit-pilot is starting at http://localhost:8080 (db migrations run automatically)."

build: ## Build the api + web images
	$(COMPOSE) build

down stop: ## Stop and remove containers (keeps the database volume)
	$(COMPOSE) down

nuke clean: ## Completely remove containers, volumes, and locally built images
	$(COMPOSE) down -v --rmi local --remove-orphans

logs: ## Follow logs from all services
	$(COMPOSE) logs -f

ps: ## Show service status
	$(COMPOSE) ps

sync-migrations: ## Copy schema migrations into the kustomize deploy dir (run after adding a migration)
	cp backend/internal/store/migrations/*.sql deploy/base/migrations/
	@echo "Synced. If you ADDED a new migration file, also list it in deploy/base/kustomization.yaml configMapGenerator."
