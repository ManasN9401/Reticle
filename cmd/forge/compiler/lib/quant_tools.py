"""Market-data tools for the quant agent: quotes, price history, financial statements, technical
indicators and risk metrics, from Yahoo Finance through the yfinance package.

The arithmetic is done here, in pandas, not by the model: a model is poor at arithmetic and a
number it computed in its head is not a result. Every tool that produces a table also saves it as a
CSV under data/ so the analysis can be checked, charted or continued with a script.

Yahoo's data can be delayed, adjusted or occasionally wrong, and it is licensed for personal use:
nothing here is investment advice, and the tool replies say so briefly.
"""
import json
import math
import re
from datetime import datetime, timezone

from forge_utils import safe_path

NOTE = "Source: Yahoo Finance via yfinance (may be delayed or adjusted; not investment advice)."
SYMBOL = re.compile(r"^[A-Za-z0-9.\-=^]{1,15}$")
PERIODS = ("1d", "5d", "1mo", "3mo", "6mo", "1y", "2y", "5y", "10y", "ytd", "max")
INTERVALS = ("1m", "2m", "5m", "15m", "30m", "60m", "90m", "1h", "1d", "5d", "1wk", "1mo", "3mo")
INDICATORS = ("sma20", "sma50", "sma200", "ema12", "ema26", "rsi14", "macd", "bollinger", "atr14")
MAX_SYMBOLS = 10
TRADING_DAYS = 252

NEEDS_PACKAGES = (
    "Market data needs the yfinance and pandas packages, which are not installed in this agent's environment. "
    "They come with the quant-finance skill; if the agent was started without it, install them with "
    "`pip install yfinance pandas`."
)


class QuantError(Exception):
    pass


def _modules():
    try:
        import pandas
        import yfinance
        return pandas, yfinance
    except ImportError as error:
        raise QuantError(NEEDS_PACKAGES + f" ({error})") from None


def _symbol(value):
    if not isinstance(value, str) or not SYMBOL.match(value.strip()):
        raise QuantError(f"{value!r} is not a ticker symbol (letters, digits and . - = ^ only, up to 15 characters)")
    return value.strip().upper()


def _symbols(value):
    items = [value] if isinstance(value, str) else value
    if not isinstance(items, list) or not items:
        raise QuantError("symbols must be a non-empty list of ticker symbols")
    if len(items) > MAX_SYMBOLS:
        raise QuantError(f"at most {MAX_SYMBOLS} symbols per call")
    return list(dict.fromkeys(_symbol(item) for item in items))


def _choice(value, allowed, name, default):
    value = default if value is None else value
    if value not in allowed:
        raise QuantError(f"{name} must be one of {', '.join(allowed)}")
    return value


def history(symbol, period="1y", interval="1d"):
    """Adjusted OHLCV bars as a DataFrame indexed by date, or QuantError."""
    pandas, yfinance = _modules()
    try:
        frame = yfinance.Ticker(symbol).history(period=period, interval=interval, auto_adjust=True)
    except Exception as error:  # noqa: BLE001 - yfinance raises assorted network and parsing errors
        raise QuantError(f"Yahoo Finance did not return data for {symbol}: {type(error).__name__}: {str(error)[:160]}") from None
    if frame is None or len(frame) == 0:
        raise QuantError(f"no price data for {symbol} over {period}/{interval}; check the symbol (exchange suffixes such as .L or .TO) and the period")
    frame = frame.copy()
    try:
        frame.index = frame.index.tz_localize(None)
    except (TypeError, AttributeError):
        pass
    return frame[[c for c in ("Open", "High", "Low", "Close", "Volume") if c in frame.columns]]


def _num(value, digits=2):
    try:
        value = float(value)
    except (TypeError, ValueError):
        return "n/a"
    if math.isnan(value) or math.isinf(value):
        return "n/a"
    if abs(value) >= 1e12:
        return f"{value / 1e12:.2f}T"
    if abs(value) >= 1e9:
        return f"{value / 1e9:.2f}B"
    if abs(value) >= 1e7:
        return f"{value / 1e6:.1f}M"
    return f"{value:,.{digits}f}"


