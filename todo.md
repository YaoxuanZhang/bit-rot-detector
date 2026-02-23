# Todo

## UI Redesign (see docs/ui-redesign-plan.md for full plan)

### Phase 0 — Config Architecture (prerequisite)
- [ ] Add `gopkg.in/yaml.v3` dependency (`go get`)
- [ ] Refactor `internal/config/config.go`: replace env-var parsing with YAML unmarshal; add `Load(path)` and `Save(cfg, path)`
- [ ] Add `yaml:"-" json:"-"` tags to `SMTP.Username` / `SMTP.Password`; overlay from env after load
- [ ] Remove `internal/settings` package; merge `DiskThresholds` and `NotificationRules` into `Config`
- [ ] Move schedule entries from scheduler into `Config.Schedule`; update scheduler to read from config store
- [ ] Update `internal/api/server.go`: `POST /api/settings` and `POST /api/schedule` write through to YAML
- [ ] Trim `.env.example` to secrets only (`SMTP_USERNAME`, `SMTP_PASSWORD`)
- [ ] Add `config.yaml.example` with all non-secret keys and comments
- [ ] Update `internal/config/config_test.go` and `internal/settings/settings_test.go`

### Phase 1 — Foundation (framework: React + Vite)
- [ ] Scaffold `web/` with Vite + React + TypeScript
- [ ] Configure `vite.config.ts` output to `internal/api/static/`
- [ ] Makefile: add `ui`, `ui-dev`, `build-go`, `clean-ui`; update `build` and `dev` to run `ui` first
- [ ] Dockerfile: split into three stages — ui-builder (Node), go-builder (Go), scratch runtime
- [ ] Add `web/node_modules`, `web/.vite`, `web/dist` to `.dockerignore`
- [ ] Implement card shell (App.tsx) with FLIP expand/collapse animation
- [ ] Wire SSE `done` event and status-poll to auto-refresh all cards

### Phase 2 — Run Card
- [ ] Migrate drive grid, progress bar, toolbar, failures panel into Run card
- [ ] Verify per-drive sync/scrub buttons work inside expanded card

### Phase 3 — History Card
- [ ] Charts auto-refresh after new sync or scrub (SSE `done` event + 30s poll)
- [ ] Migrate charts and run history table into History card
- [ ] Implement spark line for collapsed History card summary
- [ ] Migrate corruption drill-down and comparison modal

### Phase 4 — Settings Card
- [ ] Migrate disk-threshold and notification-rules forms
- [ ] Migrate schedule table
- [ ] Add config parser: display all config keys with source and mutability
- [ ] Backend: `GET /api/config` — enumerate all config entries
- [ ] Backend: `POST /api/config` — apply runtime-mutable key changes
- [ ] Wire config editor in expanded Settings card

### Phase 5 — Polish & Docs
- [ ] Responsive grid (3 → 2 → 1 columns)
- [ ] Keyboard navigation (Escape closes expanded card, Tab moves between cards)
- [ ] Rewrite docs/ui.md for new card-based layout
- [ ] Update docs/api.md with new /api/config endpoints
- [ ] Update docs/architecture.md and README.md
- [ ] Integration tests for new API endpoints
