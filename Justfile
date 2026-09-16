set dotenv-load := true
set dotenv-filename := ".env"
set shell := ["bash", "-euo", "pipefail", "-c"]

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
        if [[ ! -f .env.local ]]; then
          echo "error: .env.local missing — copy .env.example and set Privy keys (dotenvx set … -f .env.local)."
          exit 1
        fi
        dotenvx run -f .env.local -- ./scripts/ensure-ios-privy-config.sh generate
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
        ./scripts/require-docker.sh
        if [[ ! -f .env ]]; then
          echo "note: no .env file; using defaults from .env.example via compose"
          export DATABASE_URL="postgres://monaco:monaco@localhost:54322/monaco?sslmode=disable"
          export POSTGRES_USER=monaco
          export POSTGRES_PASSWORD=monaco
          export POSTGRES_DB=monaco
          export POSTGRES_PORT=54322
        fi
        source ./scripts/assert-local-database-url.sh
        docker compose up -d --wait
        ./scripts/apply-migrations.sh
        ./scripts/verify-local-db.sh
        if [[ -f apps/backend/go.mod ]]; then
          # M3-T17–T21 Jupiter quote/execute/sell locked tests run via go test ./...
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
      ./scripts/require-docker.sh
      if [[ ! -f .env ]]; then
        echo "note: copy .env.example to .env for local overrides"
        export DATABASE_URL="postgres://monaco:monaco@localhost:54322/monaco?sslmode=disable"
        export POSTGRES_USER=monaco
        export POSTGRES_PASSWORD=monaco
        export POSTGRES_DB=monaco
        export POSTGRES_PORT=54322
      fi
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
        ./scripts/require-docker.sh
        if [[ ! -f .env ]]; then
          echo "note: copy .env.example to .env for local overrides"
          export DATABASE_URL="postgres://monaco:monaco@localhost:54322/monaco?sslmode=disable"
          export POSTGRES_USER=monaco
          export POSTGRES_PASSWORD=monaco
          export POSTGRES_DB=monaco
          export POSTGRES_PORT=54322
        fi
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
        if command -v ios-sim >/dev/null 2>&1; then
          ./scripts/ios-sim
        else
          ./scripts/with-ios-privy-env.sh bash -c '
            set -euo pipefail
            xcodebuild -project apps/mobile/Monaco.xcodeproj -scheme Monaco \
              -destination "platform=iOS Simulator,id=7B30D45E-62FD-42E2-871A-787B19D38CCF" \
              -configuration Debug build
            xcrun simctl boot 7B30D45E-62FD-42E2-871A-787B19D38CCF 2>/dev/null || true
            xcrun simctl install 7B30D45E-62FD-42E2-871A-787B19D38CCF \
              "$(find ~/Library/Developer/Xcode/DerivedData -name Monaco.app -path "*Debug-iphonesimulator*" | head -1)"
            xcrun simctl launch 7B30D45E-62FD-42E2-871A-787B19D38CCF com.monaco.app
          '
        fi
        ;;
      *)
        echo "error: unknown app '{{app}}' (use backend or mobile)"
        exit 1
        ;;
    esac
