.PHONY: sync-migrations
sync-migrations: ## Copy schema migrations into the kustomize deploy dir (run after adding a migration)
	cp backend/internal/store/migrations/*.sql deploy/base/migrations/
	@echo "Synced. If you ADDED a new migration file, also list it in deploy/base/kustomization.yaml configMapGenerator."
