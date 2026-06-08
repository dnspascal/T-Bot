package api

import (
	"encoding/binary"
	"math"
	"time"
)

func appendVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func appendTag(b []byte, field int, wireType int) []byte {
	return appendVarint(b, uint64(field<<3|wireType))
}

func appendString(b []byte, field int, s string) []byte {
	b = appendTag(b, field, 2)
	b = appendVarint(b, uint64(len(s)))
	return append(b, s...)
}

func appendBytes(b []byte, field int, data []byte) []byte {
	b = appendTag(b, field, 2)
	b = appendVarint(b, uint64(len(data)))
	return append(b, data...)
}

func appendInt64(b []byte, field int, v int64) []byte {
	b = appendTag(b, field, 0)
	return appendVarint(b, uint64(v))
}

func appendUint32(b []byte, field int, v uint32) []byte {
	b = appendTag(b, field, 0)
	return appendVarint(b, uint64(v))
}


func encodeEnvelope(payloadType uint32, inner []byte) []byte {
	var b []byte
	b = appendUint32(b, 1, payloadType)
	if len(inner) > 0 {
		b = appendBytes(b, 2, inner)
	}
	return b
}


func encodeAppAuthReq(clientID, clientSecret string) []byte {
	var b []byte
	b = appendUint32(b, 1, ProtoOAApplicationAuthReq)
	b = appendString(b, 2, clientID)
	b = appendString(b, 3, clientSecret)
	return b
}

func encodeAccountAuthReq(accountID int64, accessToken string) []byte {
	var b []byte
	b = appendUint32(b, 1, ProtoOAAccountAuthReq)
	b = appendInt64(b, 2, accountID)
	b = appendString(b, 3, accessToken)
	return b
}

func encodeSubscribeSpotsReq(accountID, symbolID int64) []byte {
	var b []byte
	b = appendUint32(b, 1, ProtoOASubscribeSpotsReq)
	b = appendInt64(b, 2, accountID)
	b = appendInt64(b, 3, symbolID)
	return b
}

func encodeClosePositionReq(accountID, positionID, volume int64) []byte {
	var b []byte
	b = appendUint32(b, 1, ProtoOAClosePositionReq)
	b = appendInt64(b, 2, accountID)
	b = appendInt64(b, 3, positionID)
	b = appendInt64(b, 4, volume)
	return b
}

func encodeNewOrderReq(accountID, symbolID int64, side uint32, volume int64, sl, tp float64) []byte {
	var b []byte
	b = appendUint32(b, 1, ProtoOANewOrderReq)
	b = appendInt64(b, 2, accountID)
	b = appendInt64(b, 3, symbolID)
	b = appendUint32(b, 4, 1) 
	b = appendUint32(b, 5, side)
	b = appendInt64(b, 6, volume)

	if sl > 0 {
		b = appendInt64(b, 19, int64(sl*10)) 
	}
	if tp > 0 {
		b = appendInt64(b, 20, int64(tp*10)) 
	}
	return b
}


type TraderInfo struct {
	AccountID     int64
	Balance       float64 
	Leverage      float64 
	MaxLeverage   float64 
	AccountMode   string  
	BrokerName    string
	IsLimitedRisk bool 
	FairStopOut   bool 
}

type OpenPosition struct {
	PositionID int64
	SymbolID   int64
	Side       uint32 
	Volume     int64
}


func encodeTraderReq(accountID int64) []byte {
	var b []byte
	b = appendUint32(b, 1, ProtoOATraderReq)
	b = appendInt64(b, 2, accountID)
	return b
}

func encodeReconcileReq(accountID int64) []byte {
	var b []byte
	b = appendUint32(b, 1, ProtoOAReconcileReq)
	b = appendInt64(b, 2, accountID)
	return b
}


func encodeSubscribeLiveTrendbarReq(accountID, symbolID int64, period uint32) []byte {
	var b []byte
	b = appendUint32(b, 1, ProtoOASubscribeLiveTrendbarReq)
	b = appendInt64(b, 2, accountID)
	b = appendUint32(b, 3, period)
	b = appendInt64(b, 4, symbolID)
	return b
}

