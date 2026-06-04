package api

// Manual protobuf encoding for cTrader Open API messages.
// We encode only the specific messages the bot needs — no generated code required.
//
// Protobuf wire format:
//   field tag  = (field_number << 3) | wire_type
//   wire types: 0=varint, 2=length-delimited (string/bytes/embedded msg)

import (
	"encoding/binary"
	"math"
	"time"
)

// --- wire encoding helpers ---

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

// --- ProtoMessage (outer envelope) ---

func encodeEnvelope(payloadType uint32, inner []byte) []byte {
	var b []byte
	b = appendUint32(b, 1, payloadType)
	if len(inner) > 0 {
		b = appendBytes(b, 2, inner)
	}
	return b
}

// --- Inner message encoders ---

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

// encodeClosePositionReq builds a ProtoOAClosePositionReq message.
// positionID is the broker's numeric position ID; volume is in provider units.
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
	b = appendUint32(b, 4, 1) // MARKET order
	b = appendUint32(b, 5, side)
	b = appendInt64(b, 6, volume)

	// For MARKET orders, use relativeStopLoss/relativeTakeProfit in units of 1/100000
	// sl and tp are in pips (0.0001 EUR), convert to 1/100000 units: pips * 10
	if sl > 0 {
		b = appendInt64(b, 19, int64(sl*10)) // relativeStopLoss
	}
	if tp > 0 {
		b = appendInt64(b, 20, int64(tp*10)) // relativeTakeProfit
	}
	return b
}

func appendDouble(b []byte, field int, v float64) []byte {
	b = appendTag(b, field, 1) // wire type 1 = 64-bit
	var buf [8]byte
	bits := math.Float64bits(v)
	binary.LittleEndian.PutUint64(buf[:], bits)
	return append(b, buf[:]...)
}

// --- Decoder helpers ---

// --- Result types ---

// TraderInfo holds the fields we extract from ProtoOATraderRes.
type TraderInfo struct {
	AccountID  int64
	Balance    float64 // real value in deposit currency
	Leverage   float64 // e.g. 100.0 = 100x leverage
	BrokerName string
}

// OpenPosition is a simplified position extracted from ProtoOAReconcileRes.
// Used on startup to detect trades already open so the bot doesn't double-enter.
type OpenPosition struct {
	PositionID int64
	SymbolID   int64
	Side       uint32 // 1=BUY 2=SELL
	Volume     int64
}

// --- Encoders for new request types ---

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

// encodeSubscribeLiveTrendbarReq subscribes to real-time M5 trendbar events for a symbol.
// period: use PeriodM5 (4) for 5-minute candles.
func encodeSubscribeLiveTrendbarReq(accountID, symbolID int64, period uint32) []byte {
	var b []byte
	b = appendUint32(b, 1, ProtoOASubscribeLiveTrendbarReq)
	b = appendInt64(b, 2, accountID)
	b = appendUint32(b, 3, period)
	b = appendInt64(b, 4, symbolID)
	return b
}

// encodeGetTrendbarsReq requests the last `count` completed bars before toTimestamp.
// Actual proto fields: 2=ctidTraderAccountId, 3=fromTimestamp, 4=toTimestamp, 5=period, 6=symbolId, 7=count.
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

// --- Trendbar types and decoder ---

// Trendbar is a completed OHLC candle from cTrader.
type Trendbar struct {
	OpenTime  int64   // Unix seconds (utcTimestampInMinutes * 60)
	Period    uint32  // 5=M5, 9=H1, 10=H4, 11=D1, etc.
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    int64
}

// decodeTrendbar parses a single ProtoOATrendbar message.
// Actual proto field numbers (from OpenApiModelMessages.proto):
//   3=volume, 4=period, 5=low, 6=deltaOpen, 7=deltaClose, 8=deltaHigh, 9=utcTimestampInMinutes
// Delta encoding: low is absolute; open = low+deltaOpen, close = low+deltaClose, high = low+deltaHigh.
// All price values are in 1/100000 units (divide by 100000.0).
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
		case field == 3 && wire == 0: // volume (int64)
			v, n2 := decodeVarint(data[i:])
			i += n2
			volume = v
		case field == 4 && wire == 0: // period (enum)
			v, n2 := decodeVarint(data[i:])
			i += n2
			period = v
		case field == 5 && wire == 0: // low (int64, always positive for prices)
			v, n2 := decodeVarint(data[i:])
			i += n2
			low = v
		case field == 6 && wire == 0: // deltaOpen (uint64, offset above low)
			v, n2 := decodeVarint(data[i:])
			i += n2
			deltaOpen = v
		case field == 7 && wire == 0: // deltaClose (uint64, offset above low)
			v, n2 := decodeVarint(data[i:])
			i += n2
			deltaClose = v
		case field == 8 && wire == 0: // deltaHigh (uint64, offset above low)
			v, n2 := decodeVarint(data[i:])
			i += n2
			deltaHigh = v
		case field == 9 && wire == 0: // utcTimestampInMinutes (uint32)
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
		Period:   uint32(period),  // 5=M5, 9=H1, 10=H4, 11=D1
		Open:     lowF + float64(deltaOpen)/divisor,
		High:     lowF + float64(deltaHigh)/divisor,
		Low:      lowF,
		Close:    lowF + float64(deltaClose)/divisor,
		Volume:   int64(volume),
	}, true
}

