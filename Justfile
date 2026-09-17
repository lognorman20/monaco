set dotenv-load := true
set dotenv-filename := ".env"
set shell := ["bash", "-euo", "pipefail", "-c"]

# Re-exec recipe under dotenvx once (decrypts .env.local into the process).
# Usage inside a recipe body: _dotenvx just <recipe> <args...>
_dotenvx := "./scripts/with-dotenv-local.sh"

default:
    @just --list

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
        xcodebuild -project apps/mobile/Monaco.xcodeproj -scheme Monaco \
          -destination 'platform=iOS Simulator,id=7B30D45E-62FD-42E2-871A-787B19D38CCF' \
          -configuration Debug build
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
      backend_pid=""
      cleanup() {
        if [[ -n "${backend_pid}" ]] && kill -0 "${backend_pid}" 2>/dev/null; then
          kill "${backend_pid}" 2>/dev/null || true
          wait "${backend_pid}" 2>/dev/null || true
        fi
      }
      trap cleanup EXIT INT TERM
      echo "Starting backend (background) and mobile (foreground)..."
      (cd apps/backend && go run ./cmd/api) &
      backend_pid=$!
      if ! kill -0 "${backend_pid}" 2>/dev/null; then
        echo "error: backend failed to start"
        exit 1
      fi
      just run mobile
      cleanup
      trap - EXIT INT TERM
      exit 0
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
        if [[ -f apps/backend/go.mod ]]; then
          (cd apps/backend && go run ./cmd/api)
        else
          echo "M0: apps/backend not scaffolded yet. DB is up; wire the API in M0-T3."
          exit 1
        fi
        ;;
      mobile)
        if [[ ! -d apps/mobile ]]; then
          echo "error: apps/mobile is not scaffolded yet (M0-T4)."
          exit 1
        fi
        # Privy: xcconfig + SIMCTL_CHILD_* via with-ios-privy-env, then gold ios-sim
        ./scripts/ios-sim
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
        xcodebuild -project apps/mobile/Monaco.xcodeproj -scheme Monaco \
          -destination 'platform=iOS Simulator,id=7B30D45E-62FD-42E2-871A-787B19D38CCF' \
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