def _pct(value):
    try:
        value = float(value)
    except (TypeError, ValueError):
        return "n/a"
    return "n/a" if math.isnan(value) or math.isinf(value) else f"{value * 100:+.2f}%"


def _save(workspace_dir, relative, frame):
    target = safe_path(workspace_dir, relative)
    target.parent.mkdir(parents=True, exist_ok=True)
    frame.to_csv(target, encoding="utf-8", lineterminator="\n")
    return relative


# ---------------------------------------------------------------------------------------------
# Indicator arithmetic (pure pandas; unit-tested without a network)
# ---------------------------------------------------------------------------------------------

def sma(close, window):
    return close.rolling(window).mean()


def ema(close, span):
    return close.ewm(span=span, adjust=False).mean()


def rsi(close, window=14):
    """Wilder's RSI. A series with no losses is 100, one with no gains 0, a flat one 50."""
    change = close.diff()
    gain = change.clip(lower=0).ewm(alpha=1 / window, adjust=False, min_periods=window).mean()
    loss = (-change.clip(upper=0)).ewm(alpha=1 / window, adjust=False, min_periods=window).mean()
    strength = gain / loss
    out = 100 - 100 / (1 + strength)
    out = out.where(~((loss == 0) & (gain > 0)), 100.0)
    out = out.where(~((loss == 0) & (gain == 0) & gain.notna()), 50.0)
    return out


def macd(close, fast=12, slow=26, signal=9):
    line = ema(close, fast) - ema(close, slow)
    signal_line = line.ewm(span=signal, adjust=False).mean()
    return line, signal_line, line - signal_line


def bollinger(close, window=20, width=2.0):
    middle = close.rolling(window).mean()
    spread = close.rolling(window).std(ddof=0) * width
    return middle - spread, middle, middle + spread


def atr(frame, window=14):
    previous = frame["Close"].shift()
    ranges = (frame["High"] - frame["Low"]).to_frame("a")
    ranges["b"] = (frame["High"] - previous).abs()
    ranges["c"] = (frame["Low"] - previous).abs()
    return ranges.max(axis=1).ewm(alpha=1 / window, adjust=False, min_periods=window).mean()


def with_indicators(frame, wanted):
    out = frame.copy()
    close = out["Close"]
    for name in wanted:
        if name.startswith("sma"):
            out[name.upper()] = sma(close, int(name[3:]))
        elif name.startswith("ema"):
            out[name.upper()] = ema(close, int(name[3:]))
        elif name == "rsi14":
            out["RSI14"] = rsi(close, 14)
        elif name == "macd":
            out["MACD"], out["MACD_SIGNAL"], out["MACD_HIST"] = macd(close)
        elif name == "bollinger":
            out["BB_LOWER"], out["BB_MIDDLE"], out["BB_UPPER"] = bollinger(close)
        elif name == "atr14":
            out["ATR14"] = atr(out, 14)
    return out


def readings(frame):
    """Plain statements about the latest bar, each with the numbers behind it."""
    last = frame.iloc[-1]
    price = last["Close"]
    notes = []

    def have(*names):
        return all(name in frame.columns and not math.isnan(float(last[name])) for name in names)
    if have("SMA50", "SMA200"):
        state = "above" if last["SMA50"] > last["SMA200"] else "below"
        notes.append(f"SMA50 {_num(last['SMA50'])} is {state} SMA200 {_num(last['SMA200'])} ({'golden-cross regime' if state == 'above' else 'death-cross regime'}, a lagging signal)")
    for name in ("SMA20", "SMA50", "SMA200"):
        if have(name):
            notes.append(f"price {_num(price)} is {'above' if price > last[name] else 'below'} {name} {_num(last[name])} ({_pct(price / last[name] - 1)})")
    if have("RSI14"):
        value = float(last["RSI14"])
        zone = "overbought (>70)" if value > 70 else "oversold (<30)" if value < 30 else "neutral (30-70)"
        notes.append(f"RSI14 {value:.1f}: {zone}")
    if have("MACD", "MACD_SIGNAL"):
        notes.append(f"MACD {_num(last['MACD'], 3)} is {'above' if last['MACD'] > last['MACD_SIGNAL'] else 'below'} its signal {_num(last['MACD_SIGNAL'], 3)}")
    if have("BB_LOWER", "BB_UPPER"):
        span = last["BB_UPPER"] - last["BB_LOWER"]
        position = (price - last["BB_LOWER"]) / span if span else float("nan")
        notes.append(f"price sits at {position * 100:.0f}% of the Bollinger band (0% lower band, 100% upper band)")
    if have("ATR14"):
        notes.append(f"ATR14 {_num(last['ATR14'])} ({last['ATR14'] / price * 100:.1f}% of price) is the typical daily range")
    return notes