// decodeTrendbarForPeriod parses a ProtoOATrendbar and returns it only if
// it matches wantPeriod (field 4 of the trendbar message is the period enum).
func decodeTrendbarForPeriod(data []byte, wantPeriod uint32) (Trendbar, bool) {
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
	if uint32(period) != wantPeriod || low == 0 && tsMinutes == 0 {
		return Trendbar{}, false
	}
	lowF := float64(low) / divisor
	return Trendbar{
		OpenTime: int64(tsMinutes) * 60,
		Open:     lowF + float64(deltaOpen)/divisor,
		High:     lowF + float64(deltaHigh)/divisor,
		Low:      lowF,
		Close:    lowF + float64(deltaClose)/divisor,
		Volume:   int64(volume),
	}, true
}

// decodeGetTrendbarsRes extracts the list of historical trendbars from a ProtoOAGetTrendbarsRes.
// Field 5 is repeated ProtoOATrendbar (from actual OpenApiMessages.proto).
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

// decodeLiveTrendbarEvents extracts all trendbars from a ProtoOASpotEvent payload.
// Field 6 of SpotEvent is repeated ProtoOATrendbar, one entry per subscribed period.
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
		if field == 6 && wire == 2 { // repeated ProtoOATrendbar in SpotEvent
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

// --- Decoders for new response types ---

// decodeTraderRes extracts account info from a ProtoOATraderRes payload.
// The balance field uses sint64 (zigzag) encoding; moneyDigits converts to real currency.
func decodeTraderRes(data []byte) (TraderInfo, bool) {
	// field 3 in TraderRes is the embedded ProtoOATrader message
	traderBytes := extractLenField(data, 3)
	if traderBytes == nil {
		return TraderInfo{}, false
	}

	var info TraderInfo
	var rawBalance int64
	var moneyDigits uint64 = 2 // default — most brokers use cents

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
		case field == 1 && wire == 0: // ctidTraderAccountId (sint64 zigzag)
			v, n2 := decodeVarint(traderBytes[i:])
			i += n2
			info.AccountID = decodeZigzag64(v)
		case field == 3 && wire == 0: // balance (sint64 zigzag)
			v, n2 := decodeVarint(traderBytes[i:])
			i += n2
			rawBalance = decodeZigzag64(v)
		case field == 11 && wire == 0: // leverageInCents (uint32)
			v, n2 := decodeVarint(traderBytes[i:])
			i += n2
			info.Leverage = float64(v) / 100.0
		case field == 24 && wire == 2: // brokerName (string)
			l, n2 := decodeVarint(traderBytes[i:])
			i += n2
			info.BrokerName = string(traderBytes[i : i+int(l)])
			i += int(l)
		case field == 35 && wire == 0: // moneyDigits (uint32)
			v, n2 := decodeVarint(traderBytes[i:])
			i += n2
			moneyDigits = v
		default:
			i = skipField(traderBytes, i, wire)
		}
	}

	info.Balance = float64(rawBalance) / math.Pow(10, float64(moneyDigits))
	return info, true
}