func encodeGetTrendbarsReq(accountID, symbolID int64, period uint32, toMs int64, count uint32) []byte {
	var b []byte
	b = appendUint32(b, 1, ProtoOAGetTrendbarsReq)
	b = appendInt64(b, 2, accountID)
	b = appendInt64(b, 4, toMs)
	b = appendUint32(b, 5, period)
	b = appendInt64(b, 6, symbolID)
	b = appendUint32(b, 7, count)
	return b
}


type Trendbar struct {
	OpenTime int64  
	Period   uint32 
	Open     float64
	High     float64
	Low      float64
	Close    float64
	Volume   int64
}


func decodeTrendbar(data []byte) (Trendbar, bool) {
	const divisor = 100000.0
	var (
		low        uint64
		deltaOpen  uint64
		deltaHigh  uint64
		deltaClose uint64
		volume     uint64
		tsMinutes  uint64
		period     uint64
	)
	i := 0
	for i < len(data) {
		tag, n := decodeVarint(data[i:])
		if n == 0 {
			break
		}
		i += n
		field := tag >> 3
		wire := tag & 0x7
		switch {
		case field == 3 && wire == 0: 
			v, n2 := decodeVarint(data[i:])
			i += n2
			volume = v
		case field == 4 && wire == 0: 
			v, n2 := decodeVarint(data[i:])
			i += n2
			period = v
		case field == 5 && wire == 0: 
			v, n2 := decodeVarint(data[i:])
			i += n2
			low = v
		case field == 6 && wire == 0: 
			v, n2 := decodeVarint(data[i:])
			i += n2
			deltaOpen = v
		case field == 7 && wire == 0: 
			v, n2 := decodeVarint(data[i:])
			i += n2
			deltaClose = v
		case field == 8 && wire == 0: 
			v, n2 := decodeVarint(data[i:])
			i += n2
			deltaHigh = v
		case field == 9 && wire == 0: 
			v, n2 := decodeVarint(data[i:])
			i += n2
			tsMinutes = v
		default:
			i = skipField(data, i, wire)
		}
	}
	if low == 0 && tsMinutes == 0 {
		return Trendbar{}, false
	}
	lowF := float64(low) / divisor
	return Trendbar{
		OpenTime: int64(tsMinutes) * 60,
		Period:   uint32(period), 
		Open:     lowF + float64(deltaOpen)/divisor,
		High:     lowF + float64(deltaHigh)/divisor,
		Low:      lowF,
		Close:    lowF + float64(deltaClose)/divisor,
		Volume:   int64(volume),
	}, true
}


func decodeGetTrendbarsRes(data []byte) []Trendbar {
	var bars []Trendbar
	i := 0
	for i < len(data) {
		tag, n := decodeVarint(data[i:])
		if n == 0 {
			break
		}
		i += n
		field := tag >> 3
		wire := tag & 0x7
		if field == 5 && wire == 2 {
			l, n2 := decodeVarint(data[i:])
			i += n2
			if bar, ok := decodeTrendbar(data[i : i+int(l)]); ok {
				bars = append(bars, bar)
			}
			i += int(l)
			continue
		}
		i = skipField(data, i, wire)
	}
	return bars
}


func decodeLiveTrendbarEvents(data []byte) []Trendbar {
	var bars []Trendbar
	i := 0
	for i < len(data) {
		tag, n := decodeVarint(data[i:])
		if n == 0 {
			break
		}
		i += n
		field := tag >> 3
		wire := tag & 0x7
		if field == 6 && wire == 2 { 
			l, n2 := decodeVarint(data[i:])
			i += n2
			if bar, ok := decodeTrendbar(data[i : i+int(l)]); ok {
				bars = append(bars, bar)
			}
			i += int(l)
			continue
		}
		i = skipField(data, i, wire)
	}
	return bars
}


