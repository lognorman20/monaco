module github.com/monaco/monaco/apps/backend

go 1.24.0

toolchain go1.24.3

require (
	github.com/ethereum/go-ethereum v1.14.12
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/jackc/pgx/v5 v5.7.5
	github.com/monaco/monaco/packages/domain v0.0.0
	golang.org/x/image v0.32.0
	golang.org/x/text v0.30.0
)

require (
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.0.1 // indirect
	github.com/holiman/uint256 v1.3.1 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/crypto v0.37.0 // indirect
	golang.org/x/sync v0.17.0 // indirect
	golang.org/x/sys v0.32.0 // indirect
)

replace github.com/monaco/monaco/packages/domain => ../../packages/domain
