set dotenv-load := true
set dotenv-filename := ".env"
set shell := ["bash", "-euo", "pipefail", "-c"]

# Re-exec recipe under dotenvx once (decrypts .env.local into the process).
# Usage inside a recipe body: _dotenvx just <recipe> <args...>
_dotenvx := "./scripts/with-dotenv-local.sh"

default:
    @just --list

# Interactive clone setup. Asks before each install. `just install --check` reports only.
install *flags:
    ./scripts/install-dev.sh {{flags}}

# Encrypt or decrypt repo-root .env.local via dotenvx (file ops — not with-dotenv-local re-exec).
encrypt:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ ! -f .env.local ]]; then
      echo "error: .env.local missing — copy .env.example to .env.local, or place a teammate encrypted .env.local plus .env.keys in the clone root." >&2
      exit 1
    fi
    if ! command -v dotenvx >/dev/null 2>&1; then
      echo "error: dotenvx not on PATH. Install: https://dotenvx.com/docs/install" >&2
      exit 1
    fi
    dotenvx encrypt -f .env.local
    if [[ -f .env.production ]]; then
      dotenvx encrypt -f .env.production
    fi

decrypt:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ ! -f .env.local ]]; then
      echo "error: .env.local missing — copy .env.example to .env.local, or place a teammate encrypted .env.local plus .env.keys in the clone root." >&2
      exit 1
    fi
    if ! command -v dotenvx >/dev/null 2>&1; then
      echo "error: dotenvx not on PATH. Install: https://dotenvx.com/docs/install" >&2
      exit 1
    fi
    dotenvx decrypt -f .env.local
    if [[ -f .env.production ]]; then
      dotenvx decrypt -f .env.production
    fi

# Print decrypted .env.local keys/values (.env.production omitted).
show-env:
    #!/usr/bin/env bash
    set -euo pipefail
    {{_dotenvx}} dotenvx get -f .env.local --format eval-export
    if [[ -f .env.production ]]; then
      echo "note: .env.production exists but is omitted (dev default)." >&2
    fi

build app:
    #!/usr/bin/env bash
    set -euo pipefail
    case "{{app}}" in
      backend)
        if [[ ! -f apps/backend/go.mod ]]; then
          echo "error: apps/backend is not scaffolded yet (M0-T3)."
          exit 1
        fi
        mkdir -p bin
        (cd apps/backend && go build -o ../../bin/monaco-api ./cmd/api)
        ;;
      mobile)
        if [[ ! -d apps/mobile ]]; then
          echo "error: apps/mobile is not scaffolded yet (M0-T4)."
          exit 1
        fi
        # ensure-ios-privy-config reads .env.local via dotenvx get → Privy.local.xcconfig
        ./scripts/ensure-ios-privy-config.sh generate
        ./scripts/ios-build
        ;;
      *)
        echo "error: unknown app '{{app}}' (use backend or mobile)"
        exit 1
        ;;
    esac

test app:
    #!/usr/bin/env bash
    set -euo pipefail
    case "{{app}}" in
      backend)
        if [[ "${MONACO_DOTENVX:-}" != "1" ]]; then
          exec {{_dotenvx}} env MONACO_DOTENVX=1 just test backend
        fi
        ./scripts/require-docker.sh
        source ./scripts/assert-local-database-url.sh
        docker compose up -d --wait
        ./scripts/apply-migrations.sh
        ./scripts/ensure-test-database.sh
        ./scripts/verify-local-db.sh
        if [[ -f apps/backend/go.mod ]]; then
          (cd apps/backend && go test -p 1 ./...)
        else
          echo "M0: apps/backend not scaffolded. Local DB smoke test passed."
        fi
        if [[ "${SKIP_SCRIPTS_TESTS:-}" != "1" && -f scripts/go.mod ]]; then
          (cd scripts && go test -short ./...)
        fi
        ;;
      mobile)
        if [[ ! -d apps/mobile ]]; then
          echo "error: apps/mobile is not scaffolded yet (M0-T4)."
          exit 1
        fi
        if [[ ! -f packages/mobile-core/Package.swift ]]; then
          echo "error: packages/mobile-core is not scaffolded yet."
          exit 1
        fi
        # Host unit tests only (swift test on macOS). iOS sim UI tests stay on just build/run mobile.
        (cd packages/mobile-core && swift test)
        ;;
      *)
        echo "error: unknown app '{{app}}' (use backend or mobile)"
        exit 1
        ;;
    esac