func decodeTraderRes(data []byte) (TraderInfo, bool) {
	traderBytes := extractLenField(data, 3)
	if traderBytes == nil {
		return TraderInfo{}, false
	}

	var info TraderInfo
	var rawBalance int64
	var moneyDigits uint64 = 2 

	i := 0
	for i < len(traderBytes) {
		tag, n := decodeVarint(traderBytes[i:])
		if n == 0 {
			break
		}
		i += n
		field := tag >> 3
		wire := tag & 0x7
		switch {
		case field == 1 && wire == 0: 
			v, n2 := decodeVarint(traderBytes[i:])
			i += n2
			info.AccountID = int64(v)
		case field == 2 && wire == 0: 
			v, n2 := decodeVarint(traderBytes[i:])
			i += n2
			rawBalance = int64(v)
		case field == 10 && wire == 0: 
			v, n2 := decodeVarint(traderBytes[i:])
			i += n2
			info.Leverage = float64(v) / 100.0
		case field == 12 && wire == 0: 
			v, n2 := decodeVarint(traderBytes[i:])
			i += n2
			info.MaxLeverage = float64(v)
		case field == 15 && wire == 0: 
			v, n2 := decodeVarint(traderBytes[i:])
			i += n2
			switch v {
			case 1:
				info.AccountMode = "netted"
			case 2:
				info.AccountMode = "linked"
			default:
				info.AccountMode = "hedged"
			}
		case field == 16 && wire == 2: // brokerName (string)
			l, n2 := decodeVarint(traderBytes[i:])
			i += n2
			info.BrokerName = string(traderBytes[i : i+int(l)])
			i += int(l)
		case field == 18 && wire == 0: // isLimitedRisk (bool)
			v, n2 := decodeVarint(traderBytes[i:])
			i += n2
			info.IsLimitedRisk = v != 0
		case field == 20 && wire == 0: // moneyDigits (uint32)
			v, n2 := decodeVarint(traderBytes[i:])
			i += n2
			moneyDigits = v
		case field == 21 && wire == 0: // fairStopOut (bool)
			v, n2 := decodeVarint(traderBytes[i:])
			i += n2
			info.FairStopOut = v != 0
		default:
			i = skipField(traderBytes, i, wire)
		}
	}

	if rawBalance == 0 && info.AccountID == 0 {
		return TraderInfo{}, false
	}
	info.Balance = float64(rawBalance) / math.Pow(10, float64(moneyDigits))
	return info, true
}

func decodeReconcileRes(data []byte) []OpenPosition {
	var positions []OpenPosition
	i := 0
	for i < len(data) {
		tag, n := decodeVarint(data[i:])
		if n == 0 {
			break
		}
		i += n
		field := tag >> 3
		wire := tag & 0x7
		if field == 3 && wire == 2 { // repeated ProtoOAPosition (field2=ctidTraderAccountId, field3=position)
			l, n2 := decodeVarint(data[i:])
			i += n2
			pos := decodeOAPosition(data[i : i+int(l)])
			i += int(l)
			positions = append(positions, pos)
			continue
		}
		i = skipField(data, i, wire)
	}
	return positions
}

func decodeOAPosition(data []byte) OpenPosition {
	var pos OpenPosition
	i := 0
	for i < len(data) {
		tag, n := decodeVarint(data[i:])
		if n == 0 {
			break
		}
		i += n
		field := tag >> 3
		wire := tag & 0x7
		switch {
		case field == 1 && wire == 0: // positionId (int64)
			v, n2 := decodeVarint(data[i:])
			i += n2
			pos.PositionID = int64(v)
		case field == 2 && wire == 2: // tradeData (embedded ProtoOATradeData)
			l, n2 := decodeVarint(data[i:])
			i += n2
			pos = decodeTradeData(data[i:i+int(l)], pos)
			i += int(l)
		default:
			i = skipField(data, i, wire)
		}
	}
	return pos
}

func decodeTradeData(data []byte, pos OpenPosition) OpenPosition {
	i := 0
	for i < len(data) {
		tag, n := decodeVarint(data[i:])
		if n == 0 {
			break
		}
		i += n
		field := tag >> 3
		wire := tag & 0x7
		switch {
		case field == 1 && wire == 0: // symbolId (int64)
			v, n2 := decodeVarint(data[i:])
			i += n2
			pos.SymbolID = int64(v)
		case field == 2 && wire == 0: // volume (int64)
			v, n2 := decodeVarint(data[i:])
			i += n2
			pos.Volume = int64(v)
		case field == 3 && wire == 0: // tradeSide (enum: 1=BUY 2=SELL)
			v, n2 := decodeVarint(data[i:])
			i += n2
			pos.Side = uint32(v)
		default:
			i = skipField(data, i, wire)
		}
	}
	return pos
}


