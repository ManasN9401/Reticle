"""The quant agent's tools. The arithmetic is checked against values worked out independently;
the tools run against a stand-in for yfinance, so no network is needed.

pandas is a dependency of the quant-finance skill, not of the repository: without it these tests are
skipped (run them with `uv run --with pandas --with numpy python -m unittest tests.test_quant_tools`)."""
import math
import sys
import tempfile
import types
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cmd/forge/compiler/lib"))
import quant_tools as q

try:
    import numpy as np
    import pandas as pd
except ImportError:  # pragma: no cover - exercised by the skip
    pd = None


def bars(closes, start="2025-01-01", spread=1.0, volume=1000):
    index = pd.bdate_range(start, periods=len(closes))
    closes = pd.Series(closes, index=index, dtype=float)
    return pd.DataFrame({"Open": closes, "High": closes + spread, "Low": closes - spread, "Close": closes, "Volume": float(volume)})


@unittest.skipIf(pd is None, "pandas is not installed")
class IndicatorArithmetic(unittest.TestCase):
    def test_simple_and_exponential_averages(self):
        close = pd.Series([1.0, 2.0, 3.0, 4.0, 5.0])
        self.assertEqual(q.sma(close, 3).tolist()[2:], [2.0, 3.0, 4.0])
        self.assertTrue(math.isnan(q.sma(close, 3).iloc[1]))
        # EMA with span 3 has alpha 0.5: 1, 1.5, 2.25, 3.125, 4.0625
        self.assertEqual([round(v, 4) for v in q.ema(close, 3)], [1.0, 1.5, 2.25, 3.125, 4.0625])

    def test_rsi_at_the_extremes(self):
        up = pd.Series(np.arange(1.0, 40.0))
        down = pd.Series(np.arange(40.0, 1.0, -1.0))
        flat = pd.Series([10.0] * 40)
        self.assertEqual(q.rsi(up).iloc[-1], 100.0)
        self.assertEqual(q.rsi(down).iloc[-1], 0.0)
        self.assertEqual(q.rsi(flat).iloc[-1], 50.0)
        self.assertTrue(q.rsi(up).iloc[:13].isna().all(), "no value before a full window")

    def test_rsi_of_a_mixed_series_is_between_the_extremes(self):
        rng = np.random.default_rng(7)
        close = pd.Series(100 + rng.normal(0, 1, 200).cumsum())
        value = q.rsi(close).iloc[-1]
        self.assertTrue(0 < value < 100)

    def test_macd_histogram_is_line_minus_signal(self):
        close = pd.Series(np.sin(np.arange(120) / 6) * 10 + 100)
        line, signal, hist = q.macd(close)
        self.assertTrue(((line - signal) - hist).abs().max() < 1e-12)
        self.assertAlmostEqual(line.iloc[-1], q.ema(close, 12).iloc[-1] - q.ema(close, 26).iloc[-1], places=10)

    def test_bollinger_bands_bracket_the_mean_by_two_deviations(self):
        close = pd.Series([10.0, 12.0] * 20)
        lower, middle, upper = q.bollinger(close)
        self.assertAlmostEqual(middle.iloc[-1], 11.0)
        self.assertAlmostEqual(upper.iloc[-1] - middle.iloc[-1], 2.0 * 1.0)
        self.assertAlmostEqual(middle.iloc[-1] - lower.iloc[-1], 2.0)

    def test_atr_of_a_constant_range(self):
        frame = bars([100.0] * 40, spread=2.0)
        self.assertAlmostEqual(q.atr(frame).iloc[-1], 4.0)

    def test_risk_of_a_known_series(self):
        returns = pd.Series([0.10, -0.05, 0.02, 0.03, -0.10, 0.04])
        stats = q.risk(returns, periods_per_year=252)
        self.assertAlmostEqual(stats["total_return"], (1.1 * 0.95 * 1.02 * 1.03 * 0.9 * 1.04) - 1, places=12)
        curve = (1 + returns).cumprod()
        self.assertAlmostEqual(stats["max_drawdown"], (curve / curve.cummax() - 1).min(), places=12)
        self.assertAlmostEqual(stats["annual_volatility"], returns.std(ddof=1) * math.sqrt(252), places=12)
        self.assertEqual((stats["best_day"], stats["worst_day"]), (0.10, -0.10))

    def test_beta_of_a_series_against_itself_and_a_scaled_copy(self):
        rng = np.random.default_rng(1)
        market = pd.Series(rng.normal(0, 0.01, 300))
        self.assertAlmostEqual(q.risk(market, market)["beta"], 1.0, places=9)
        self.assertAlmostEqual(q.risk(market * 1.5, market)["beta"], 1.5, places=9)
        self.assertAlmostEqual(q.risk(-market, market)["correlation"], -1.0, places=9)

    def test_too_little_data_is_an_error_not_a_number(self):
        with self.assertRaises(q.QuantError):
            q.risk(pd.Series([0.01]))

    def test_readings_state_where_the_price_sits(self):
        frame = q.with_indicators(bars(list(np.linspace(50, 150, 260))), ["sma20", "sma50", "sma200", "rsi14", "macd", "bollinger", "atr14"])
        text = "\n".join(q.readings(frame))
        self.assertIn("SMA50", text)
        self.assertIn("above SMA200", text.replace("is above SMA200", "above SMA200"))
        self.assertIn("RSI14 100.0: overbought", text)
        self.assertIn("MACD", text)
        self.assertIn("Bollinger", text)
        self.assertIn("ATR14", text)


