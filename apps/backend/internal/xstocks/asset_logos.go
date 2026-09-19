package xstocks

import "strings"

// assetLogoURLs maps base tickers to stable HTTPS logo URLs for mobile rendering.
var assetLogoURLs = map[string]string{
	"AAPL":  "https://storage.googleapis.com/iexcloud-hl37opg/api/logos/AAPL.png",
	"NVDA":  "https://storage.googleapis.com/iexcloud-hl37opg/api/logos/NVDA.png",
	"TSLA":  "https://storage.googleapis.com/iexcloud-hl37opg/api/logos/TSLA.png",
	"META":  "https://storage.googleapis.com/iexcloud-hl37opg/api/logos/META.png",
	"GOOGL": "https://storage.googleapis.com/iexcloud-hl37opg/api/logos/GOOGL.png",
	"GOOG":  "https://storage.googleapis.com/iexcloud-hl37opg/api/logos/GOOG.png",
	"MSFT":  "https://storage.googleapis.com/iexcloud-hl37opg/api/logos/MSFT.png",
	"AMZN":  "https://storage.googleapis.com/iexcloud-hl37opg/api/logos/AMZN.png",
	"NFLX":  "https://storage.googleapis.com/iexcloud-hl37opg/api/logos/NFLX.png",
	"COIN":  "https://storage.googleapis.com/iexcloud-hl37opg/api/logos/COIN.png",
	"JPM":   "https://storage.googleapis.com/iexcloud-hl37opg/api/logos/JPM.png",
	"DIS":   "https://storage.googleapis.com/iexcloud-hl37opg/api/logos/DIS.png",
	"WMT":   "https://storage.googleapis.com/iexcloud-hl37opg/api/logos/WMT.png",
	"AMD":   "https://storage.googleapis.com/iexcloud-hl37opg/api/logos/AMD.png",
	"INTC":  "https://storage.googleapis.com/iexcloud-hl37opg/api/logos/INTC.png",
	"PYPL":  "https://storage.googleapis.com/iexcloud-hl37opg/api/logos/PYPL.png",
	"CRM":   "https://storage.googleapis.com/iexcloud-hl37opg/api/logos/CRM.png",
	"ORCL":  "https://storage.googleapis.com/iexcloud-hl37opg/api/logos/ORCL.png",
}

// BaseTicker strips the trailing xStock suffix from a catalog symbol.
func BaseTicker(symbol string) string {
	s := strings.TrimSpace(symbol)
	if s == "" {
		return s
	}
	if strings.HasSuffix(strings.ToLower(s), "x") && len(s) > 1 {
		return strings.ToUpper(s[:len(s)-1])
	}
	return strings.ToUpper(s)
}

// CatalogAssetLogoURL returns an HTTPS logo URL for a catalog symbol when known.
func CatalogAssetLogoURL(symbol string) string {
	base := BaseTicker(symbol)
	if base == "" {
		return ""
	}
	if url, ok := assetLogoURLs[base]; ok {
		return url
	}
	return ""
}