func decodeZigzag64(v uint64) int64 {
	return int64((v >> 1) ^ -(v & 1))
}

func extractLenField(data []byte, targetField uint64) []byte {
	i := 0
	for i < len(data) {
		tag, n := decodeVarint(data[i:])
		if n == 0 {
			break
		}
		i += n
		field := tag >> 3
		wire := tag & 0x7
		if field == targetField && wire == 2 {
			l, n2 := decodeVarint(data[i:])
			i += n2
			return data[i : i+int(l)]
		}
		i = skipField(data, i, wire)
	}
	return nil
}

func skipField(data []byte, i int, wire uint64) int {
	switch wire {
	case 0:
		_, n := decodeVarint(data[i:])
		return i + n
	case 1:
		return i + 8
	case 2:
		l, n := decodeVarint(data[i:])
		return i + n + int(l)
	case 5:
		return i + 4
	default:
		return len(data)
	}
}


type CloseDetail struct {
	EntryPrice       float64
	Swap             float64
	Commission       float64
	GrossProfit      float64
	Balance          float64 // account balance after close
	ClosedVolume     int64
	PnLConversionFee float64
}

type DealInfo struct {
	DealID         int64
	OrderID        int64
	PositionID     int64
	Volume         int64
	FilledVolume   int64
	ExecutionPrice float64
	TradeSide      uint32 // 1=BUY 2=SELL
	DealStatus     uint32
	Commission     float64
	CreateTime     time.Time
	ExecTime       time.Time
	IsClose        bool        // true when closePositionDetail is present
	Close          CloseDetail // populated only when IsClose==true
}


func decodeFullExecutionEvent(data []byte) (execType string, deal DealInfo, hasDeal bool, closedPosID int64) {
	i := 0
	var rawDeal []byte
	var rawPosition []byte
	for i < len(data) {
		tag, n := decodeVarint(data[i:])
		if n == 0 {
			break
		}
		i += n
		field := tag >> 3
		wire := tag & 0x7
		switch {
		case field == 3 && wire == 0:
			v, n2 := decodeVarint(data[i:])
			i += n2
			execType = execTypeString(v)
		case field == 4 && wire == 2: // position (ProtoOAPosition) — always present
			l, n2 := decodeVarint(data[i:])
			i += n2
			rawPosition = data[i : i+int(l)]
			i += int(l)
		case field == 6 && wire == 2: // deal (ProtoOADeal) — optional, may be absent for broker-side closes
			l, n2 := decodeVarint(data[i:])
			i += n2
			rawDeal = data[i : i+int(l)]
			i += int(l)
		default:
			i = skipField(data, i, wire)
		}
	}
	if rawDeal != nil {
		deal = decodeDeal(rawDeal)
		hasDeal = true
	}
	// When deal is absent (broker-side TP/SL close), read positionId+status from the position field.
	if rawPosition != nil && !hasDeal {
		posID, isClosed := decodePositionIDAndStatus(rawPosition)
		if isClosed {
			closedPosID = posID
		}
	}
	return
}

// decodePositionIDAndStatus extracts positionId (field 1) and positionStatus (field 3) from a
// ProtoOAPosition message. Returns (positionId, isClosed) where isClosed means status==CLOSED (2).
func decodePositionIDAndStatus(data []byte) (positionID int64, isClosed bool) {
	i := 0
	for i < len(data) {
		tag, n := decodeVarint(data[i:])
		if n == 0 {
			break
		}
		i += n
		field := tag >> 3
		wire := tag & 0x7
		switch {
		case field == 1 && wire == 0: // positionId
			v, n2 := decodeVarint(data[i:])
			i += n2
			positionID = int64(v)
		case field == 3 && wire == 0: // positionStatus: 1=OPEN, 2=CLOSED
			v, n2 := decodeVarint(data[i:])
			i += n2
			isClosed = v == 2
		default:
			i = skipField(data, i, wire)
		}
	}
	return
}

