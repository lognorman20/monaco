package config

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/errs"
)

type Env string

const (
	EnvLocal      Env = "local"
	EnvTest       Env = "test"
	EnvStaging    Env = "staging"
	EnvProduction Env = "production"
)

func (e Env) Deployed() bool { return e == EnvStaging || e == EnvProduction }

type Config struct {
	Env        Env
	HTTP       HTTP
	Worker     Worker
	DB         DB
	NATS       NATS
	Bus        Bus
	OTel       OTel
	Auth       Auth
	Identity   Identity
	Timeouts   Timeouts
	Jupiter    Jupiter
	XStocks    XStocks
	Tessera    Tessera
	PreStocks  PreStocks
	Market     Market
	Funding    Funding
	Privy      Privy
	Solana     Solana
	Relayer    Relayer
	APNs       APNs
	PostHog    PostHog
	Supabase   Supabase
	Faultpoint string
}

type Identity struct {
	NudgesInterval time.Duration
}

type Auth struct {
	DevTokenKey string
}

type HTTP struct {
	Addr              string
	MaxBodyBytes      int32
	TrustProxyHeaders bool
}

type Worker struct {
	HealthAddr string
}

type DB struct {
	URL      string
	MaxConns int32
}

type NATS struct {
	URL string
}

type Bus struct {
	AckWait  time.Duration
	APIRelay bool
}

type OTel struct {
	Endpoint    string
	Headers     string
	ServiceName string
}

type Jupiter struct {
	SwapBaseURL  string
	PriceBaseURL string
	APIKey       string
}

type XStocks struct {
	BaseURL string
}

type Tessera struct {
	BaseURL string
}

type PreStocks struct {
	BaseURL string
}

type Market struct {
	PricePollInterval time.Duration
}

type Funding struct {
	DepositPollInterval time.Duration
	DepositRPCRate      int32
}

type Privy struct {
	AppID                   string
	AppSecret               string
	VerificationKey         string
	AuthorizationPrivateKey string
	AuthorizationKeyID      string
	WebhookSecret           string
	BaseURL                 string
}

type Solana struct {
	RPCURL   string
	USDCMint string
}

type Relayer struct {
	PrivateKey string
}

type APNs struct {
	KeyP8   string
	KeyID   string
	TeamID  string
	Topic   string
	BaseURL string
}

type PostHog struct {
	APIKey string
	Host   string
}

type Supabase struct {
	URL            string
	ServiceRoleKey string
}

type Timeouts struct {
	RPC             time.Duration
	Privy           time.Duration
	APNs            time.Duration
	JupiterQuote    time.Duration
	JupiterExecute  time.Duration
	XStocks         time.Duration
	Tessera         time.Duration
	PreStocks       time.Duration
	PostHog         time.Duration
	Storage         time.Duration
	HTTPServerRead  time.Duration
	HTTPServerWrite time.Duration
	Shutdown        time.Duration
}

const redacted = "***"

func Load(environ []string) (Config, error) {
	vars := make(map[string]string, len(environ))
	for _, kv := range environ {
		k, v, _ := strings.Cut(kv, "=")
		vars[k] = v
	}
	var cfg Config
	var bad keysError
	known := map[string]bool{}
	for _, f := range fields() {
		known[f.key] = true
		v := vars[f.key]
		if v == "" && f.mandatory {
			bad.missing = append(bad.missing, f.key)
			continue
		}
		if v == "" {
			v = f.fallback
		}
		if !f.set(&cfg, v) {
			bad.invalid = append(bad.invalid, f.key+" ("+f.want+")")
		}
	}
	bad.checkAPNs(cfg)
	bad.missing = append(bad.missing, missingIn(cfg)...)
	for k := range vars {
		if strings.HasPrefix(k, "MONACO_") && !known[k] {
			bad.unknown = append(bad.unknown, k)
		}
	}
	if len(bad.missing)+len(bad.unknown)+len(bad.invalid) > 0 {
		slices.Sort(bad.unknown)
		return Config{}, errs.Wrap(bad, errs.CodeInvalidInput, "config.Load")
	}
	return cfg, nil
}

const DefaultTestDBURL = "postgres://monaco:monaco@localhost:54323/monaco?sslmode=disable"

