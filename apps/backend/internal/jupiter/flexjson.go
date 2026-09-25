package jupiter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// jsonFloat unmarshals a JSON number or numeric string. Jupiter v2 sends
// priceImpactPct as a string; older fixtures send a number.
type jsonFloat float64

func (f *jsonFloat) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*f = 0
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			*f = 0
			return nil
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return fmt.Errorf("json float string %q: %w", s, err)
		}
		*f = jsonFloat(v)
		return nil
	}
	var v float64
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*f = jsonFloat(v)
	return nil
}

func (f jsonFloat) Float64() float64 { return float64(f) }

// jsonUnixTime unmarshals unix seconds (number or numeric string) or an RFC3339
// timestamp. Jupiter Price v3 currently sends stockData.updatedAt as ISO-8601.
type jsonUnixTime int64

func (t *jsonUnixTime) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*t = 0
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		sec, err := parseUnixOrRFC3339(s)
		if err != nil {
			return err
		}
		*t = jsonUnixTime(sec)
		return nil
	}
	var n float64
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*t = jsonUnixTime(normalizeUnix(n))
	return nil
}

func (t jsonUnixTime) Unix() int64 { return int64(t) }

func parseUnixOrRFC3339(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if n, err := strconvParseFloat(s); err == nil {
		return normalizeUnix(n), nil
	}
	if tm, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return tm.Unix(), nil
	}
	return 0, fmt.Errorf("updatedAt %q", s)
}

func strconvParseFloat(s string) (float64, error) {
	return json.Number(s).Float64()
}

func normalizeUnix(n float64) int64 {
	if n > 1e12 {
		return int64(n / 1000)
	}
	return int64(n)
}