// decodeReconcileRes extracts all open positions from a ProtoOAReconcileRes payload.
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
		if field == 2 && wire == 2 { // repeated ProtoOAPosition
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
		case field == 1 && wire == 0: // positionId (sint64 zigzag)
			v, n2 := decodeVarint(data[i:])
			i += n2
			pos.PositionID = decodeZigzag64(v)
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
		case field == 1 && wire == 0: // symbolId (sint64 zigzag)
			v, n2 := decodeVarint(data[i:])
			i += n2
			pos.SymbolID = decodeZigzag64(v)
		case field == 2 && wire == 0: // volume (sint64 zigzag)
			v, n2 := decodeVarint(data[i:])
			i += n2
			pos.Volume = decodeZigzag64(v)
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

// --- Shared decoder helpers ---

// decodeZigzag64 converts a zigzag-encoded uint64 (used for proto sint64 fields) to int64.
// zigzag: 0→0, 1→-1, 2→1, 3→-2, 4→2 ...
func decodeZigzag64(v uint64) int64 {
	return int64((v >> 1) ^ -(v & 1))
}

// extractLenField returns the bytes of the first length-delimited field matching targetField.
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

// skipField advances past the value of a field with the given wire type.
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

// CloseDetail holds the fields from ProtoOAClosePositionDetail.
// All money amounts are already divided by moneyDigits from the parent deal.
type CloseDetail struct {
	EntryPrice       float64
	Swap             float64
	Commission       float64
	GrossProfit      float64
	Balance          float64 // account balance after close
	ClosedVolume     int64
	PnLConversionFee float64
}

// DealInfo holds the fields we extract from ProtoOADeal inside a ProtoOAExecutionEvent.
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

// decodeFullExecutionEvent decodes a ProtoOAExecutionEvent payload.
// Returns the execution type string and deal info (if the event carries one).
// ProtoOAExecutionEvent fields: 2=deal, 3=executionType.
func decodeFullExecutionEvent(data []byte) (execType string, deal DealInfo, hasDeal bool) {
	i := 0
	var rawDeal []byte
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
		case field == 6 && wire == 2: // deal (ProtoOADeal) is at field 6
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
	return
}

func execTypeString(v uint64) string {
	switch v {
	case 2:
		return "ORDER_ACCEPTED"
	case 3:
		return "ORDER_FILLED"
	case 4:
		return "ORDER_REJECTED"
	case 5:
		return "ORDER_CANCELLED"
	case 6:
		return "ORDER_EXPIRED"
	case 9:
		return "SWAP"
	case 10:
		return "DEPOSIT_WITHDRAW"
	case 11:
		return "ORDER_PARTIAL_FILL"
	default:
		return "UNKNOWN"
	}
}

// decodeDeal parses a ProtoOADeal message.
// ProtoOADeal fields: 1=dealId, 2=orderId, 3=positionId, 4=volume, 5=filledVolume,
// 7=createTimestamp(ms), 8=executionTimestamp(ms), 10=executionPrice(double),
// 11=tradeSide, 12=dealStatus, 14=commission(sint64), 16=closePositionDetail, 17=moneyDigits.
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
		case field == 14 && wire == 0: // commission (sint64 zigzag)
			v, n2 := decodeVarint(data[i:])
			i += n2
			rawCommission = decodeZigzag64(v)
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

// decodeCloseDetail parses a ProtoOAClosePositionDetail message.
// Fields: 1=entryPrice(double), 2=swap(sint64), 3=commission(sint64),
// 4=balance(sint64), 5=grossProfit(sint64), 7=closedVolume(sint64), 9=pnlConversionFee(sint64).
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
		case field == 2 && wire == 0:
			v, n2 := decodeVarint(data[i:])
			i += n2
			rawSwap = decodeZigzag64(v)
		case field == 3 && wire == 0:
			v, n2 := decodeVarint(data[i:])
			i += n2
			rawCommission = decodeZigzag64(v)
		case field == 4 && wire == 0:
			v, n2 := decodeVarint(data[i:])
			i += n2
			rawBalance = decodeZigzag64(v)
		case field == 5 && wire == 0:
			v, n2 := decodeVarint(data[i:])
			i += n2
			rawGrossProfit = decodeZigzag64(v)
		case field == 7 && wire == 0:
			v, n2 := decodeVarint(data[i:])
			i += n2
			rawClosedVolume = decodeZigzag64(v)
		case field == 9 && wire == 0:
			v, n2 := decodeVarint(data[i:])
			i += n2
			rawPnLFee = decodeZigzag64(v)
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

// decodeExecutionType reads only the executionType field (kept for callers that don't need deal data).
func decodeExecutionType(data []byte) string {
	i := 0
	for i < len(data) {
		tag, n := decodeVarint(data[i:])
		if n == 0 {
			break
		}
		i += n
		field := tag >> 3
		wire := tag & 0x7
		if field == 3 && wire == 0 {
			v, _ := decodeVarint(data[i:])
			return execTypeString(v)
		}
		i = skipField(data, i, wire)
	}
	return "UNKNOWN"
}

// decodeOrderError decodes a ProtoOAOrderErrorEvent (2132) payload.
// Fields: 2=errorCode(string), 3=orderId(int64), 7=description(string).
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

// decodeGenericError decodes a type-50 ProtoErrorRes payload.
// Fields: 2=errorCode (uint32), 3=description (string).
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

// decodeOAError decodes a ProtoOAErrorRes (2142) payload.
// Fields: 2=errorCode (string), 3=description (string).
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
		case field == 2 && wire == 2:
			l, n2 := decodeVarint(data[i:])
			i += n2
			code = string(data[i : i+int(l)])
			i += int(l)
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

// --- Original decoders ---

// decodeSpotEvent reads bid and ask from a raw spot event payload.
// Returns (bid, ask, ok).
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
		switch {
		case wire == 0: // varint
			val, n2 := decodeVarint(data[i:])
			i += n2
			if field == 4 {
				bid = val
			} else if field == 5 {
				ask = val
			}
		case wire == 2: // length-delimited
			l, n2 := decodeVarint(data[i:])
			i += n2 + int(l)
		case wire == 1: // 64-bit
			i += 8
		case wire == 5: // 32-bit
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

// decodeAccountListRes decodes ProtoOAGetAccountListByAccessTokenRes.
// Each ctidTraderAccount has: field1=ctidTraderAccountId (uint64), field2=isLive (bool), field3=traderLogin (uint64).
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
		default:
			i = skipField(data, i, wire)
		}
	}
	return acc
}

// payloadTypeOf extracts field 1 (payloadType) from a raw proto envelope.
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

// payloadOf extracts field 2 (inner payload bytes) from a raw proto envelope.
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
