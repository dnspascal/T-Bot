import pandas as pd
import numpy as np
import pandas_ta as ta

# ── Load & split ──────────────────────────────────────────────────────────────

def load_symbol(path):
    df = pd.read_csv(path)
    df['datetime'] = pd.to_datetime(df['open_time'], unit='s')
    df = df.sort_values('datetime').reset_index(drop=True)
    m5  = df[df['period'] == 'M5'].copy().reset_index(drop=True)
    m15 = df[df['period'] == 'M15'].copy().reset_index(drop=True)
    h1  = df[df['period'] == 'H1'].copy().reset_index(drop=True)
    return m5, m15, h1

# ── Indicators ────────────────────────────────────────────────────────────────

def compute_features(m5, m15, h1):
    m5['rsi']     = ta.rsi(m5['close'], length=14)
    m5['rsi_vel'] = m5['rsi'].diff()           
    m5['ema50']   = ta.ema(m5['close'], length=50)
    m5['ema200']  = ta.ema(m5['close'], length=200)
    m5['atr']     = ta.atr(m5['high'], m5['low'], m5['close'], length=14)
    m5['hour']    = m5['datetime'].dt.hour
    m5['above_ema50']  = (m5['close'] > m5['ema50']).astype(float)
    m5['above_ema200'] = (m5['close'] > m5['ema200']).astype(float)

    m15['rsi_m15'] = ta.rsi(m15['close'], length=14)
    h1['rsi_h1']   = ta.rsi(h1['close'], length=14)

    # Join higher-TF RSI to M5 by most recent prior bar
    m5 = pd.merge_asof(
        m5.sort_values('datetime'),
        m15[['datetime', 'rsi_m15']].sort_values('datetime'),
        on='datetime', direction='backward'
    )
    m5 = pd.merge_asof(
        m5.sort_values('datetime'),
        h1[['datetime', 'rsi_h1']].sort_values('datetime'),
        on='datetime', direction='backward'
    )
    return m5.reset_index(drop=True)

# ── Label: did price hit 1.67R TP before 1R SL within 20 bars? ──────────────

def label_reversals(m5, lookahead=20, sl_atr_mult=1.0, rr=1.67, rsi_high=68, rsi_low=32):
    close = m5['close'].values
    high  = m5['high'].values
    low   = m5['low'].values
    rsi   = m5['rsi'].values
    atr   = m5['atr'].values

    labels  = np.full(len(m5), np.nan)
    signals = np.full(len(m5), '', dtype=object)

    for i in range(len(m5)):
        r = rsi[i]
        a = atr[i]
        if np.isnan(r) or np.isnan(a) or a == 0:
            continue

        if r > rsi_high:
            signal = 'SELL'
            sl = close[i] + sl_atr_mult * a
            tp = close[i] - rr * sl_atr_mult * a
        elif r < rsi_low:
            signal = 'BUY'
            sl = close[i] - sl_atr_mult * a
            tp = close[i] + rr * sl_atr_mult * a
        else:
            continue

        hit_tp = False
        for j in range(i + 1, min(i + 1 + lookahead, len(m5))):
            if signal == 'SELL':
                if low[j]  <= tp: hit_tp = True; break
                if high[j] >= sl: break
            else:
                if high[j] >= tp: hit_tp = True; break
                if low[j]  <= sl: break

        labels[i]  = 1 if hit_tp else 0
        signals[i] = signal

    m5['label']  = labels
    m5['signal'] = signals
    return m5

# ── Feature matrix for XGBoost ───────────────────────────────────────────────

FEATURES = [
    'rsi', 'rsi_vel', 'rsi_m15', 'rsi_h1',
    'atr', 'above_ema50', 'above_ema200', 'hour',
]