func execTypeString(v uint64) string {
	switch v {
	case 2:
		return "ORDER_ACCEPTED"
	case 3:
		return "ORDER_FILLED"
	case 4:
		return "ORDER_REPLACED"
	case 5:
		return "ORDER_CANCELLED"
	case 6:
		return "ORDER_EXPIRED"
	case 7:
		return "ORDER_REJECTED"
	case 8:
		return "ORDER_CANCEL_REJECTED"
	case 9:
		return "SWAP"
	case 10:
		return "DEPOSIT_WITHDRAW"
	case 11:
		return "ORDER_PARTIAL_FILL"
	case 12:
		return "BONUS_DEPOSIT_WITHDRAW"
	default:
		return "UNKNOWN"
	}
}

func decodeDeal(data []byte) DealInfo {
	var d DealInfo
	var rawCommission int64
	var rawClose []byte
	var moneyDigits uint64 = 2

	i := 0
	for i < len(data) {
		tag, n := decodeVarint(data[i:])
		if n == 0 {
			break
		}
		i += n
		field := tag >> 3
		wire := tag & 0x7
		switch {
		case field == 1 && wire == 0:
			v, n2 := decodeVarint(data[i:])
			i += n2
			d.DealID = int64(v)
		case field == 2 && wire == 0:
			v, n2 := decodeVarint(data[i:])
			i += n2
			d.OrderID = int64(v)
		case field == 3 && wire == 0:
			v, n2 := decodeVarint(data[i:])
			i += n2
			d.PositionID = int64(v)
		case field == 4 && wire == 0:
			v, n2 := decodeVarint(data[i:])
			i += n2
			d.Volume = int64(v)
		case field == 5 && wire == 0:
			v, n2 := decodeVarint(data[i:])
			i += n2
			d.FilledVolume = int64(v)
		case field == 7 && wire == 0:
			v, n2 := decodeVarint(data[i:])
			i += n2
			d.CreateTime = time.UnixMilli(int64(v)).UTC()
		case field == 8 && wire == 0:
			v, n2 := decodeVarint(data[i:])
			i += n2
			d.ExecTime = time.UnixMilli(int64(v)).UTC()
		case field == 10 && wire == 1: // double
			if i+8 <= len(data) {
				bits := binary.LittleEndian.Uint64(data[i:])
				d.ExecutionPrice = math.Float64frombits(bits)
			}
			i += 8
		case field == 11 && wire == 0:
			v, n2 := decodeVarint(data[i:])
			i += n2
			d.TradeSide = uint32(v)
		case field == 12 && wire == 0:
			v, n2 := decodeVarint(data[i:])
			i += n2
			d.DealStatus = uint32(v)
		case field == 14 && wire == 0: // commission (int64)
			v, n2 := decodeVarint(data[i:])
			i += n2
			rawCommission = int64(v)
		case field == 16 && wire == 2: // closePositionDetail
			l, n2 := decodeVarint(data[i:])
			i += n2
			rawClose = data[i : i+int(l)]
			i += int(l)
			d.IsClose = true
		case field == 17 && wire == 0: // moneyDigits
			v, n2 := decodeVarint(data[i:])
			i += n2
			moneyDigits = v
		default:
			i = skipField(data, i, wire)
		}
	}

	scale := math.Pow(10, float64(moneyDigits))
	d.Commission = float64(rawCommission) / scale
	if rawClose != nil {
		d.Close = decodeCloseDetail(rawClose, scale)
	}
	return d
}


