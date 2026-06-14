package oanda

// Account adalah ringkasan akun dari GET /v3/accounts.
type Account struct {
	ID   string `json:"id"`
	Tags []struct {
		Type string `json:"type"`
		Name string `json:"name"`
	} `json:"tags"`
}

// accountsResponse membungkus daftar akun.
type accountsResponse struct {
	Accounts []Account `json:"accounts"`
}

// ohlc adalah blok harga (mid/bid/ask) di dalam satu candle.
// OANDA mengirim angka sebagai string, jadi di-decode sebagai string lalu
// di-parse ke float64 saat dikonversi ke domain Candle.
type ohlc struct {
	O string `json:"o"`
	H string `json:"h"`
	L string `json:"l"`
	C string `json:"c"`
}

// rawCandle adalah satu candle apa adanya dari respons OANDA.
type rawCandle struct {
	Time     string `json:"time"` // RFC3339 nano, UTC
	Volume   int64  `json:"volume"`
	Complete bool   `json:"complete"`
	Mid      ohlc   `json:"mid"`
}

// candlesResponse adalah respons GET /v3/instruments/{instrument}/candles.
type candlesResponse struct {
	Instrument  string      `json:"instrument"`
	Granularity string      `json:"granularity"`
	Candles     []rawCandle `json:"candles"`
}

// apiError adalah bentuk error JSON standar dari OANDA.
type apiError struct {
	ErrorMessage string `json:"errorMessage"`
}
