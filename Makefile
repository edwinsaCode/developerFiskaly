# ================================================================
# esaProperti — Makefile
# Jalankan `make help` untuk daftar semua perintah
# ================================================================

.PHONY: help \
        setup start stop restart-backend \
        dev-up dev-down dev-restart dev-logs dev-logs-backend dev-logs-frontend dev-shell-backend dev-shell-db \
        build up down restart logs \
        migrate migrate-prod migrate-down migrate-status migrate-create \
        seed demo-seed reset-demo reset-demo-dry \
        test test-cover lint fmt \
        clean

# ANSI colors
GREEN  := \033[0;32m
YELLOW := \033[0;33m
CYAN   := \033[0;36m
NC     := \033[0m

.DEFAULT_GOAL := help

# Port host yang dipakai docker compose. Dibaca dari .env (hanya dua kunci ini —
# .env tidak di-include penuh karena berisi nilai dengan komentar inline yang
# tidak valid sebagai sintaks make). Tanpa ini `make start` mencetak URL yang
# salah begitu port host digeser.
BACKEND_HOST_PORT  := $(or $(shell sed -n 's/^BACKEND_HOST_PORT=\([0-9]\{1,\}\).*/\1/p' .env 2>/dev/null),8080)
FRONTEND_HOST_PORT := $(or $(shell sed -n 's/^FRONTEND_HOST_PORT=\([0-9]\{1,\}\).*/\1/p' .env 2>/dev/null),3000)

# ================================================================
# Help
# ================================================================

start: ## ⭐ JALANKAN PROYEK (idempoten): siapkan .env, naikkan service, migrasi otomatis
	@if [ ! -f .env ]; then \
		echo "$(YELLOW).env belum ada — menyalin dari .env.example (isi nilainya bila perlu).$(NC)"; \
		cp .env.example .env; \
	fi
	@echo "$(CYAN)Menaikkan service (MySQL → backend → frontend)...$(NC)"
	docker compose up -d
	@echo "$(CYAN)Menunggu MySQL sehat lalu menjalankan migrasi (aman diulang)...$(NC)"
	docker compose --profile tools run --rm migrate
	@echo ""
	@echo "$(GREEN)✓ Proyek siap:$(NC)"
	@echo "  Frontend : http://localhost:$(FRONTEND_HOST_PORT)"
	@echo "  Backend  : http://localhost:$(BACKEND_HOST_PORT)"
	@echo ""
	@echo "Cek log bila ada masalah: $(YELLOW)make dev-logs-backend$(NC)"

stop: ## Hentikan semua service (data MySQL tetap aman)
	docker compose down

restart-backend: ## Restart HANYA backend (mis. setelah backend mati sendiri)
	docker compose restart backend
	@echo "$(GREEN)✓ Backend di-restart. Cek: curl -s localhost:$(BACKEND_HOST_PORT)/health$(NC)"

setup: ## Bootstrap pertama kali: build image dev, generate go.sum & package-lock.json
	@echo "$(CYAN)Building dev images...$(NC)"
	docker compose build
	@echo "$(CYAN)Generating backend go.sum...$(NC)"
	docker compose run --rm --no-deps backend sh -c "go mod tidy"
	@echo "$(CYAN)Installing frontend packages...$(NC)"
	docker compose run --rm --no-deps frontend sh -c "npm install"
	@echo ""
	@echo "$(GREEN)✓ Setup selesai.$(NC)"
	@echo "Langkah berikutnya:"
	@echo "  1. $(YELLOW)cp .env.example .env$(NC)  (isi nilainya)"
	@echo "  2. $(YELLOW)make dev-up$(NC)"
	@echo "  3. $(YELLOW)make migrate$(NC)"

help: ## Tampilkan daftar perintah ini
	@echo ""
	@echo "$(CYAN)esaProperti — Makefile$(NC)"
	@echo ""
	@echo "$(YELLOW)Utama:$(NC)"
	@echo "  $(GREEN)start                    $(NC) ⭐ Jalankan proyek (service + migrasi otomatis, aman diulang)"
	@echo "  $(GREEN)stop                     $(NC) Hentikan semua service"
	@echo "  $(GREEN)restart-backend          $(NC) Restart backend saja bila mati sendiri"
	@echo "  $(GREEN)setup                    $(NC) Bootstrap pertama kali (build image + dependency)"
	@echo ""
	@echo "$(YELLOW)Development:$(NC)"
	@grep -E '^dev-[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(GREEN)%-25s$(NC) %s\n", $$1, $$2}'
	@echo ""
	@echo "$(YELLOW)Production:$(NC)"
	@grep -E '^(build|up|down|restart|logs):.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(GREEN)%-25s$(NC) %s\n", $$1, $$2}'
	@echo ""
	@echo "$(YELLOW)Database:$(NC)"
	@grep -E '^migrate[a-zA-Z_-]*:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(GREEN)%-25s$(NC) %s\n", $$1, $$2}'
	@echo "  $(GREEN)seed                     $(NC) Seed database dengan fixture LITHOS (master data)"
	@echo "  $(GREEN)demo-seed                $(NC) Seed data transaksi demo (RAB/biaya/BAST/pajak)"
	@echo "  $(GREEN)reset-demo-dry           $(NC) Preview apa yang akan dihapus (tanpa eksekusi)"
	@echo "  $(GREEN)reset-demo               $(NC) Hapus transaksi demo — HANYA development"
	@echo ""
	@echo "$(YELLOW)Quality:$(NC)"
	@grep -E '^(test|test-cover|lint|fmt):.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(GREEN)%-25s$(NC) %s\n", $$1, $$2}'
	@echo ""

# ================================================================
# Development
# ================================================================

dev-up: ## Jalankan semua service development (MySQL + backend + frontend)
	@echo "$(CYAN)Starting development environment...$(NC)"
	docker compose up -d
	@echo ""
	@echo "$(GREEN)✓ Services running:$(NC)"
	@echo "  Frontend : http://localhost:$(FRONTEND_HOST_PORT)"
	@echo "  Backend  : http://localhost:$(BACKEND_HOST_PORT)"
	@echo "  MySQL    : localhost:3307"
	@echo ""
	@echo "Jalankan $(YELLOW)make migrate$(NC) jika belum ada tabel."

dev-down: ## Hentikan semua service development
	docker compose down

dev-restart: ## Restart semua service development
	docker compose restart

dev-logs: ## Tail log semua service development
	docker compose logs -f

dev-logs-backend: ## Tail log backend saja
	docker compose logs -f backend

dev-logs-frontend: ## Tail log frontend saja
	docker compose logs -f frontend

dev-shell-backend: ## Buka shell di container backend
	docker compose exec backend sh

dev-shell-db: ## Buka MySQL shell (database esaproperti)
	docker compose exec mysql mysql -u $${MYSQL_USER:-esaproperti} -p$${MYSQL_PASSWORD:-changeme_app} $${MYSQL_DATABASE:-esaproperti}

# ================================================================
# Production
# ================================================================

build: ## Build semua image production
	@echo "$(CYAN)Building production images...$(NC)"
	docker compose -f docker-compose.prod.yml build --no-cache
	@echo "$(GREEN)✓ Build selesai$(NC)"

up: ## Jalankan production environment
	@echo "$(CYAN)Starting production environment...$(NC)"
	docker compose -f docker-compose.prod.yml up -d
	@echo "$(GREEN)✓ Production running$(NC)"

down: ## Hentikan production environment
	docker compose -f docker-compose.prod.yml down

restart: ## Restart production environment
	docker compose -f docker-compose.prod.yml restart

logs: ## Tail log production
	docker compose -f docker-compose.prod.yml logs -f

# ================================================================
# Database (dev default — tambahkan ENV=prod untuk production)
# ================================================================

migrate: ## Jalankan semua pending migration (dev)
	@echo "$(CYAN)Running migrations...$(NC)"
	docker compose --profile tools run --rm migrate
	@echo "$(GREEN)✓ Migrations done$(NC)"

migrate-prod: ## Jalankan semua pending migration (production)
	@echo "$(CYAN)Running migrations on production...$(NC)"
	docker compose -f docker-compose.prod.yml --profile tools run --rm migrate
	@echo "$(GREEN)✓ Migrations done$(NC)"

# Kredensial hidup di .env (dibaca otomatis oleh docker compose untuk file
# compose, TIDAK untuk argumen CLI). Target yang menimpa command service migrate
# harus memuatnya sendiri — tanpa ini DSN jadi "mysql://:@..." dan MySQL menolak
# dengan "Access denied for user ''" yang menyesatkan.
migrate-down: ## Rollback 1 migration (dev)
	@set -a; . ./.env; set +a; \
	docker compose --profile tools run --rm migrate \
		-path /migrations \
		-database "mysql://$$MYSQL_USER:$$MYSQL_PASSWORD@tcp(mysql:3306)/$$MYSQL_DATABASE" \
		down 1

migrate-status: ## Lihat versi migration saat ini (dev)
	@set -a; . ./.env; set +a; \
	docker compose --profile tools run --rm migrate \
		-path /migrations \
		-database "mysql://$$MYSQL_USER:$$MYSQL_PASSWORD@tcp(mysql:3306)/$$MYSQL_DATABASE" \
		version

migrate-create: ## Buat file migration baru: make migrate-create NAME=nama_migration
	@if [ -z "$(NAME)" ]; then echo "$(YELLOW)Usage: make migrate-create NAME=nama_migration$(NC)"; exit 1; fi
	docker compose exec backend migrate create -ext sql -dir /app/migrations -seq $(NAME)
	@echo "$(GREEN)✓ Migration files created in backend/migrations/$(NC)"

TENANT ?= 1

seed: ## Seed database dengan fixture LITHOS (dev). Override tenant: make seed TENANT=2
	@echo "$(CYAN)Seeding database (tenant=$(TENANT))...$(NC)"
	docker compose exec backend go run ./cmd/seed --tenant $(TENANT)
	@echo "$(GREEN)✓ Seed selesai$(NC)"

demo-seed: ## Seed data transaksi demo (RAB, biaya, BAST, pajak) untuk tenant $(TENANT)
	@echo "$(CYAN)Seeding demo transactions (tenant=$(TENANT))...$(NC)"
	@echo "$(YELLOW)Pastikan make seed sudah dijalankan terlebih dahulu.$(NC)"
	docker compose exec backend go run ./cmd/demo-seed --tenant $(TENANT)
	@echo "$(GREEN)✓ Demo seed selesai$(NC)"

reset-demo-dry: ## Preview apa yang akan dihapus oleh reset-demo (tanpa eksekusi)
	@echo "$(YELLOW)[DRY-RUN] Preview reset-demo (tenant=$(TENANT))...$(NC)"
	docker compose exec backend go run ./cmd/reset-demo --tenant $(TENANT) --dry-run

reset-demo: ## Hapus transaksi demo (sale, jurnal terkait) — HANYA development. Lihat dulu: make reset-demo-dry
	@echo "$(YELLOW)⚠  Reset demo data untuk tenant=$(TENANT)...$(NC)"
	@echo "$(YELLOW)   Jalankan 'make reset-demo-dry TENANT=$(TENANT)' untuk preview terlebih dahulu.$(NC)"
	docker compose exec backend go run ./cmd/reset-demo --tenant $(TENANT) --confirm-reset
	@echo "$(GREEN)✓ Reset selesai. Jalankan 'make demo-seed TENANT=$(TENANT)' untuk mengisi ulang.$(NC)"

# ================================================================
# Quality
# ================================================================

test-consistency: ## Financial Consistency Suite (kesetaraan angka lintas modul — butuh MySQL dev)
	@echo "$(CYAN)Running financial consistency suite...$(NC)"
	docker compose exec -T backend sh -c 'TEST_DB_DSN="$$DB_DSN" go test -tags integration ./internal/reporting/ -run TestConsistency -v -count=1'
	@echo "$(GREEN)✓ Consistency suite selesai$(NC)"

test-consistency-strict: ## Suite + tegakkan KNOWN-DIVERGENCE (merah = bukti temuan audit masih ada)
	docker compose exec -T backend sh -c 'TEST_DB_DSN="$$DB_DSN" CONSISTENCY_STRICT=1 go test -tags integration ./internal/reporting/ -run TestConsistency -v -count=1'

test: ## Jalankan semua backend test
	@echo "$(CYAN)Running tests...$(NC)"
	docker compose exec backend go test ./... -v -count=1

test-cover: ## Jalankan backend test + generate coverage report
	docker compose exec backend go test ./... -coverprofile=/tmp/coverage.out -count=1
	docker compose exec backend go tool cover -html=/tmp/coverage.out -o /tmp/coverage.html
	@echo "$(GREEN)✓ Coverage report: backend/coverage.html$(NC)"

lint: ## Jalankan linter (backend + frontend)
	@echo "$(CYAN)Linting backend...$(NC)"
	docker compose exec backend golangci-lint run ./...
	@echo "$(CYAN)Linting frontend...$(NC)"
	cd frontend && npm run lint

fmt: ## Format semua kode (backend + frontend)
	docker compose exec backend gofmt -w .
	docker compose exec backend goimports -w .
	cd frontend && npm run format

# ================================================================
# Misc
# ================================================================

clean: ## Hapus volume dev (HATI-HATI: data MySQL hilang)
	@echo "$(YELLOW)PERINGATAN: Ini akan menghapus semua data MySQL development!$(NC)"
	@read -p "Ketik 'ya' untuk lanjut: " confirm; [ "$$confirm" = "ya" ] || exit 1
	docker compose down -v
	@echo "$(GREEN)✓ Volumes removed$(NC)"