def risk(returns, benchmark_returns=None, periods_per_year=TRADING_DAYS, risk_free=0.0):
    """Annualised return and volatility, Sharpe ratio, maximum drawdown, and beta against a benchmark."""
    clean = returns.dropna()
    if len(clean) < 2:
        raise QuantError("too few observations to measure risk")
    growth = (1 + clean).prod()
    years = len(clean) / periods_per_year
    annual_return = growth ** (1 / years) - 1 if years > 0 and growth > 0 else float("nan")
    volatility = clean.std(ddof=1) * math.sqrt(periods_per_year)
    curve = (1 + clean).cumprod()
    drawdown = (curve / curve.cummax() - 1).min()
    result = {
        "observations": int(len(clean)), "total_return": growth - 1, "annual_return": annual_return,
        "annual_volatility": volatility, "sharpe": (annual_return - risk_free) / volatility if volatility else float("nan"),
        "max_drawdown": drawdown, "worst_day": clean.min(), "best_day": clean.max(),
    }
    if benchmark_returns is not None:
        joined = clean.to_frame("a").join(benchmark_returns.dropna().to_frame("b"), how="inner")
        if len(joined) > 2 and joined["b"].var() > 0:
            result["beta"] = joined["a"].cov(joined["b"]) / joined["b"].var()
            result["correlation"] = joined["a"].corr(joined["b"])
    return result


# ---------------------------------------------------------------------------------------------
# The tools
# ---------------------------------------------------------------------------------------------

def market_quote(symbols, workspace_dir=None):
    try:
        pandas, yfinance = _modules()
        rows = []
        for symbol in _symbols(symbols):
            try:
                ticker = yfinance.Ticker(symbol)
                fast = ticker.fast_info
                price, previous = fast["lastPrice"], fast["previousClose"]
                try:
                    info = ticker.info or {}
                except Exception:  # noqa: BLE001 - .info is the flaky endpoint; the quote stands without it
                    info = {}
                rows.append(
                    f"{symbol}  {info.get('shortName') or info.get('longName') or ''}\n"
                    f"  price {_num(price)} {fast['currency']}  day {_pct(price / previous - 1 if previous else float('nan'))}  "
                    f"52w {_num(fast['yearLow'])}-{_num(fast['yearHigh'])}  volume {_num(fast['lastVolume'], 0)}\n"
                    f"  market cap {_num(fast['marketCap'])}  P/E (trailing) {_num(info.get('trailingPE'))}  "
                    f"forward P/E {_num(info.get('forwardPE'))}  dividend yield {(_num(info.get('dividendYield')) + '%') if info.get('dividendYield') is not None else 'n/a'}  "
                    f"sector {info.get('sector') or 'n/a'}")
            except Exception as error:  # noqa: BLE001 - one bad symbol must not lose the others
                rows.append(f"{symbol}: no quote ({type(error).__name__}: {str(error)[:120]})")
        return "\n".join(rows) + "\n" + NOTE
    except QuantError as error:
        return f"Error: {error}"