func decodeCloseDetail(data []byte, scale float64) CloseDetail {
	var c CloseDetail
	var rawSwap, rawCommission, rawBalance, rawGrossProfit, rawClosedVolume, rawPnLFee int64

	i := 0
	for i < len(data) {
		tag, n := decodeVarint(data[i:])
		if n == 0 {
			break
		}
		i += n
		field := tag >> 3
		wire := tag & 0x7
		switch {
		case field == 1 && wire == 1: // entryPrice (double)
			if i+8 <= len(data) {
				bits := binary.LittleEndian.Uint64(data[i:])
				c.EntryPrice = math.Float64frombits(bits)
			}
			i += 8
		case field == 2 && wire == 0: // grossProfit (int64)
			v, n2 := decodeVarint(data[i:])
			i += n2
			rawGrossProfit = int64(v)
		case field == 3 && wire == 0: // swap (int64)
			v, n2 := decodeVarint(data[i:])
			i += n2
			rawSwap = int64(v)
		case field == 4 && wire == 0: // commission (int64)
			v, n2 := decodeVarint(data[i:])
			i += n2
			rawCommission = int64(v)
		case field == 5 && wire == 0: // balance (int64)
			v, n2 := decodeVarint(data[i:])
			i += n2
			rawBalance = int64(v)
		case field == 7 && wire == 0: // closedVolume (int64)
			v, n2 := decodeVarint(data[i:])
			i += n2
			rawClosedVolume = int64(v)
		case field == 10 && wire == 0: // pnlConversionFee (int64)
			v, n2 := decodeVarint(data[i:])
			i += n2
			rawPnLFee = int64(v)
		default:
			i = skipField(data, i, wire)
		}
	}

	c.Swap = float64(rawSwap) / scale
	c.Commission = float64(rawCommission) / scale
	c.Balance = float64(rawBalance) / scale
	c.GrossProfit = float64(rawGrossProfit) / scale
	c.ClosedVolume = rawClosedVolume
	c.PnLConversionFee = float64(rawPnLFee) / scale
	return c
}



func decodeOrderError(data []byte) (errorCode string, orderID int64, description string) {
	i := 0
	for i < len(data) {
		tag, n := decodeVarint(data[i:])
		if n == 0 {
			break
		}
		i += n
		field := tag >> 3
		wire := tag & 0x7
		switch {
		case field == 2 && wire == 2:
			l, n2 := decodeVarint(data[i:])
			i += n2
			errorCode = string(data[i : i+int(l)])
			i += int(l)
		case field == 3 && wire == 0:
			v, n2 := decodeVarint(data[i:])
			i += n2
			orderID = int64(v)
		case field == 7 && wire == 2:
			l, n2 := decodeVarint(data[i:])
			i += n2
			description = string(data[i : i+int(l)])
			i += int(l)
		default:
			i = skipField(data, i, wire)
		}
	}
	return
}


func decodeGenericError(data []byte) (code uint32, description string) {
	i := 0
	for i < len(data) {
		tag, n := decodeVarint(data[i:])
		if n == 0 {
			break
		}
		i += n
		field := tag >> 3
		wire := tag & 0x7
		switch {
		case field == 2 && wire == 0:
			v, n2 := decodeVarint(data[i:])
			i += n2
			code = uint32(v)
		case field == 3 && wire == 2:
			l, n2 := decodeVarint(data[i:])
			i += n2
			description = string(data[i : i+int(l)])
			i += int(l)
		default:
			i = skipField(data, i, wire)
		}
	}
	return
}

func decodeOAError(data []byte) (code, description string) {
	i := 0
	for i < len(data) {
		tag, n := decodeVarint(data[i:])
		if n == 0 {
			break
		}
		i += n
		field := tag >> 3
		wire := tag & 0x7
		switch {
		case field == 3 && wire == 2: // errorCode (string)
			l, n2 := decodeVarint(data[i:])
			i += n2
			code = string(data[i : i+int(l)])
			i += int(l)
		case field == 4 && wire == 2: // description (string)
			l, n2 := decodeVarint(data[i:])
			i += n2
			description = string(data[i : i+int(l)])
			i += int(l)
		default:
			i = skipField(data, i, wire)
		}
	}
	return
}