run *app:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ -z "{{app}}" ]]; then
      missing=()
      if [[ ! -f apps/backend/go.mod ]]; then
        missing+=("apps/backend (M0-T3)")
      fi
      if [[ ! -d apps/mobile ]]; then
        missing+=("apps/mobile (M0-T4)")
      fi
      if [[ ${#missing[@]} -gt 0 ]]; then
        echo "error: cannot run full stack. Missing:"
        for item in "${missing[@]}"; do
          echo "  - ${item}"
        done
        echo ""
        echo "Run per-app recipes once scaffold exists: just run backend | just run mobile"
        exit 1
      fi
      if [[ "${MONACO_DOTENVX:-}" != "1" ]]; then
        exec {{_dotenvx}} env MONACO_DOTENVX=1 just run
      fi
      ./scripts/require-docker.sh
      source ./scripts/assert-local-database-url.sh
      docker compose up -d --wait
      ./scripts/apply-migrations.sh
      echo ""
      echo "Local Postgres is ready."
      echo "  DATABASE_URL=${DATABASE_URL}"
      echo ""
      source ./scripts/run-with-logs.sh
      monaco_init_logs
      backend_pid=""
      cleanup() {
        if [[ -n "${backend_pid}" ]] && kill -0 "${backend_pid}" 2>/dev/null; then
          kill "${backend_pid}" 2>/dev/null || true
          wait "${backend_pid}" 2>/dev/null || true
        fi
      }
      # INT/TERM only — ios-sim exits after launch; do not kill API on mobile recipe return.
      trap cleanup INT TERM
      echo "Starting backend (background) and mobile (foreground)..."
      (cd apps/backend && go run ./cmd/api) 2>&1 | tee -a "${MONACO_LOG_DIR}/backend.log" &
      backend_pid=$!
      if ! kill -0 "${backend_pid}" 2>/dev/null; then
        echo "error: backend failed to start"
        exit 1
      fi
      export MONACO_LOG_DIR
      just run mobile
      echo ""
      echo "Simulator launched. Backend still running — Ctrl+C to stop."
      wait "${backend_pid}" 2>/dev/null || true
      trap - INT TERM
    fi
    case "{{app}}" in
      backend)
        if [[ "${MONACO_DOTENVX:-}" != "1" ]]; then
          exec {{_dotenvx}} env MONACO_DOTENVX=1 just run backend
        fi
        ./scripts/require-docker.sh
        source ./scripts/assert-local-database-url.sh
        docker compose up -d --wait
        ./scripts/apply-migrations.sh
        echo ""
        echo "Local Postgres is ready."
        echo "  DATABASE_URL=${DATABASE_URL}"
        echo "  psql: docker compose exec postgres psql -U ${POSTGRES_USER:-monaco} -d ${POSTGRES_DB:-monaco}"
        echo ""
        source ./scripts/run-with-logs.sh
        monaco_init_logs
        if [[ -f apps/backend/go.mod ]]; then
          (cd apps/backend && go run ./cmd/api) 2>&1 | tee -a "${MONACO_LOG_DIR}/backend.log"
        else
          echo "M0: apps/backend not scaffolded yet. DB is up; wire the API in M0-T3."
          exit 1
        fi
        ;;
      mobile)
        if [[ "${MONACO_DOTENVX:-}" != "1" ]]; then
          exec {{_dotenvx}} env MONACO_DOTENVX=1 just run mobile
        fi
        if [[ ! -d apps/mobile ]]; then
          echo "error: apps/mobile is not scaffolded yet (M0-T4)."
          exit 1
        fi
        # Privy: xcconfig + SIMCTL_CHILD_* via with-ios-privy-env, then scripts/ios-sim
        source ./scripts/run-with-logs.sh
        monaco_init_logs
        ./scripts/ios-sim 2>&1 | tee -a "${MONACO_LOG_DIR}/mobile.log"
        ;;
      *)
        echo "error: unknown app '{{app}}' (use backend or mobile)"
        exit 1
        ;;
    esac