def price_history(symbol, workspace_dir, period="1y", interval="1d"):
    try:
        symbol = _symbol(symbol)
        period = _choice(period, PERIODS, "period", "1y")
        interval = _choice(interval, INTERVALS, "interval", "1d")
        frame = history(symbol, period, interval)
        path = _save(workspace_dir, f"data/{symbol}-{period}-{interval}.csv", frame)
        close = frame["Close"]
        change = close.iloc[-1] / close.iloc[0] - 1
        high, low = frame["High"].max(), frame["Low"].min()
        return (
            f"{symbol} {period}/{interval}: {len(frame)} bars, {frame.index[0]:%Y-%m-%d} to {frame.index[-1]:%Y-%m-%d}\n"
            f"  first close {_num(close.iloc[0])}, last close {_num(close.iloc[-1])} ({_pct(change)})\n"
            f"  period high {_num(high)} on {frame['High'].idxmax():%Y-%m-%d}, low {_num(low)} on {frame['Low'].idxmin():%Y-%m-%d}\n"
            f"  average volume {_num(frame['Volume'].mean(), 0)}\n"
            f"  last 3 bars:\n" + "\n".join(f"    {index:%Y-%m-%d}  O {_num(row.Open)}  H {_num(row.High)}  L {_num(row.Low)}  C {_num(row.Close)}  V {_num(row.Volume, 0)}" for index, row in frame.tail(3).iterrows())
            + f"\n  saved: {path} (adjusted for splits and dividends)\n" + NOTE)
    except QuantError as error:
        return f"Error: {error}"


_LINES = {
    "income": ["Total Revenue", "Gross Profit", "Operating Income", "EBITDA", "Net Income", "Diluted EPS"],
    "balance": ["Total Assets", "Total Liabilities Net Minority Interest", "Stockholders Equity", "Cash And Cash Equivalents", "Total Debt"],
    "cashflow": ["Operating Cash Flow", "Capital Expenditure", "Free Cash Flow", "Repurchase Of Capital Stock", "Cash Dividends Paid"],
}


def financial_statements(symbol, workspace_dir, statements=None, frequency="annual"):
    try:
        symbol = _symbol(symbol)
        wanted = statements or ["income", "balance", "cashflow"]
        if isinstance(wanted, str):
            wanted = [wanted]
        if not isinstance(wanted, list) or any(w not in _LINES for w in wanted):
            raise QuantError("statements must be a list drawn from income, balance, cashflow")
        frequency = _choice(frequency, ("annual", "quarterly"), "frequency", "annual")
        pandas, yfinance = _modules()
        ticker = yfinance.Ticker(symbol)
        out = [f"{symbol} {frequency} statements:"]
        for kind in wanted:
            attribute = {"income": "income_stmt", "balance": "balance_sheet", "cashflow": "cashflow"}[kind]
            if frequency == "quarterly":
                attribute = "quarterly_" + attribute
            try:
                table = getattr(ticker, attribute)
            except Exception as error:  # noqa: BLE001
                out.append(f"  {kind}: unavailable ({type(error).__name__}: {str(error)[:100]})")
                continue
            if table is None or len(table) == 0:
                out.append(f"  {kind}: Yahoo has no {frequency} {kind} statement for {symbol}")
                continue
            table = table.sort_index(axis=1)
            path = _save(workspace_dir, f"data/{symbol}-{kind}-{frequency}.csv", table.T)
            columns = list(table.columns)
            out.append(f"  {kind} ({', '.join(f'{c:%Y-%m-%d}' for c in columns[-4:])}) saved: {path}")
            for line in _LINES[kind]:
                if line in table.index:
                    values = [table.loc[line, c] for c in columns[-4:]]
                    growth = ""
                    if len(values) >= 2 and values[-2] not in (0, None) and not any(isinstance(v, float) and math.isnan(v) for v in values[-2:]):
                        growth = f"  (latest vs prior {_pct(values[-1] / values[-2] - 1)})"
                    out.append(f"    {line}: " + "  ".join(_num(v) for v in values) + growth)
            if kind == "income" and "Total Revenue" in table.index:
                latest = columns[-1]
                revenue = table.loc["Total Revenue", latest]
                for line, label in (("Gross Profit", "gross margin"), ("Operating Income", "operating margin"), ("Net Income", "net margin")):
                    if line in table.index and revenue:
                        out.append(f"    {label}: {table.loc[line, latest] / revenue * 100:.1f}%")
        return "\n".join(out) + "\n" + NOTE
    except QuantError as error:
        return f"Error: {error}"


