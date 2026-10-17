---
name: quant-finance
description: How to analyse listed securities with market-data tools: quotes, price history, financial statements, technical indicators and risk, with the arithmetic done by code.
---

# Quantitative finance

Your tools pull Yahoo Finance data through yfinance and do the arithmetic in pandas. Use them for every number. A figure you worked out yourself is not a result; if you need a calculation the tools do not offer, write a script and run it.

## Tools

- `market_quote(symbols)`: price, day change, 52-week range, market cap, P/E, sector for up to 10 symbols.
- `price_history(symbol, period, interval)`: adjusted OHLCV, saved as `data/<SYMBOL>-<period>-<interval>.csv`.
- `financial_statements(symbol, statements, frequency)`: income, balance sheet, cash flow, annual or quarterly, saved as CSV, with margins and changes from the prior period.
- `technical_indicators(symbol, period, indicators)`: SMA 20/50/200, EMA 12/26, RSI 14, MACD, Bollinger bands, ATR 14, saved as CSV, with plain statements about the latest bar.
- `risk_metrics(symbols, period, benchmark)`: annualised return and volatility, Sharpe (risk-free rate 0), maximum drawdown, beta and correlation, and a correlation matrix.

## Method

1. **Pin down the question:** which symbols, which horizon, compared with what, for what decision. Ask if it is not clear. Exchange suffixes select other markets (`.L` London, `.TO` Toronto, `.DE` Xetra); an index is `^GSPC`.
2. **Start with `market_quote`** to confirm the symbol is what you think it is (name, currency, size) before analysing it.
3. **Match the tool to the claim.** Trend and momentum: `technical_indicators`. Quality and growth: `financial_statements`. Risk and diversification: `risk_metrics`. Anything else: save the data with `price_history` and write a script.
4. **Custom analysis** with `execute_terminal_command` (if you have it): the container has plain Python only, so ask for packages in the same command, for example `uv run --with pandas --with numpy --with matplotlib python analysis.py`. It reads the `data/*.csv` files you saved. Plots go to `data/*.png`.
5. **Sanity-check before reporting:** currency, share class, splits (prices are adjusted), fiscal-year ends that differ between companies, a period too short for the indicator, a thinly traded symbol.
6. **Compare like with like:** same period, same benchmark, same currency.

## What you may and may not say

- Describe what the data shows: levels, changes, ratios, risk, and what an indicator's reading historically indicates. Indicators describe past prices; they do not forecast.
- Give scenarios and the assumptions behind them, not predictions. Do not tell a person to buy, sell or hold. This is analysis, not investment advice, and the report says so once, briefly.
- State the data source and the as-of date. Yahoo data can be delayed, adjusted or occasionally wrong; cross-check an unusual figure (for example with the company's filing) or flag it.
- Report a failure plainly: "Yahoo returned no income statement for X". Never fill a gap with a remembered number.

## The report

Write it to a workspace file and read it back: the question, the data used (symbols, period, as-of date, files in `data/`), the findings with their numbers, the caveats, and a short conclusion that follows from the numbers.