class FakeTicker:
    frames = {}
    statements = {}
    info = {"shortName": "Acme Inc", "trailingPE": 20.5, "forwardPE": 18.0, "sector": "Industrials", "dividendYield": 1.25}

    def __init__(self, symbol):
        self.symbol = symbol

    def history(self, period, interval, auto_adjust):
        if self.symbol not in FakeTicker.frames:
            raise RuntimeError("symbol may be delisted")
        frame = FakeTicker.frames[self.symbol]
        frame.index = frame.index.tz_localize("UTC")
        return frame

    @property
    def fast_info(self):
        if self.symbol not in FakeTicker.frames:
            raise KeyError("currentTradingPeriod")
        return {"lastPrice": 110.0, "previousClose": 100.0, "currency": "USD", "yearLow": 80.0, "yearHigh": 120.0, "lastVolume": 5_000_000, "marketCap": 2.5e11}

    def _statement(self, name):
        return FakeTicker.statements.get(name)

    income_stmt = property(lambda self: self._statement("income_stmt"))
    balance_sheet = property(lambda self: self._statement("balance_sheet"))
    cashflow = property(lambda self: self._statement("cashflow"))
    quarterly_income_stmt = property(lambda self: self._statement("quarterly_income_stmt"))


@unittest.skipIf(pd is None, "pandas is not installed")
class Tools(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        (Path(self.temp.name) / "src").mkdir()
        rng = np.random.default_rng(3)
        FakeTicker.frames = {
            "ACME": bars(list(100 + rng.normal(0, 1, 400).cumsum() + np.arange(400) * 0.05), start="2024-01-01", volume=2000),
            "^GSPC": bars(list(4000 + rng.normal(0, 10, 400).cumsum()), start="2024-01-01"),
            "OTHR": bars(list(50 + rng.normal(0, 0.5, 400).cumsum()), start="2024-01-01"),
        }
        dates = pd.to_datetime(["2023-12-31", "2024-12-31", "2025-12-31"])
        FakeTicker.statements = {
            "income_stmt": pd.DataFrame({dates[2]: [300.0, 120.0, 60.0, 45.0], dates[1]: [250.0, 100.0, 50.0, 40.0], dates[0]: [200.0, 80.0, 30.0, 25.0]},
                                        index=["Total Revenue", "Gross Profit", "Operating Income", "Net Income"]),
            "cashflow": pd.DataFrame({dates[2]: [70.0], dates[1]: [60.0], dates[0]: [50.0]}, index=["Operating Cash Flow"]),
        }
        module = types.SimpleNamespace(Ticker=FakeTicker, __version__="test")
        self.patch = patch.dict(sys.modules, {"yfinance": module})
        self.patch.start()
        self.addCleanup(self.patch.stop)

    def csv(self, name):
        return Path(self.temp.name, "src", "data", name)

    def test_quote_lists_each_symbol_and_survives_a_bad_one(self):
        reply = q.market_quote(["acme", "NOPE"])
        self.assertIn("ACME  Acme Inc", reply)
        self.assertIn("price 110.00 USD  day +10.00%", reply)
        self.assertIn("market cap 250.00B", reply)
        self.assertIn("dividend yield 1.25%", reply)
        self.assertIn("NOPE: no quote", reply)
        self.assertIn("not investment advice", reply)

    def test_price_history_summarises_and_saves_the_series(self):
        reply = q.price_history("ACME", self.temp.name, "1y", "1d")
        self.assertIn("ACME 1y/1d: 400 bars", reply)
        self.assertIn("saved: data/ACME-1y-1d.csv", reply)
        saved = pd.read_csv(self.csv("ACME-1y-1d.csv"), index_col=0)
        self.assertEqual(len(saved), 400)
        self.assertEqual(list(saved.columns), ["Open", "High", "Low", "Close", "Volume"])

    def test_statements_report_figures_margins_and_growth(self):
        reply = q.financial_statements("ACME", self.temp.name, ["income", "cashflow", "balance"])
        self.assertIn("Total Revenue: 200.00  250.00  300.00  (latest vs prior +20.00%)", reply)
        self.assertIn("gross margin: 40.0%", reply)
        self.assertIn("net margin: 15.0%", reply)
        self.assertIn("Operating Cash Flow: 50.00  60.00  70.00", reply)
        self.assertIn("balance: Yahoo has no annual balance statement", reply)
        self.assertTrue(self.csv("ACME-income-annual.csv").is_file())

    def test_indicators_are_computed_trimmed_to_the_period_and_saved(self):
        reply = q.technical_indicators("ACME", self.temp.name, "6mo", ["sma20", "rsi14"])
        self.assertIn("ACME indicators as of", reply)
        self.assertIn("(126 bars saved: data/ACME-indicators-6mo.csv)", reply)
        saved = pd.read_csv(self.csv("ACME-indicators-6mo.csv"), index_col=0)
        self.assertEqual(len(saved), 126)
        self.assertIn("SMA20", saved.columns)
        self.assertFalse(saved["SMA20"].isna().any(), "the average is warmed up on history from before the period")
        self.assertIn("RSI14", reply)

    def test_risk_includes_beta_and_a_correlation_matrix(self):
        reply = q.risk_metrics(["ACME", "OTHR"], self.temp.name, "1y", "^GSPC")
        self.assertIn("ACME: return", reply)
        self.assertIn("beta", reply)
        self.assertIn("correlation of daily returns", reply)
        self.assertTrue(any(p.name.startswith("risk-ACME-OTHR") for p in self.csv("x").parent.iterdir()))

    def test_bad_input_is_explained(self):
        self.assertIn("is not a ticker symbol", q.price_history("bad symbol!", self.temp.name))
        self.assertIn("period must be one of", q.price_history("ACME", self.temp.name, period="forever"))
        self.assertIn("interval must be one of", q.price_history("ACME", self.temp.name, interval="3s"))
        self.assertIn("at most 10 symbols", q.risk_metrics([f"S{n}" for n in range(11)], self.temp.name))
        self.assertIn("indicators must be drawn from", q.technical_indicators("ACME", self.temp.name, indicators=["magic"]))
        self.assertIn("statements must be a list", q.financial_statements("ACME", self.temp.name, ["profit"]))
        self.assertIn("Yahoo Finance did not return data for GONE: RuntimeError: symbol may be delisted", q.price_history("GONE", self.temp.name))

    def test_paths_cannot_escape_the_workspace(self):
        self.assertIn("is not a ticker symbol", q.price_history("../../etc", self.temp.name))


class WithoutThePackages(unittest.TestCase):
    def test_missing_packages_say_how_to_get_them(self):
        with patch.dict(sys.modules, {"yfinance": None}):
            reply = q.market_quote(["AAPL"])
        self.assertTrue(reply.startswith("Error: Market data needs the yfinance and pandas packages"))
        self.assertIn("quant-finance skill", reply)


if __name__ == "__main__":
    unittest.main()