func TestDBURL(environ []string) string {
	url := DefaultTestDBURL
	for _, kv := range environ {
		if k, v, _ := strings.Cut(kv, "="); k == "TEST_DATABASE_URL" && v != "" {
			url = v
		}
	}
	return url
}

func (c Config) Redacted() map[string]string {
	fs := fields()
	out := make(map[string]string, len(fs))
	for _, f := range fs {
		v := f.get(&c)
		if f.redact {
			v = redacted
		}
		out[f.key] = v
	}
	return out
}

type keysError struct {
	missing []string
	unknown []string
	invalid []string
}

func (e keysError) Error() string {
	var parts []string
	for _, group := range []struct {
		label string
		keys  []string
	}{{"missing", e.missing}, {"unknown", e.unknown}, {"invalid", e.invalid}} {
		if len(group.keys) > 0 {
			parts = append(parts, group.label+" "+strings.Join(group.keys, ", "))
		}
	}
	return strings.Join(parts, "; ")
}

type field struct {
	key         string
	fallback    string
	mandatory   bool
	mandatoryIn Env
	redact      bool
	want        string
	set         func(c *Config, v string) bool
	get         func(c *Config) string
}

func (f field) required() field {
	f.mandatory = true
	return f
}

func (f field) requiredIn(env Env) field {
	f.mandatoryIn = env
	return f
}

func missingIn(cfg Config) []string {
	var keys []string
	for _, f := range fields() {
		if f.mandatoryIn != "" && f.mandatoryIn == cfg.Env && f.get(&cfg) == "" {
			keys = append(keys, f.key)
		}
	}
	return keys
}

func (f field) secret() field {
	f.redact = true
	return f
}

func fields() []field {
	return slices.Concat(platformFields(), marketFields(), fundingFields())
}

func marketFields() []field {
	return []field{
		duration("MONACO_TIMEOUT_JUPITER_QUOTE", 5*time.Second,
			func(c *Config) *time.Duration { return &c.Timeouts.JupiterQuote }),
		duration("MONACO_TIMEOUT_JUPITER_EXECUTE", 2*time.Minute,
			func(c *Config) *time.Duration { return &c.Timeouts.JupiterExecute }),
		duration("MONACO_TIMEOUT_XSTOCKS", 15*time.Second,
			func(c *Config) *time.Duration { return &c.Timeouts.XStocks }),
		duration("MONACO_TIMEOUT_TESSERA", 15*time.Second,
			func(c *Config) *time.Duration { return &c.Timeouts.Tessera }),
		duration("MONACO_TIMEOUT_PRESTOCKS", 15*time.Second,
			func(c *Config) *time.Duration { return &c.Timeouts.PreStocks }),
		text("MONACO_JUPITER_SWAP_BASE_URL", "https://api.jup.ag/swap/v2",
			func(c *Config) *string { return &c.Jupiter.SwapBaseURL }),
		text("MONACO_JUPITER_PRICE_BASE_URL", "https://api.jup.ag/price/v3",
			func(c *Config) *string { return &c.Jupiter.PriceBaseURL }),
		text("JUPITER_API_KEY", "", func(c *Config) *string { return &c.Jupiter.APIKey }).secret(),
		text("XSTOCKS_BASE_URL", "https://api.xstocks.fi", func(c *Config) *string { return &c.XStocks.BaseURL }),
		text("TESSERA_API_BASE_URL", "https://rest-api.tessera.pe",
			func(c *Config) *string { return &c.Tessera.BaseURL }),
		text("PRESTOCKS_API_BASE_URL", "https://prestocks.com",
			func(c *Config) *string { return &c.PreStocks.BaseURL }),
		duration("MARKET_PRICE_POLL_INTERVAL", 120*time.Second,
			func(c *Config) *time.Duration { return &c.Market.PricePollInterval }),
	}
}

