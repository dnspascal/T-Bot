package ml

import (
	"fmt"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// Feature order must match train.py FEATURES + ['symbol', 'is_sell']
// [rsi, rsi_vel, rsi_m15, rsi_h1, atr, above_ema50, above_ema200, hour, symbol, is_sell]
const numFeatures = 10

type Predictor struct {
	mu          sync.Mutex
	session     *ort.AdvancedSession
	inputTensor *ort.Tensor[float32]
	probTensor  *ort.Tensor[float32]
	labelTensor *ort.Tensor[int64]
}

type Features struct {
	RSI         float32
	RSIVel      float32
	RSIM15      float32
	RSIH1       float32
	ATR         float32
	AboveEMA50  float32 // 1.0 or 0.0
	AboveEMA200 float32
	Hour        float32 // 0–23
	Symbol      float32 // 0=EURUSD, 1=XAUUSD
	IsSell      float32 // 1=SELL, 0=BUY
}

func NewPredictor(modelPath string) (*Predictor, error) {
	inputTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(1, numFeatures))
	if err != nil {
		return nil, fmt.Errorf("ml: input tensor: %w", err)
	}

	labelTensor, err := ort.NewEmptyTensor[int64](ort.NewShape(1))
	if err != nil {
		inputTensor.Destroy()
		return nil, fmt.Errorf("ml: label tensor: %w", err)
	}

	probTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 2))
	if err != nil {
		inputTensor.Destroy()
		labelTensor.Destroy()
		return nil, fmt.Errorf("ml: prob tensor: %w", err)
	}

	session, err := ort.NewAdvancedSession(modelPath,
		[]string{"float_input"},
		[]string{"label", "probabilities"},
		[]ort.ArbitraryTensor{inputTensor},
		[]ort.ArbitraryTensor{labelTensor, probTensor},
		nil,
	)
	if err != nil {
		inputTensor.Destroy()
		labelTensor.Destroy()
		probTensor.Destroy()
		return nil, fmt.Errorf("ml: session %s: %w", modelPath, err)
	}

	return &Predictor{
		session:     session,
		inputTensor: inputTensor,
		probTensor:  probTensor,
		labelTensor: labelTensor,
	}, nil
}

// Predict returns probability [0,1] that this entry will be a winner.
func (p *Predictor) Predict(f Features) (float32, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	data := p.inputTensor.GetData()
	data[0] = f.RSI
	data[1] = f.RSIVel
	data[2] = f.RSIM15
	data[3] = f.RSIH1
	data[4] = f.ATR
	data[5] = f.AboveEMA50
	data[6] = f.AboveEMA200
	data[7] = f.Hour
	data[8] = f.Symbol
	data[9] = f.IsSell

	if err := p.session.Run(); err != nil {
		return 0, fmt.Errorf("ml: run: %w", err)
	}

	// probabilities shape [1, 2]: index 0 = p(loss), index 1 = p(win)
	probs := p.probTensor.GetData()
	if len(probs) < 2 {
		return 0, fmt.Errorf("ml: expected 2 probabilities, got %d", len(probs))
	}
	return probs[1], nil
}

func (p *Predictor) Close() {
	if p.session != nil {
		p.session.Destroy()
	}
	if p.inputTensor != nil {
		p.inputTensor.Destroy()
	}
	if p.labelTensor != nil {
		p.labelTensor.Destroy()
	}
	if p.probTensor != nil {
		p.probTensor.Destroy()
	}
}
