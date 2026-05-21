package api

// cTrader Open API payload type constants (from OpenApiModelMessages.proto)
const (
	ProtoOAApplicationAuthReq = uint32(2100)
	ProtoOAApplicationAuthRes = uint32(2101)
	ProtoOAAccountAuthReq     = uint32(2102)
	ProtoOAAccountAuthRes     = uint32(2103)
	ProtoOATraderReq          = uint32(2104)
	ProtoOATraderRes          = uint32(2105)
	ProtoOANewOrderReq        = uint32(2106)
	ProtoOAReconcileReq       = uint32(2124)
	ProtoOAReconcileRes       = uint32(2125)
	ProtoOAExecutionEvent     = uint32(2126)
	ProtoOASubscribeSpotsReq  = uint32(2127)
	ProtoOASubscribeSpotsRes  = uint32(2128)
	ProtoOASpotEvent          = uint32(2131)
	ProtoOAErrorRes           = uint32(2142)

	ProtoOAGetAccountListByAccessTokenReq = uint32(2149)
	ProtoOAGetAccountListByAccessTokenRes = uint32(2150)

	ProtoOASubscribeLiveTrendbarReq = uint32(2135)
	ProtoOASubscribeLiveTrendbarRes = uint32(2165)
	ProtoOAGetTrendbarsReq          = uint32(2137)
	ProtoOAGetTrendbarsRes          = uint32(2138)
	ProtoOAOrderErrorEvent          = uint32(2132)

	TradeSideBuy  = uint32(1)
	TradeSideSell = uint32(2)

	// ProtoOATrendbarPeriod enum values (M1=1, M2=2, M3=3, M4=4, M5=5, M10=6, M15=7, M30=8, H1=9)
	PeriodM5  = uint32(5)
	PeriodM15 = uint32(7)
	PeriodH1  = uint32(9)
)