def build_dataset(m5, symbol_id):
    # Only rows where we have a label (RSI was extreme)
    df = m5[m5['label'].notna()].copy()
    df['symbol'] = symbol_id
    df['is_sell'] = (df['signal'] == 'SELL').astype(float)
    features = FEATURES + ['symbol', 'is_sell']
    df = df.dropna(subset=features + ['label'])
    X = df[features].values
    y = df['label'].values.astype(int)
    return X, y, df

# ── Train ─────────────────────────────────────────────────────────────────────

def train_symbol(path, symbol_name, symbol_id, rsi_high=68, rsi_low=32):
    print(f"\n=== {symbol_name} (RSI thresholds: >{rsi_high} / <{rsi_low}) ===")
    m5, m15, h1 = load_symbol(path)
    m5 = compute_features(m5, m15, h1)
    m5 = label_reversals(m5, rsi_high=rsi_high, rsi_low=rsi_low)

    X, y, df = build_dataset(m5, symbol_id)
    print(f"  Candidate entries: {len(df)}")
    print(f"  Win rate in data: {y.mean():.1%}")
    print(f"  BUY / SELL: {(df['signal']=='BUY').sum()} / {(df['signal']=='SELL').sum()}")

    from xgboost import XGBClassifier
    from sklearn.model_selection import train_test_split
    from sklearn.metrics import classification_report

    X_train, X_test, y_train, y_test = train_test_split(
        X, y, test_size=0.2, shuffle=False  # time-ordered split
    )

    model = XGBClassifier(
        n_estimators=300,
        max_depth=4,
        learning_rate=0.05,
        subsample=0.8,
        colsample_bytree=0.8,
        scale_pos_weight=(y == 0).sum() / (y == 1).sum(),  # handle class imbalance
        eval_metric='logloss',
        random_state=42,
    )
    model.fit(X_train, y_train, eval_set=[(X_test, y_test)], verbose=False)

    y_pred = model.predict(X_test)
    print(classification_report(y_test, y_pred, target_names=['loss', 'win']))

    # Find threshold where win precision >= 60%
    proba = model.predict_proba(X_test)[:, 1]
    print("  Threshold tuning (win precision / win recall / trades taken):")
    for thresh in [0.50, 0.55, 0.60, 0.65, 0.70, 0.75]:
        pred = (proba >= thresh).astype(int)
        taken = pred.sum()
        if taken == 0:
            print(f"    {thresh:.2f}: no trades")
            continue
        tp = ((pred == 1) & (y_test == 1)).sum()
        prec = tp / taken
        recall = tp / (y_test == 1).sum()
        print(f"    {thresh:.2f}: precision={prec:.1%}  recall={recall:.1%}  trades={taken}/{len(y_test)}")

    # Feature importance
    import matplotlib
    matplotlib.use('Agg')
    import matplotlib.pyplot as plt
    fi = pd.Series(model.feature_importances_, index=FEATURES + ['symbol', 'is_sell'])
    fi.sort_values().plot(kind='barh', title=f'{symbol_name} feature importance')
    plt.tight_layout()
    plt.savefig(f'ml/{symbol_name}_importance.png')
    print(f"  Saved ml/{symbol_name}_importance.png")

    return model

from onnxmltools import convert_xgboost
from onnxmltools.convert.common.data_types import FloatTensorType
import onnx

def export_onnx(model, name, n_features):
    initial_type = [('float_input', FloatTensorType([None, n_features]))]
    onnx_model = convert_xgboost(model, initial_types=initial_type)
    path = f'ml/{name}_model.onnx'
    onnx.save_model(onnx_model, path)
    print(f"  Exported {path}")

eurusd_model = train_symbol("ml/EURUSD.csv", "EURUSD", 0)
export_onnx(eurusd_model, "eurusd", len(FEATURES) + 2)

xauusd_model = train_symbol("ml/XAUUSD.csv", "XAUUSD", 1, rsi_high=75, rsi_low=25)
export_onnx(xauusd_model, "xauusd", len(FEATURES) + 2)
