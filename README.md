Trading bot

## Tech Stack

- **Language:** Go
- **Database:** PostgreSQL
- **Migrations:** golang-migrate
- **Transport:** gRPC (protobuf)

## Strategy

The bot uses a combined technical analysis strategy on M5 candles:

- **EMA Crossover** — fast EMA (9) vs slow EMA (21) generates the primary entry signal
- **RSI Filter** — RSI (14) filters out exhausted moves; trades are skipped when RSI is overbought or oversold
- **Confluence** — signals are graded weak or strong depending on whether RSI confirms the EMA direction
- **Session Gate** — trades are only considered during active market hours (08:00–17:00 UTC, weekdays)

## Usage

```bash
# Run database migrations
make migrate-up

# Build the bot
make build

# Run the bot
make run
```
