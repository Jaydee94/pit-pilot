# Generated copies — DO NOT EDIT

These SQL files are copies of `backend/internal/store/migrations/` (the source of truth).
Kustomize's load restrictions prevent referencing files outside `deploy/`, so the migrate
Job's ConfigMap is generated from these local copies.

After adding or changing a migration in `backend/internal/store/migrations/`:
1. Run `make sync-migrations`
2. If you ADDED a new file, list it in `deploy/base/kustomization.yaml` under `configMapGenerator`.
