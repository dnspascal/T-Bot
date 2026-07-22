# t-bot

Algorithmic trading bot for forex and commodities via cTrader.

## Stack

- **Go** — core bot, order execution, risk management
- **PostgreSQL + TimescaleDB** — tick and candle storage
- **XGBoost / ONNX** — ML signal filter, trained on 13 months of M5/M15/H1 data
- **Telegram** — trade notifications

## Strategies

| Name | Description |
|------|-------------|
| `sr_bounce` | RSI extreme at M15 S/R level, ML-filtered via XGBoost ONNX model |
| `trend_follow` | EMA + ADX trend continuation |
| `combined` | Runs both; first signal wins |

## Configuration

Copy `.env.example` and fill in credentials. Key vars:

```
STRATEGY=combined
CTRADER_SYMBOL=EURUSD
ML_MODEL_DIR=/path/to/models   # directory containing eurusd_model.onnx / xauusd_model.onnx
```

## ML Model

Train locally, deploy as ONNX:

```bash
python3 ml/train.py          # outputs ml/eurusd_model.onnx, ml/xauusd_model.onnx
scp ml/*.onnx server:~/models/
```

## Run

```bash
make migrate-up
make build
make run
```
