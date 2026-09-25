# WP1 foundation: acceptance screenshots

Captured on an iOS 18 simulator from the Debug design gallery:

```
xcrun simctl launch <sim> com.monaco.app -MonacoDesignGallery [-MonacoDesignGalleryTab primitives|money|controls|toast]
```

| File | Shows |
|---|---|
| `primitives-light.png`, `primitives-dark.png` | CabalMark (five names, one emoji-only, one 40 characters), StockMark (tickers, cash, bot), section header, grouped rows with PnLText and PercentText |
| `money-light.png`, `money-dark.png` | Hero with PnLBadge; MoneyText at `$0.00`, `$1,248.50`, `$12,431,180.00`, unparseable (`—`); sizes; PnLText for `+48.20`, `-7.60`, `+0.00`, `-0.00`; badges |
| `controls-light.png`, `controls-dark.png` | AmountEntry with presets, MonacoSegmented, MonacoTextField, search field, BottomCTA |
| `toast-success-light.png`, `toast-error-dark.png` | Bottom toast above the tab bar |
| `pushed-nav-bar-light.png` | Pushed screen after scrolling: opaque canvas bar with hairline, chevron-only back |