func platformFields() []field {
	return []field{
		environment("MONACO_ENV", func(c *Config) *Env { return &c.Env }).required(),
		text("MONACO_HTTP_ADDR", ":8080", func(c *Config) *string { return &c.HTTP.Addr }),
		count("MONACO_HTTP_MAX_BODY_BYTES", 1<<20, func(c *Config) *int32 { return &c.HTTP.MaxBodyBytes }),
		boolean("TRUST_PROXY_HEADERS", func(c *Config) *bool { return &c.HTTP.TrustProxyHeaders }),
		text("MONACO_WORKER_HEALTH_ADDR", ":8081", func(c *Config) *string { return &c.Worker.HealthAddr }),
		text("DATABASE_URL", "", func(c *Config) *string { return &c.DB.URL }).required().secret(),
		count("MONACO_DB_MAX_CONNS", 11, func(c *Config) *int32 { return &c.DB.MaxConns }),
		text("NATS_URL", "", func(c *Config) *string { return &c.NATS.URL }).required().secret(),
		text("OTEL_EXPORTER_OTLP_ENDPOINT", "", func(c *Config) *string { return &c.OTel.Endpoint }),
		text("OTEL_EXPORTER_OTLP_HEADERS", "", func(c *Config) *string { return &c.OTel.Headers }).secret(),
		text("OTEL_SERVICE_NAME", "monaco", func(c *Config) *string { return &c.OTel.ServiceName }),
		text("MONACO_DEV_TOKEN_KEY", "", func(c *Config) *string { return &c.Auth.DevTokenKey }).secret(),
		duration(
			"IDENTITY_NUDGES_INTERVAL",
			24*time.Hour,
			func(c *Config) *time.Duration { return &c.Identity.NudgesInterval },
		),
		duration("MONACO_TIMEOUT_RPC", 5*time.Second, func(c *Config) *time.Duration { return &c.Timeouts.RPC }),
		duration("MONACO_TIMEOUT_PRIVY", 10*time.Second, func(c *Config) *time.Duration { return &c.Timeouts.Privy }),
		duration("MONACO_TIMEOUT_APNS", 10*time.Second, func(c *Config) *time.Duration { return &c.Timeouts.APNs }),
		duration("MONACO_TIMEOUT_HTTP_SERVER_READ", 10*time.Second,
			func(c *Config) *time.Duration { return &c.Timeouts.HTTPServerRead }),
		duration("MONACO_TIMEOUT_HTTP_SERVER_WRITE", 30*time.Second,
			func(c *Config) *time.Duration { return &c.Timeouts.HTTPServerWrite }),
		duration("MONACO_TIMEOUT_SHUTDOWN", 10*time.Second,
			func(c *Config) *time.Duration { return &c.Timeouts.Shutdown }),
		text("PRIVY_APP_ID", "", func(c *Config) *string { return &c.Privy.AppID }),
		text("PRIVY_APP_SECRET", "", func(c *Config) *string { return &c.Privy.AppSecret }).secret(),
		text("PRIVY_VERIFICATION_KEY", "", func(c *Config) *string { return &c.Privy.VerificationKey }),
		text("PRIVY_AUTHORIZATION_PRIVATE_KEY", "",
			func(c *Config) *string { return &c.Privy.AuthorizationPrivateKey }).secret(),
		text("PRIVY_AUTHORIZATION_KEY_ID", "", func(c *Config) *string { return &c.Privy.AuthorizationKeyID }),
		text("PRIVY_WEBHOOK_SECRET", "", func(c *Config) *string { return &c.Privy.WebhookSecret }).secret(),
		text("PRIVY_BASE_URL", "https://api.privy.io", func(c *Config) *string { return &c.Privy.BaseURL }),
		text("SOLANA_RPC_URL", "https://api.mainnet-beta.solana.com",
			func(c *Config) *string { return &c.Solana.RPCURL }).secret(),
		text("SOLANA_USDC_MINT", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
			func(c *Config) *string { return &c.Solana.USDCMint }),
		text("RELAYER_PRIVATE_KEY", "", func(c *Config) *string { return &c.Relayer.PrivateKey }).secret(),
		text("APNS_KEY_P8", "", func(c *Config) *string { return &c.APNs.KeyP8 }).secret(),
		text("APNS_KEY_ID", "", func(c *Config) *string { return &c.APNs.KeyID }),
		text("APNS_TEAM_ID", "", func(c *Config) *string { return &c.APNs.TeamID }),
		text("APNS_TOPIC", "com.monaco.app", func(c *Config) *string { return &c.APNs.Topic }),
		text("APNS_BASE_URL", "", func(c *Config) *string { return &c.APNs.BaseURL }),
		text("POSTHOG_API_KEY", "", func(c *Config) *string { return &c.PostHog.APIKey }).secret().
			requiredIn(EnvProduction),
		text("POSTHOG_HOST", "https://us.i.posthog.com", func(c *Config) *string { return &c.PostHog.Host }),
		duration("MONACO_TIMEOUT_POSTHOG", 3*time.Second,
			func(c *Config) *time.Duration { return &c.Timeouts.PostHog }),
		text("SUPABASE_URL", "", func(c *Config) *string { return &c.Supabase.URL }),
		text("SUPABASE_SERVICE_ROLE_KEY", "", func(c *Config) *string { return &c.Supabase.ServiceRoleKey }).secret(),
		duration("MONACO_TIMEOUT_STORAGE", 10*time.Second,
			func(c *Config) *time.Duration { return &c.Timeouts.Storage }),
		text("MONACO_FAULTPOINT", "", func(c *Config) *string { return &c.Faultpoint }),
		optionalDuration("MONACO_BUS_ACK_WAIT", func(c *Config) *time.Duration { return &c.Bus.AckWait }),
		toggle("MONACO_BUS_API_RELAY", true, func(c *Config) *bool { return &c.Bus.APIRelay }),
	}
}