stop *app:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ -z "{{app}}" ]]; then
      ./scripts/stop-backend.sh
      ./scripts/stop-mobile.sh
      exit 0
    fi
    case "{{app}}" in
      backend)
        ./scripts/stop-backend.sh
        ;;
      mobile)
        ./scripts/stop-mobile.sh
        ;;
      *)
        echo "error: unknown app '{{app}}' (use backend or mobile)"
        exit 1
        ;;
    esac

reset *target:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ -z "{{target}}" ]]; then
      ./scripts/stop-backend.sh
      ./scripts/stop-mobile.sh
      if [[ "${MONACO_DOTENVX:-}" != "1" ]]; then
        exec {{_dotenvx}} env MONACO_DOTENVX=1 just reset
      fi
      ./scripts/reset-db.sh
      exit 0
    fi
    case "{{target}}" in
      backend)
        ./scripts/stop-backend.sh
        rm -f bin/monaco-api
        echo "removed bin/monaco-api"
        ;;
      mobile)
        ./scripts/stop-mobile.sh
        if [[ ! -d apps/mobile ]]; then
          echo "error: apps/mobile is not scaffolded yet (M0-T4)."
          exit 1
        fi
        gold_udid="$(./scripts/resolve-ios-sim.sh)"
        xcodebuild -project apps/mobile/Monaco.xcodeproj -scheme Monaco \
          -destination "platform=iOS Simulator,id=${gold_udid}" \
          clean
        echo "xcodebuild clean complete"
        ;;
      db)
        if [[ "${MONACO_DOTENVX:-}" != "1" ]]; then
          exec {{_dotenvx}} env MONACO_DOTENVX=1 just reset db
        fi
        ./scripts/reset-db.sh
        ;;
      *)
        echo "error: unknown target '{{target}}' (use backend, mobile, or db)"
        exit 1
        ;;
    esac

# Seed #153 demo data into local Postgres: `just faker scale`, `just faker mixed <group_id>`,
# `just faker all <group_id>`, `just faker demo <group_id> [proposal_id]` (recording variant).
# Local DATABASE_URL only; never calls Privy/RPC/Jupiter. Refresh path: `just reset db` then `just faker ...`.
faker profile *ids:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ "${MONACO_DOTENVX:-}" != "1" ]]; then
      exec {{_dotenvx}} env MONACO_DOTENVX=1 just faker {{profile}} {{ids}}
    fi
    source ./scripts/assert-local-database-url.sh
    read -r -a ids <<< "{{ids}}"
    if (( ${#ids[@]} > 2 )); then
      echo "error: usage: just faker <profile> [group_id] [proposal_id]" >&2
      exit 1
    fi
    args=(-profile "{{profile}}")
    if (( ${#ids[@]} >= 1 )); then
      args+=(-group-id "${ids[0]}")
    fi
    if (( ${#ids[@]} == 2 )); then
      args+=(-proposal-id "${ids[1]}")
    fi
    go run -C apps/backend ./cmd/faker-seed "${args[@]}"

relayer target:
    #!/usr/bin/env bash
    set -euo pipefail
    case "{{target}}" in
      balance)
        if [[ "${MONACO_DOTENVX:-}" != "1" ]]; then
          exec {{_dotenvx}} env MONACO_DOTENVX=1 just relayer balance
        fi
        go run -C apps/backend ./cmd/print-relayer-pubkey
        ;;
      *)
        echo "error: unknown target '{{target}}' (use balance)"
        exit 1
        ;;
    esac

killports:
    #!/usr/bin/env bash
    set -euo pipefail
    # App dev ports only — Postgres stays up (use just reset db for volume wipe).
    port=8080
    if [[ -n "${API_ADDR:-}" ]]; then
      port="${API_ADDR##*:}"
    elif command -v dotenvx >/dev/null 2>&1 && [[ -f .env.local ]]; then
      addr="$(dotenvx get API_ADDR -f .env.local 2>/dev/null || true)"
      if [[ -n "$addr" ]]; then
        port="${addr##*:}"
      fi
    fi
    ./scripts/kill-listeners.sh "$port"
