package mintinfo

import (
	"encoding/json"
	"fmt"
	"time"
)

type accountInfoRPC struct {
	Result *struct {
		Value *struct {
			Owner string `json:"owner"`
			Data  *struct {
				Parsed *struct {
					Info mintParsedInfo `json:"info"`
				} `json:"parsed"`
			} `json:"data"`
		} `json:"value"`
	} `json:"result"`
}

type mintParsedInfo struct {
	Decimals   int               `json:"decimals"`
	Extensions []json.RawMessage `json:"extensions"`
}

type extensionEnvelope struct {
	Extension string          `json:"extension"`
	State     json.RawMessage `json:"state"`
}

type transferFeeConfigState struct {
	OlderTransferFee transferFee `json:"olderTransferFee"`
	NewerTransferFee transferFee `json:"newerTransferFee"`
}

type transferFee struct {
	Epoch                  uint64 `json:"epoch"`
	TransferFeeBasisPoints uint16 `json:"transferFeeBasisPoints"`
}

type scaledUiAmountConfigState struct {
	Multiplier                      string `json:"multiplier"`
	NewMultiplier                   string `json:"newMultiplier"`
	NewMultiplierEffectiveTimestamp int64  `json:"newMultiplierEffectiveTimestamp"`
}

type pausableConfigState struct {
	Paused bool `json:"paused"`
}

func parseAccountInfo(body []byte, mint string, epoch uint64, now time.Time) (Info, error) {
	var resp accountInfoRPC
	if err := json.Unmarshal(body, &resp); err != nil {
		return Info{}, err
	}
	if resp.Result == nil || resp.Result.Value == nil || resp.Result.Value.Data == nil ||
		resp.Result.Value.Data.Parsed == nil {
		return Info{}, fmt.Errorf("mintinfo: empty account info for %s", mint)
	}

	val := resp.Result.Value
	info := val.Data.Parsed.Info
	out := Info{
		Mint:      mint,
		Decimals:  info.Decimals,
		FetchedAt: now,
	}

	if val.Owner != Token2022ProgramID {
		out.UiMultiplier = ratOne()
		out.TransferFeeBps = 0
		return out, nil
	}

	var scaled scaledUiAmountConfigState
	var scaledPresent bool
	var scaledEffectiveTs int64
	var feeCfg transferFeeConfigState
	var feePresent bool
	var paused bool
	var pausedPresent bool
	var permanentDelegate bool

	for _, rawExt := range info.Extensions {
		var env extensionEnvelope
		if err := json.Unmarshal(rawExt, &env); err != nil {
			continue
		}
		switch env.Extension {
		case "transferFeeConfig":
			if err := json.Unmarshal(env.State, &feeCfg); err == nil {
				feePresent = true
			}
		case "scaledUiAmountConfig":
			if err := json.Unmarshal(env.State, &scaled); err == nil {
				scaledPresent = true
				scaledEffectiveTs = scaled.NewMultiplierEffectiveTimestamp
			}
		case "pausableConfig":
			var p pausableConfigState
			if err := json.Unmarshal(env.State, &p); err == nil {
				paused = p.Paused
				pausedPresent = true
			}
		case "permanentDelegate":
			permanentDelegate = true
		}
	}

	if feePresent {
		fee := feeCfg.OlderTransferFee
		if epoch >= feeCfg.NewerTransferFee.Epoch {
			fee = feeCfg.NewerTransferFee
		}
		out.TransferFeeBps = int(fee.TransferFeeBasisPoints)
	}

	if scaledPresent {
		multStr := scaled.Multiplier
		if scaledEffectiveTs != 0 && now.Unix() >= scaledEffectiveTs {
			multStr = scaled.NewMultiplier
		}
		out.UiMultiplier = parseMultiplierString(multStr)
	} else {
		out.UiMultiplier = ratOne()
	}

	if pausedPresent {
		out.Paused = paused
	}
	out.PermanentDelegate = permanentDelegate

	_ = scaledEffectiveTs // used by cache layer via separate tracking if needed

	return out, nil
}

type epochInfoRPC struct {
	Result *struct {
		Epoch uint64 `json:"epoch"`
	} `json:"result"`
}

func parseEpochInfo(body []byte) (uint64, error) {
	var resp epochInfoRPC
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, err
	}
	if resp.Result == nil {
		return 0, fmt.Errorf("mintinfo: empty epoch info")
	}
	return resp.Result.Epoch, nil
}

// multiplierEffectiveTimestamp extracts the effective timestamp from raw account JSON (for cache invalidation).
func multiplierEffectiveTimestamp(body []byte) int64 {
	var resp accountInfoRPC
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0
	}
	if resp.Result == nil || resp.Result.Value == nil || resp.Result.Value.Data == nil ||
		resp.Result.Value.Data.Parsed == nil {
		return 0
	}
	for _, rawExt := range resp.Result.Value.Data.Parsed.Info.Extensions {
		var env extensionEnvelope
		if err := json.Unmarshal(rawExt, &env); err != nil || env.Extension != "scaledUiAmountConfig" {
			continue
		}
		var scaled scaledUiAmountConfigState
		if err := json.Unmarshal(env.State, &scaled); err != nil {
			return 0
		}
		return scaled.NewMultiplierEffectiveTimestamp
	}
	return 0
}