func decodeSpotEvent(data []byte) (bid, ask uint64, ok bool) {
	i := 0
	for i < len(data) {
		tag, n := decodeVarint(data[i:])
		if n == 0 {
			break
		}
		i += n
		field := tag >> 3
		wire := tag & 0x7
		switch  wire{
		case 0: // varint
			val, n2 := decodeVarint(data[i:])
			i += n2
			switch field {
			case 4:
				bid = val
			case 5:
				ask = val
			}
		case 2: // length-delimited
			l, n2 := decodeVarint(data[i:])
			i += n2 + int(l)
		case 1: // 64-bit
			i += 8
		case 5: // 32-bit
			i += 4
		default:
			return 0, 0, false
		}
	}
	return bid, ask, true
}

func decodeVarint(b []byte) (uint64, int) {
	var x uint64
	var s uint
	for i, c := range b {
		if i == 10 {
			return 0, 0
		}
		if c < 0x80 {
			return x | uint64(c)<<s, i + 1
		}
		x |= uint64(c&0x7f) << s
		s += 7
	}
	return 0, 0
}

func encodeGetAccountListReq(accessToken string) []byte {
	var b []byte
	b = appendUint32(b, 1, ProtoOAGetAccountListByAccessTokenReq)
	b = appendString(b, 2, accessToken)
	return b
}

func decodeAccountListRes(data []byte) []CtidAccount {
	var accounts []CtidAccount
	i := 0
	for i < len(data) {
		tag, n := decodeVarint(data[i:])
		if n == 0 {
			break
		}
		i += n
		field := tag >> 3
		wire := tag & 0x7
		if field == 4 && wire == 2 { // repeated ctidTraderAccount (field 2=accessToken, field 3=bool, field 4=accounts)
			l, n2 := decodeVarint(data[i:])
			i += n2
			acc := decodeCtidAccount(data[i : i+int(l)])
			i += int(l)
			accounts = append(accounts, acc)
			continue
		}
		i = skipField(data, i, wire)
	}
	return accounts
}

func decodeCtidAccount(data []byte) CtidAccount {
	var acc CtidAccount
	i := 0
	for i < len(data) {
		tag, n := decodeVarint(data[i:])
		if n == 0 {
			break
		}
		i += n
		field := tag >> 3
		wire := tag & 0x7
		switch {
		case field == 1 && wire == 0: // ctidTraderAccountId
			v, n2 := decodeVarint(data[i:])
			i += n2
			acc.CtidTraderAccountID = int64(v)
		case field == 2 && wire == 0: // isLive
			v, n2 := decodeVarint(data[i:])
			i += n2
			acc.IsLive = v != 0
		case field == 3 && wire == 0: // traderLogin (broker account number)
			v, n2 := decodeVarint(data[i:])
			i += n2
			acc.TraderLogin = int64(v)
		case field == 6 && wire == 2: // brokerTitle (string)
			l, n2 := decodeVarint(data[i:])
			i += n2
			acc.BrokerName = string(data[i : i+int(l)])
			i += int(l)
		default:
			i = skipField(data, i, wire)
		}
	}
	return acc
}

func payloadTypeOf(data []byte) uint32 {
	i := 0
	for i < len(data) {
		tag, n := decodeVarint(data[i:])
		if n == 0 {
			break
		}
		i += n
		field := tag >> 3
		wire := tag & 0x7
		if field == 1 && wire == 0 {
			val, _ := decodeVarint(data[i:])
			return uint32(val)
		}
		// skip other fields
		switch wire {
		case 0:
			_, n2 := decodeVarint(data[i:])
			i += n2
		case 2:
			l, n2 := decodeVarint(data[i:])
			i += n2 + int(l)
		case 1:
			i += 8
		case 5:
			i += 4
		}
	}
	return 0
}

func payloadOf(data []byte) []byte {
	i := 0
	for i < len(data) {
		tag, n := decodeVarint(data[i:])
		if n == 0 {
			break
		}
		i += n
		field := tag >> 3
		wire := tag & 0x7
		if field == 2 && wire == 2 {
			l, n2 := decodeVarint(data[i:])
			i += n2
			return data[i : i+int(l)]
		}
		switch wire {
		case 0:
			_, n2 := decodeVarint(data[i:])
			i += n2
		case 2:
			l, n2 := decodeVarint(data[i:])
			i += n2 + int(l)
		case 1:
			i += 8
		case 5:
			i += 4
		}
	}
	return nil
}