func fundingFields() []field {
	return []field{
		duration("FUNDING_DEPOSIT_POLL_INTERVAL", 30*time.Second,
			func(c *Config) *time.Duration { return &c.Funding.DepositPollInterval }),
		count("FUNDING_DEPOSIT_RPC_RATE", 20, func(c *Config) *int32 { return &c.Funding.DepositRPCRate }),
	}
}

func optionalDuration(key string, at func(*Config) *time.Duration) field {
	return field{
		key:  key,
		want: "empty or a positive duration like 100ms",
		set: func(c *Config, v string) bool {
			if v == "" {
				*at(c) = 0
				return true
			}
			d, err := time.ParseDuration(v)
			*at(c) = d
			return err == nil && d > 0
		},
		get: func(c *Config) string {
			if *at(c) == 0 {
				return ""
			}
			return at(c).String()
		},
	}
}

func toggle(key string, fallback bool, at func(*Config) *bool) field {
	name := map[bool]string{true: "on", false: "off"}
	return field{
		key:      key,
		fallback: name[fallback],
		want:     "on or off",
		set: func(c *Config, v string) bool {
			*at(c) = v == "on"
			return v == "on" || v == "off"
		},
		get: func(c *Config) string { return name[*at(c)] },
	}
}

func text(key, fallback string, at func(*Config) *string) field {
	return field{
		key:      key,
		fallback: fallback,
		set:      func(c *Config, v string) bool { *at(c) = v; return true },
		get:      func(c *Config) string { return *at(c) },
	}
}

func environment(key string, at func(*Config) *Env) field {
	envs := []Env{EnvLocal, EnvTest, EnvStaging, EnvProduction}
	return field{
		key:  key,
		want: "local, test, staging or production",
		set: func(c *Config, v string) bool {
			*at(c) = Env(v)
			return slices.Contains(envs, Env(v))
		},
		get: func(c *Config) string { return string(*at(c)) },
	}
}

func count(key string, fallback int32, at func(*Config) *int32) field {
	return field{
		key:      key,
		fallback: strconv.Itoa(int(fallback)),
		want:     "positive integer",
		set: func(c *Config, v string) bool {
			n, err := strconv.ParseInt(v, 10, 32)
			*at(c) = int32(n)
			return err == nil && n > 0
		},
		get: func(c *Config) string { return strconv.Itoa(int(*at(c))) },
	}
}

func boolean(key string, at func(*Config) *bool) field {
	return field{
		key:      key,
		fallback: "false",
		want:     "true or false",
		set: func(c *Config, v string) bool {
			b, err := strconv.ParseBool(v)
			*at(c) = b
			return err == nil
		},
		get: func(c *Config) string { return strconv.FormatBool(*at(c)) },
	}
}

func duration(key string, fallback time.Duration, at func(*Config) *time.Duration) field {
	return field{
		key:      key,
		fallback: fallback.String(),
		want:     "positive duration like 5s",
		set: func(c *Config, v string) bool {
			d, err := time.ParseDuration(v)
			*at(c) = d
			return err == nil && d > 0
		},
		get: func(c *Config) string { return at(c).String() },
	}
}