def technical_indicators(symbol, workspace_dir, period="1y", indicators=None):
    try:
        symbol = _symbol(symbol)
        period = _choice(period, PERIODS, "period", "1y")
        wanted = indicators or ["sma20", "sma50", "sma200", "rsi14", "macd", "bollinger", "atr14"]
        if isinstance(wanted, str):
            wanted = [wanted]
        if not isinstance(wanted, list) or any(w not in INDICATORS for w in wanted):
            raise QuantError(f"indicators must be drawn from {', '.join(INDICATORS)}")
        # Long averages need history before the period starts; fetch extra and trim afterwards.
        needed = max([int(w[3:]) for w in wanted if w.startswith(("sma", "ema"))] + [35])
        fetch = "5y" if period in ("1y", "2y", "ytd", "max") or needed > 120 else "2y"
        frame = history(symbol, fetch if period != "max" else "max", "1d")
        enriched = with_indicators(frame, wanted)
        keep = {"1d": 1, "5d": 5, "1mo": 21, "3mo": 63, "6mo": 126, "1y": 252, "2y": 504, "5y": 1260, "10y": 2520}.get(period)
        enriched = enriched.iloc[-keep:] if keep else enriched
        path = _save(workspace_dir, f"data/{symbol}-indicators-{period}.csv", enriched)
        last = enriched.iloc[-1]
        lines = [f"{symbol} indicators as of {enriched.index[-1]:%Y-%m-%d}, close {_num(last['Close'])} ({len(enriched)} bars saved: {path})"]
        lines += ["  " + note for note in readings(enriched)]
        if not readings(enriched):
            lines.append("  not enough history for the requested indicators")
        return "\n".join(lines) + "\n  These are descriptions of past prices, not forecasts.\n" + NOTE
    except QuantError as error:
        return f"Error: {error}"


def risk_metrics(symbols, workspace_dir, period="1y", benchmark="^GSPC"):
    try:
        names = _symbols(symbols)
        period = _choice(period, PERIODS, "period", "1y")
        benchmark = _symbol(benchmark) if benchmark else None
        pandas, _ = _modules()
        bench_returns = history(benchmark, period, "1d")["Close"].pct_change() if benchmark else None
        rows, closes = [], {}
        for name in names:
            frame = history(name, period, "1d")
            closes[name] = frame["Close"]
            stats = risk(frame["Close"].pct_change(), bench_returns if benchmark != name else None)
            rows.append((name, stats))
        table = pandas.DataFrame({name: stats for name, stats in rows}).T
        path = _save(workspace_dir, f"data/risk-{'-'.join(names)[:60]}-{period}.csv", table)
        out = [f"Risk over {period} (daily data, risk-free rate 0, vs {benchmark or 'no benchmark'}); saved: {path}"]
        for name, stats in rows:
            line = (f"  {name}: return {_pct(stats['total_return'])} (annualised {_pct(stats['annual_return'])}), volatility {_pct(stats['annual_volatility']).lstrip('+')}, "
                    f"Sharpe {stats['sharpe']:.2f}, max drawdown {_pct(stats['max_drawdown'])}, best day {_pct(stats['best_day'])}, worst day {_pct(stats['worst_day'])}")
            if "beta" in stats:
                line += f", beta {stats['beta']:.2f}, correlation {stats['correlation']:.2f}"
            out.append(line)
        if len(names) > 1:
            correlation = pandas.DataFrame(closes).pct_change().corr()
            out.append("  correlation of daily returns:")
            out += ["    " + name + "  " + "  ".join(f"{correlation.loc[name, other]:5.2f}" for other in names) for name in names]
            out.append("    columns: " + ", ".join(names))
        return "\n".join(out) + "\n" + NOTE
    except QuantError as error:
        return f"Error: {error}"
